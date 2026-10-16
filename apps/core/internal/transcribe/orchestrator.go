package transcribe

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Segment is one transcribed unit tagged with the audio start offset (seconds)
// of the chunk it came from. Single-shot transcriptions produce one segment at
// offset 0. This is the lib's output unit — callers map it onto their own
// persistence model (DB rows, NATS payloads…). The lib itself persists nothing.
//
// StartSeconds (int) is kept for backward compatibility: it is the chunk start
// offset, and remains the coarse value existing callers rely on. Start/End
// (float seconds, absolute) carry the provider's real per-segment timing when
// available (verbose_json), already re-offset to absolute time across chunks.
// When the provider exposed no timing, Start == float64(StartSeconds) and End
// is the next boundary (or 0), so a caller can always read Start/End.
//
// HasTiming reports whether Start/End come from real provider segment timing
// (true) versus a coarse chunk-offset fallback (false). Callers must use this
// flag — NOT a Start == 0 heuristic — to tell the two apart: a legitimate real
// segment can have an absolute Start of 0 (the first segment of chunk 0).
type Segment struct {
	Text         string
	StartSeconds int
	Start        float64
	End          float64
	HasTiming    bool
}

// Result is the value returned by Transcribe. It is a plain Go value: the lib
// does not publish events or write to any store: the caller decides what to do
// with it.
type Result struct {
	Segments []Segment
	// DurationSeconds is the measured audio length (via ffprobe), the authoritative
	// basis for duration-based billing. It is the FULL length — unlike a segment's
	// StartSeconds, it includes the final segment. 0 means the probe was unavailable
	// (e.g. ffprobe missing); callers must treat 0 as "unknown", not "empty audio".
	DurationSeconds float64
	// ProbeError is why the duration is unknown, nil when it was measured. The
	// lib logs nothing by design, and a swallowed probe failure is invisible:
	// it silently disables the duration cap and bills an estimate instead of a
	// measurement. Surfacing it lets the caller log how often this really
	// happens rather than guess.
	ProbeError error
	// FailedChunks lists indices that permanently failed (graceful degradation
	// kept the surviving text and substituted a marker for these). Empty on a
	// fully successful transcription.
	FailedChunks []int
	// Language is the ISO 639-1 code the backend detected in the audio, or ""
	// when unavailable. On a chunked transcription it is the first chunk's
	// detection: the spoken language of a single recording does not change
	// partway through, and a per-chunk disagreement would only reflect noise.
	Language string
}

// Options tunes a Transcribe call. Zero values fall back to sane defaults.
type Options struct {
	// Language hint passed to the provider ("" = auto-detect). Note: a user's
	// desired OUTPUT language is NOT a valid transcription hint — leave empty
	// to let the backend detect the native audio language.
	Language string
	// ChunkDuration in seconds for long-file splitting. 0 → DefaultChunkDuration.
	ChunkDuration int
	// MaxSingleShotDuration in seconds: audio longer than this is chunked even
	// when it fits MaxFileSize. 0 → DefaultMaxSingleShotDuration. Raise it for
	// backends that accept long single requests (e.g. OVH AI Endpoints) so a
	// whole podcast goes in ONE request instead of many chunks.
	MaxSingleShotDuration int
	// MaxParallel bounds concurrent chunk transcriptions. 0 → 1 (sequential).
	// The worker, not the lib, decides how many ffmpeg/STT run at once.
	MaxParallel int
	// MaxDurationSeconds rejects audio longer than this with ErrTooLong, BEFORE
	// any provider call. 0 → no cap, so callers that do not set it are
	// unaffected.
	//
	// This is the last gate that can still act on a real measurement: callers
	// that know the duration up front (yt-dlp metadata, a YouTube API response)
	// should reject even earlier and never pay for the download. This one exists
	// because those sources report 0 often enough, and the alternative is
	// transcribing a 6-hour file in full before noticing.
	MaxDurationSeconds float64
	// MaxBytesWhenUnmeasurable caps files whose duration ffprobe could NOT read,
	// rejecting them with ErrUnmeasurable. 0 → accept any unmeasurable file.
	//
	// It applies ONLY on that fallback: a probed file is judged on its real
	// duration, never on its size. Bytes are a poor proxy for length — bitrate
	// varies wildly — which is exactly why this is a blunt outer bound and not a
	// second duration cap. Set it well above any plausible file so it catches
	// the absurd (a 10-hour upload) and never a legitimate one.
	//
	// The alternative, refusing everything unmeasurable, would fail real audio
	// whenever ffprobe trips on an unusual container.
	MaxBytesWhenUnmeasurable int64
	// RequireMeasurableAudio turns the probe into a gate: the file must carry a
	// decodable audio stream AND report a finite duration, or it is refused
	// before any provider call, with ErrNoAudioStream or ErrUnboundedDuration.
	//
	// It exists because "we could not measure it" and "it has no end" are the
	// same observation from here, and a stream with no end is what a decoder
	// must never start reading. Off by default: the byte fallback above stays
	// the behaviour for callers that would rather transcribe an odd container
	// than refuse it.
	RequireMeasurableAudio bool
	// Attribution passed straight through to the provider for cost tracking.
	SourceID   string
	UserID     string
	APITokenID string
	// FFmpegPath / FFprobePath override binary lookup (tests). Empty → PATH.
	FFmpegPath  string
	FFprobePath string
}

