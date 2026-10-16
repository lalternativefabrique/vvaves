package transcribe

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Source is the audio origin adapter. The muscle (chunker/orchestrator) only ever sees a local file path,
// never the transport.
//
// Localize MUST materialize the audio as a seekable local file and return its
// path plus a cleanup func the caller defers. A seekable file is required
// because container muxers (m4a/mp4 keep their index at the end of the file)
// cannot be segmented from a non-seekable pipe.
type Source interface {
	Localize(ctx context.Context) (path string, cleanup func(), err error)
}

// Streamable is an optional capability a Source may implement when its format
// and transport tolerate streaming (e.g. raw mp3 over HTTP). The orchestrator
// uses it only when streaming is known to be safe; otherwise it falls back to
// Localize. Keeping it optional avoids forcing every adapter to support a mode
// that breaks for seek-dependent containers.
type Streamable interface {
	Open(ctx context.Context) (io.ReadCloser, error)
}

// URLSource localizes a direct audio URL via ffmpeg: the input is a direct audio
// stream URL. ffmpeg can read the URL
// natively, demux it, and (optionally) re-encode to WAV when the STT backend
// requires it.
type URLSource struct {
	// URL is the resolved, direct audio stream URL.
	URL string
	// WAV requests WAV PCM 16kHz mono output (whisper.cpp). When false the
	// original codec is kept via stream-copy — no re-encode, far cheaper.
	WAV bool
	// FFmpegPath overrides the ffmpeg binary lookup (tests). Empty → PATH.
	FFmpegPath string
	// FFprobePath overrides the ffprobe binary lookup (tests). Empty → PATH.
	FFprobePath string
}

// Localize downloads/normalizes the URL into a temp file. When WAV is false it
// stream-copies (-c:a copy) into a container matching the input's codec,
// avoiding a full re-encode. When WAV is true it produces WAV PCM 16kHz mono as
// whisper.cpp expects.
func (s URLSource) Localize(ctx context.Context) (string, func(), error) {
	ffmpegPath := s.FFmpegPath
	if ffmpegPath == "" {
		p, err := exec.LookPath("ffmpeg")
		if err != nil {
			return "", nil, fmt.Errorf("ffmpeg not installed")
		}
		ffmpegPath = p
	}

	tmpDir, err := os.MkdirTemp("", "transcribe-src-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	var outputFile string
	var args []string
	if s.WAV {
		outputFile = filepath.Join(tmpDir, "audio.wav")
		args = []string{"-y", "-i", s.URL, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "--", outputFile}
	} else {
		outputFile = filepath.Join(tmpDir, "audio"+s.copyExtension(ctx))
		args = []string{"-y", "-i", s.URL, "-vn", "-c:a", "copy", "--", outputFile}
	}

	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("ffmpeg download failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	if _, err := os.Stat(outputFile); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("downloaded audio file not found: %w", err)
	}

	return outputFile, cleanup, nil
}

// containerForCodec maps an audio codec to a container that can hold it under
// stream-copy. No single container fits every codec: MP4/iPod rejects mp3
// ("Could not find tag for codec mp3"), so a fixed output extension breaks
// whichever codec it does not cover.
var containerForCodec = map[string]string{
	"aac":       ".m4a",
	"alac":      ".m4a",
	"mp3":       ".mp3",
	"opus":      ".ogg",
	"vorbis":    ".ogg",
	"flac":      ".flac",
	"pcm_s16le": ".wav",
	"pcm_s16be": ".wav",
	"pcm_f32le": ".wav",
}

// copyExtension picks the output container for a stream-copy from the codec
// ffprobe reports for the input.
//
// The output extension MUST NOT be derived from the input URL: ffmpeg selects
// its muxer from that extension, so an HLS URL (.m3u8) made it re-emit a
// playlist plus .ts segments instead of one audio file. That "succeeded" —
// exit 0, file present — and the manifest (a few hundred bytes of text) was
// then sent to the STT provider, which rejected the extension minutes later.
//
// Falls back to .m4a, matching the STT client's own default, when the codec is
// unknown or ffprobe cannot read the input; ffmpeg then fails loudly on an
// incompatible pair rather than silently producing a non-audio file.
func (s URLSource) copyExtension(ctx context.Context) string {
	const fallback = ".m4a"
	ffprobePath, err := ffmpegBinary(s.FFprobePath, "ffprobe")
	if err != nil {
		return fallback
	}

	var stdout strings.Builder
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		"--", s.URL,
	)
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return fallback
	}

	codec := strings.ToLower(strings.TrimSpace(stdout.String()))
	if i := strings.IndexAny(codec, "\r\n"); i != -1 {
		codec = codec[:i]
	}
	if ext, ok := containerForCodec[codec]; ok {
		return ext
	}
	return fallback
}

// FileSource localizes audio already present on the local filesystem. Useful
// when a caller (or another adapter) has fetched the bytes — e.g. an S3 adapter
// that downloads to a temp file then hands the path off.
type FileSource struct {
	Path string
}

// Localize returns the existing path with a no-op cleanup (the file's lifecycle
// is owned by whoever created it).
func (s FileSource) Localize(_ context.Context) (string, func(), error) {
	if _, err := os.Stat(s.Path); err != nil {
		return "", nil, fmt.Errorf("file source not found: %w", err)
	}
	return s.Path, func() {}, nil
}
