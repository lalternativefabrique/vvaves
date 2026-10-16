// Package transcribe is the reusable speech-to-text "muscle": it downloads
// audio from a pluggable Source, splits long files into chunks with ffmpeg,
// transcribes the chunks in parallel against a pluggable STT Provider (with
// retry/backoff/jitter and graceful degradation), and returns timed segments.
//
// It is deliberately free of any product-specific concern (no S3, no HTTP
// routing, no DB, no NATS): the origin of the audio is an adapter (see Source)
// and the STT backend is an adapter (see Provider).
package transcribe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// TranscribeRequest holds the parameters for a single provider call (one chunk
// or one whole file).
type TranscribeRequest struct {
	FileName string
	File     io.Reader
	Language string
	// AudioSeconds is the measured duration of this audio payload. Optional —
	// providers that bill per second use it for usage accounting.
	AudioSeconds float64
	// SourceID / UserID / APITokenID are opaque attribution identifiers passed
	// straight through to the provider for cost tracking. The muscle never
	// interprets them.
	SourceID   string
	UserID     string
	APITokenID string
}

// ProviderSegment is one timed span returned by a provider for one payload,
// with Start/End in seconds RELATIVE to that payload's start (a chunk, or the
// whole file in single-shot). Empty when the backend returns plain text only.
type ProviderSegment struct {
	Text  string
	Start float64
	End   float64
}

// TranscribeResult holds the output a provider returns for one payload. Text is
// the full joined transcript. Segments carries timed spans when the backend
// exposed them (verbose_json); nil otherwise — the orchestrator then falls back
// to a single chunk-offset segment.
type TranscribeResult struct {
	Text     string
	Segments []ProviderSegment
	// Language is the ISO 639-1 code the backend detected in the audio, or ""
	// when it reported none. Distinct from Options.Language, which is a HINT
	// sent TO the backend: this is what the backend actually heard.
	Language string
}

// Provider abstracts a speech-to-text backend (OpenAI-compatible, whisper.cpp…).
//
//   - MaxFileSize bounds the payload a single request accepts (bytes); the
//     orchestrator splits inputs larger than this.
//   - NeedsWAVConversion reports whether the backend requires WAV PCM 16kHz
//     mono input (whisper.cpp) versus accepting compressed audio (OpenAI/Groq/
//     OVH). Callers use it to decide whether to re-encode at download time.
type Provider interface {
	Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResult, error)
	MaxFileSize() int64
	NeedsWAVConversion() bool
}

// RateLimitError is returned by a Provider when the upstream service responds
// with HTTP 429. RetryAfter carries the honored delay parsed from the
// Retry-After header (0 when absent/unparseable). Providers that wrap another
// provider must use %w so the orchestrator can detect it via errors.As.
type RateLimitError struct {
	RetryAfter time.Duration
	StatusCode int
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited: status=%d retry_after=%s", e.StatusCode, e.RetryAfter)
}

// ParseRetryAfter interprets a Retry-After header value relative to now. It
// supports both delta-seconds ("120") and an HTTP-date, returning 0 when the
// header is empty, unparseable, or points to a past instant.
func ParseRetryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}

	if secs, err := strconv.Atoi(header); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}

	if t, err := http.ParseTime(header); err == nil {
		d := t.Sub(now)
		if d <= 0 {
			return 0
		}
		return d
	}

	return 0
}