// Retry budgets. 429 (rate limit) gets a wider budget because the upstream
// explicitly asks us to back off and retry.
const (
	maxRetriesDefault     = 3
	maxRetriesRateLimited = 5
	retryBaseDelay        = 500 * time.Millisecond
	retryMaxDelay         = 30 * time.Second
	// maxFailedChunkRatio: fraction of chunks allowed to fail before the whole
	// transcription is a hard failure. At or below it we degrade gracefully.
	maxFailedChunkRatio = 0.5
)

// DefaultMaxSingleShotDuration: audio longer than this is chunked, even when it
// fits MaxFileSize — some backends 500 on long single files. It is the SAFE
// default; callers override via Options.MaxSingleShotDuration for backends that
// accept long single requests.
const DefaultMaxSingleShotDuration = 300

const chunkUnavailableMarker = "[transcription unavailable for segment %d]"

// ErrTooLong reports audio exceeding Options.MaxDurationSeconds. Returned
// before any provider call, so a caller seeing it has paid for the download and
// the probe, never for STT. Callers map it onto their own domain failure.
var ErrTooLong = errors.New("audio exceeds the maximum duration")

// ErrUnmeasurable reports a file whose duration ffprobe could not read AND
// whose size exceeds Options.MaxBytesWhenUnmeasurable.
//
// Distinct from ErrTooLong on purpose: "too long" is a measured fact, this one
// is a refusal to gamble. The caller may well want to say something different
// to the user — the file may be fine and merely encoded oddly.
var ErrUnmeasurable = errors.New("audio duration could not be measured and the file is too large to accept blindly")

// Transcriber is the lib's entry point. Construct it with a Provider; it holds
// no product state. Safe for concurrent use.
type Transcriber struct {
	provider Provider
}

// New returns a Transcriber backed by the given STT Provider.
func New(provider Provider) *Transcriber {
	return &Transcriber{provider: provider}
}

