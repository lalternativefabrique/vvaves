package transcribe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DefaultChunkDuration is the target duration per audio chunk, in seconds.
const DefaultChunkDuration = 300 // 5 minutes

// AudioChunk is a single audio chunk produced by splitting, tagged with its
// 0-based index (chunk i starts at i*chunkDuration seconds).
type AudioChunk struct {
	FilePath string
	Index    int
}

// SplitResult holds the output of an audio split operation. Cleanup removes the
// temp directory holding the chunks; callers must defer it.
type SplitResult struct {
	Chunks  []AudioChunk
	TmpDir  string
	Cleanup func()
}

// ffmpegPath resolves the ffmpeg/ffprobe binary, allowing an override for tests.
func ffmpegBinary(override, name string) (string, error) {
	if override != "" {
		return override, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not installed", name)
	}
	return p, nil
}

// ProbeDuration returns the duration of an audio file in seconds using ffprobe.
func ProbeDuration(ctx context.Context, ffprobeOverride, filePath string) (float64, error) {
	ffprobePath, err := ffmpegBinary(ffprobeOverride, "ffprobe")
	if err != nil {
		return 0, err
	}

	var stdout, stderr strings.Builder
	cmd := exec.CommandContext(
		ctx,
		ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		"--", filePath,
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(stdout.String()), 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %w", err)
	}
	return duration, nil
}

// SplitAudio splits an audio file into chunks of chunkDurationSec seconds using
// stream-copy (-c:a copy): it keeps the original codec/container and avoids a
// full re-encode. The output extension matches the input so ffmpeg's segment
// muxer infers the right format. Chunks are returned sorted by index.
//
// ffmpegOverride lets tests inject a fake binary; empty resolves via PATH.
func SplitAudio(ctx context.Context, ffmpegOverride, inputPath string, chunkDurationSec int) (*SplitResult, error) {
	ffmpegPath, err := ffmpegBinary(ffmpegOverride, "ffmpeg")
	if err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "transcribe-chunks-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	ext := strings.ToLower(filepath.Ext(inputPath))
	if ext == "" {
		ext = ".mp3"
	}
	outputPattern := filepath.Join(tmpDir, "chunk_%03d"+ext)
	globPattern := filepath.Join(tmpDir, "chunk_*"+ext)

	var stderr strings.Builder
	cmd := exec.CommandContext(
		ctx,
		ffmpegPath,
		"-y",
		"-i", inputPath,
		"-vn",
		"-f", "segment",
		"-segment_time", strconv.Itoa(chunkDurationSec),
		"-c:a", "copy",
		"-reset_timestamps", "1",
		outputPattern,
	)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return nil, fmt.Errorf("ffmpeg segment failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	matches, err := filepath.Glob(globPattern)
	if err != nil || len(matches) == 0 {
		cleanup()
		return nil, fmt.Errorf("no chunks produced after splitting")
	}

	sort.Strings(matches)
	chunks := make([]AudioChunk, len(matches))
	for i, path := range matches {
		chunks[i] = AudioChunk{FilePath: path, Index: i}
	}

	return &SplitResult{Chunks: chunks, TmpDir: tmpDir, Cleanup: cleanup}, nil
}
