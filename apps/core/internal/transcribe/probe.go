package transcribe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// probeTimeout bounds one ffprobe run. A crafted file can make it read for a
// very long time; nothing else in the pipeline puts a wall clock on it.
const probeTimeout = 2 * time.Minute

var (
	// ErrNoAudioStream reports a file ffprobe could open but which carries no
	// decodable audio.
	ErrNoAudioStream = errors.New("transcribe: no decodable audio stream")
	// ErrUnreadableMedia reports a file ffprobe could not parse at all.
	ErrUnreadableMedia = errors.New("transcribe: media could not be read")
	// ErrUnboundedDuration reports media whose length ffprobe does not report,
	// which is what a live stream looks like from here.
	ErrUnboundedDuration = errors.New("transcribe: media has no finite duration")
)

// MediaInfo is what a probe established about a local file.
type MediaInfo struct {
	Codec           string
	DurationSeconds float64
}

// Probe verifies that a local file really carries audio a decoder can read,
// and for how long.
//
// It is the only step that MEASURES. Everything upstream — an RSS enclosure's
// length attribute, a Content-Length header, yt-dlp's metadata — is a
// declaration by whoever produced the file, useful to refuse early and cheaply
// but never proof. This opens it.
//
// Unlike copyExtension, which picks an output container and deliberately falls
// back when it cannot tell, this returns an error: a caller that cannot
// establish there is audio of finite length must not pay for a transcription.
func Probe(ctx context.Context, path string, ffprobePath string) (MediaInfo, error) {
	bin, err := ffmpegBinary(ffprobePath, "ffprobe")
	if err != nil {
		return MediaInfo{}, fmt.Errorf("%w: %v", ErrUnreadableMedia, err)
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var stdout, stderr strings.Builder
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name:format=duration",
		"-of", "json",
		"--", path,
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return MediaInfo{}, fmt.Errorf("%w: %v", ErrUnreadableMedia, firstLine(stderr.String(), err))
	}

	info, err := parseProbe(stdout.String())
	if err != nil {
		return MediaInfo{}, err
	}
	return info, nil
}

type probeOutput struct {
	Streams []struct {
		CodecName string `json:"codec_name"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// parseProbe turns ffprobe's JSON into a verdict. Split out from Probe so the
// decision is testable without the binary.
func parseProbe(raw string) (MediaInfo, error) {
	var out probeOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return MediaInfo{}, fmt.Errorf("%w: %v", ErrUnreadableMedia, err)
	}

	// An empty stream list is ffprobe's answer for a file with no audio track:
	// a video with its sound stripped, or a container holding something else
	// entirely. Passing it to the decoder buys an empty transcript at full
	// price.
	if len(out.Streams) == 0 || strings.TrimSpace(out.Streams[0].CodecName) == "" {
		return MediaInfo{}, ErrNoAudioStream
	}

	// A live stream reports no duration. So does a file whose index ffprobe
	// could not read — both are media whose end nothing here can predict, and
	// both must be refused before a decoder starts reading a stream that never
	// ends.
	seconds, err := strconv.ParseFloat(strings.TrimSpace(out.Format.Duration), 64)
	if err != nil || seconds <= 0 {
		return MediaInfo{}, ErrUnboundedDuration
	}

	return MediaInfo{
		Codec:           strings.ToLower(strings.TrimSpace(out.Streams[0].CodecName)),
		DurationSeconds: seconds,
	}, nil
}

func firstLine(stderr string, fallback error) string {
	s := strings.TrimSpace(stderr)
	if s == "" {
		return fallback.Error()
	}
	if i := strings.IndexAny(s, "\r\n"); i != -1 {
		s = s[:i]
	}
	return s
}