// Transcribe localizes the source, decides single-shot vs chunked based on file
// size and duration, transcribes (chunked runs in parallel up to MaxParallel
// with retry/backoff/jitter), and returns timed Segments. It performs NO I/O
// beyond ffmpeg + the provider: no events, no DB.
func (t *Transcriber) Transcribe(ctx context.Context, src Source, opts Options) (*Result, error) {
	if src == nil {
		return nil, fmt.Errorf("source is required")
	}
	opts = withDefaults(opts)

	path, cleanup, err := src.Localize(ctx)
	if err != nil {
		return nil, fmt.Errorf("localize source: %w", err)
	}
	defer cleanup()

	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat audio file: %w", err)
	}
	fileSize := fi.Size()
	maxSize := t.provider.MaxFileSize()

	// Probe duration to route long files to chunking even when under MaxFileSize.
	// The probed value is also the authoritative billing basis, carried out on the
	// Result so callers never reconstruct length from segment offsets (which omit
	// the final segment). durErr → duration stays 0, meaning "unknown".
	var duration float64
	var durErr error
	if opts.RequireMeasurableAudio {
		// One ffprobe run answers both questions — is there audio, and how long
		// — so the gate costs nothing over the measurement it replaces.
		info, err := Probe(ctx, path, opts.FFprobePath)
		if err != nil {
			return nil, err
		}
		duration = info.DurationSeconds
	} else {
		duration, durErr = ProbeDuration(ctx, opts.FFprobePath, path)
		if durErr != nil {
			duration = 0
		}
	}

	// Cost gate, placed here because this is the first point where the duration
	// is a MEASUREMENT rather than a claim: the file is local and probed, and no
	// provider call has been made yet.
	//
	// A failed probe (duration 0) falls through deliberately. Refusing what we
	// could not measure would reject legitimate audio whenever ffprobe stumbles,
	// trading a cost risk for a correctness one.
	if opts.MaxDurationSeconds > 0 && durErr == nil && duration > opts.MaxDurationSeconds {
		return nil, fmt.Errorf("%w: %.0fs > %.0fs", ErrTooLong, duration, opts.MaxDurationSeconds)
	}

	// Fallback bound for the case the duration cap cannot judge. A file we could
	// not measure is accepted on the assumption it is ordinary — this is the
	// limit of that assumption, using the one figure we always have.
	if durErr != nil && opts.MaxBytesWhenUnmeasurable > 0 && fileSize > opts.MaxBytesWhenUnmeasurable {
		return nil, fmt.Errorf("%w: %d bytes > %d (probe failed: %v)",
			ErrUnmeasurable, fileSize, opts.MaxBytesWhenUnmeasurable, durErr)
	}

	mustChunk := fileSize > maxSize || (durErr == nil && int(duration) > opts.MaxSingleShotDuration)
	if mustChunk {
		res, err := t.transcribeChunked(ctx, path, opts)
		if err != nil {
			return nil, err
		}
		res.DurationSeconds = duration
		res.ProbeError = durErr
		return res, nil
	}

	res, err := t.transcribeFile(ctx, path, opts)
	if err != nil {
		return nil, err
	}
	// Single-shot: the whole file starts at offset 0, so the provider's
	// segment timings are already absolute.
	return &Result{Segments: segmentsForChunk(res, 0, duration), DurationSeconds: duration, ProbeError: durErr, Language: res.Language}, nil
}

func withDefaults(o Options) Options {
	if o.ChunkDuration <= 0 {
		o.ChunkDuration = DefaultChunkDuration
	}
	if o.MaxParallel <= 0 {
		o.MaxParallel = 1
	}
	if o.MaxSingleShotDuration <= 0 {
		o.MaxSingleShotDuration = DefaultMaxSingleShotDuration
	}
	return o
}

// transcribeFile sends one whole file to the provider in a single request,
// returning the full provider result (text + any timed segments).
func (t *Transcriber) transcribeFile(ctx context.Context, path string, opts Options) (*TranscribeResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audio file: %w", err)
	}
	defer f.Close()

	return t.provider.Transcribe(ctx, TranscribeRequest{
		FileName:   filepath.Base(path),
		File:       f,
		Language:   opts.Language,
		SourceID:   opts.SourceID,
		UserID:     opts.UserID,
		APITokenID: opts.APITokenID,
	})
}

// segmentsForChunk maps one chunk's provider result onto output Segments,
// re-offsetting the provider's relative timings to absolute time by adding the
// chunk's start offset (seconds). When the provider returned no timed segments,
// it emits a single segment spanning the chunk: Start at the offset, End at the
// next boundary (offset+chunkDuration), or 0 when unknown (single-shot).
func segmentsForChunk(res *TranscribeResult, chunkStartSeconds int, chunkEndSeconds float64) []Segment {
	off := float64(chunkStartSeconds)
	if len(res.Segments) == 0 {
		// No provider timing: one coarse segment spanning the chunk. HasTiming
		// stays false so consumers know Start/End are chunk offsets, not real.
		return []Segment{{
			Text:         res.Text,
			StartSeconds: chunkStartSeconds,
			Start:        off,
			End:          chunkEndSeconds,
		}}
	}
	out := make([]Segment, len(res.Segments))
	for i, s := range res.Segments {
		out[i] = Segment{
			Text:         s.Text,
			StartSeconds: chunkStartSeconds,
			Start:        off + s.Start,
			End:          off + s.End,
			HasTiming:    true,
		}
	}
	return out
}

// transcribeChunked splits the file and transcribes chunks in parallel (bounded
// by MaxParallel), each with retry/backoff/jitter. Up to maxFailedChunkRatio of
// chunks may fail before the whole call fails; otherwise surviving text is kept
// and gaps are filled with a marker (graceful degradation).
func (t *Transcriber) transcribeChunked(ctx context.Context, path string, opts Options) (*Result, error) {
	split, err := SplitAudio(ctx, opts.FFmpegPath, path, opts.ChunkDuration)
	if err != nil {
		// Split failed: fall back to a single-shot attempt on the whole file.
		res, ferr := t.transcribeFile(ctx, path, opts)
		if ferr != nil {
			return nil, ferr
		}
		return &Result{Segments: segmentsForChunk(res, 0, 0), Language: res.Language}, nil
	}
	defer split.Cleanup()

	n := len(split.Chunks)
	results := make([]*TranscribeResult, n)
	errs := make([]error, n)

	sem := make(chan struct{}, opts.MaxParallel)
	done := make(chan struct{})
	var pending int
	for i, chunk := range split.Chunks {
		pending++
		go func(i int, chunkPath string) {
			defer func() { done <- struct{}{} }()
			sem <- struct{}{}
			defer func() { <-sem }()

			res, err := t.transcribeChunkWithRetry(ctx, i, n, chunkPath, opts)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = res
		}(i, chunk.FilePath)
	}
	for ; pending > 0; pending-- {
		<-done
	}

	var failed []int
	for i, e := range errs {
		if e != nil {
			failed = append(failed, i)
		}
	}
	if len(failed) > 0 {
		ratio := float64(len(failed)) / float64(n)
		if len(failed) == n || ratio > maxFailedChunkRatio {
			return nil, fmt.Errorf("%d/%d chunks failed transcription: chunks %v", len(failed), n, failed)
		}
		for _, i := range failed {
			results[i] = &TranscribeResult{Text: fmt.Sprintf(chunkUnavailableMarker, i)}
		}
	}

	// Flatten each chunk's segments into the output, re-offsetting relative
	// provider timings to absolute time. Chunk i starts at i*ChunkDuration and
	// ends at the next boundary.
	var segments []Segment
	// The spoken language does not change partway through a recording, so the
	// first chunk that reported one speaks for the whole file. Failed chunks
	// carry no detection, hence the skip rather than indexing results[0].
	var language string
	for i, res := range results {
		if language == "" && res != nil {
			language = res.Language
		}
		start := i * opts.ChunkDuration
		end := float64((i + 1) * opts.ChunkDuration)
		segments = append(segments, segmentsForChunk(res, start, end)...)
	}
	return &Result{Segments: segments, FailedChunks: failed, Language: language}, nil
}

// transcribeChunkWithRetry transcribes one chunk with exponential backoff and
// full jitter, honoring Retry-After on HTTP 429 (which also gets a wider budget).
// The sleep is context-aware.
func (t *Transcriber) transcribeChunkWithRetry(ctx context.Context, idx, total int, chunkPath string, opts Options) (*TranscribeResult, error) {
	fileName := filepath.Base(chunkPath)
	maxRetries := maxRetriesDefault
	var lastErr error

	for attempt := 1; ; attempt++ {
		f, err := os.Open(chunkPath)
		if err != nil {
			return nil, fmt.Errorf("chunk %d: open: %w", idx, err)
		}
		res, err := t.provider.Transcribe(ctx, TranscribeRequest{
			FileName:   fileName,
			File:       f,
			Language:   opts.Language,
			SourceID:   opts.SourceID,
			UserID:     opts.UserID,
			APITokenID: opts.APITokenID,
		})
		f.Close()

		if err == nil {
			return res, nil
		}
		lastErr = err

		var rle *RateLimitError
		isRateLimited := errors.As(err, &rle)
		if isRateLimited && maxRetries < maxRetriesRateLimited {
			maxRetries = maxRetriesRateLimited
		}
		if attempt >= maxRetries {
			break
		}

		delay := backoffDelay(attempt)
		if isRateLimited && rle.RetryAfter > 0 {
			delay = rle.RetryAfter
		}
		if err := sleepCtx(ctx, delay); err != nil {
			return nil, fmt.Errorf("chunk %d: %w", idx, err)
		}
	}
	return nil, fmt.Errorf("chunk %d: all %d attempts failed: %w", idx, maxRetries, lastErr)
}

// backoffDelay computes exponential backoff with full jitter for the given
// 1-based attempt, capped at retryMaxDelay.
func backoffDelay(attempt int) time.Duration {
	exp := retryBaseDelay << (attempt - 1)
	if exp > retryMaxDelay || exp <= 0 {
		exp = retryMaxDelay
	}
	return time.Duration(rand.Int63n(int64(exp) + 1))
}

// sleepCtx sleeps for d or returns ctx.Err() if the context is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	tm := time.NewTimer(d)
	defer tm.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tm.C:
		return nil
	}
}

// JoinText concatenates segment texts in order — a convenience for callers that
// only need the flat transcript.
func JoinText(segs []Segment) string {
	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = s.Text
	}
	return strings.Join(parts, " ")
}
