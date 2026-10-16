package transcribe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProvider is an in-memory Provider for testing the orchestrator without a
// real STT backend. Per-call behavior is driven by responder.
type fakeProvider struct {
	maxFileSize int64
	needsWAV    bool
	mu          sync.Mutex
	calls       int32
	// responder maps the call's filename -> (text, err). When nil, returns OK.
	responder func(call int, req TranscribeRequest) (string, error)
}

func (p *fakeProvider) MaxFileSize() int64       { return p.maxFileSize }
func (p *fakeProvider) NeedsWAVConversion() bool { return p.needsWAV }

func (p *fakeProvider) Transcribe(_ context.Context, req TranscribeRequest) (*TranscribeResult, error) {
	n := atomic.AddInt32(&p.calls, 1)
	if p.responder != nil {
		text, err := p.responder(int(n), req)
		if err != nil {
			return nil, err
		}
		return &TranscribeResult{Text: text}, nil
	}
	return &TranscribeResult{Text: "ok"}, nil
}

// writeTempAudio creates a small file standing in for an audio payload. Single-
// shot routing only stats it, so the bytes don't matter.
func writeTempAudio(t *testing.T, size int) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "audio.mp3")
	if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTranscribe_SingleShot(t *testing.T) {
	path := writeTempAudio(t, 10)
	// Large MaxFileSize + missing ffprobe (override to a non-existent bin so the
	// duration probe fails) => single-shot route.
	prov := &fakeProvider{maxFileSize: 1 << 30, responder: func(_ int, _ TranscribeRequest) (string, error) {
		return "hello world", nil
	}}
	tr := New(prov)

	res, err := tr.Transcribe(context.Background(), FileSource{Path: path}, Options{FFprobePath: "/nonexistent/ffprobe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Segments) != 1 || res.Segments[0].Text != "hello world" {
		t.Fatalf("got %+v", res.Segments)
	}
	if res.Segments[0].StartSeconds != 0 {
		t.Fatalf("single-shot offset must be 0, got %d", res.Segments[0].StartSeconds)
	}
}

// Fix #4 — the probed duration must be carried on Result so callers bill the
// FULL audio length rather than reconstructing it from segment offsets (which
// omit the final segment, and are 0 for single-shot audio).
func TestTranscribe_CarriesProbedDuration(t *testing.T) {
	path := writeTempAudio(t, 10)
	ffprobe := fakeFFprobe(t, 95) // 95s file, under the single-shot threshold
	prov := &fakeProvider{maxFileSize: 1 << 30, responder: func(_ int, _ TranscribeRequest) (string, error) {
		return "hi", nil
	}}
	tr := New(prov)

	res, err := tr.Transcribe(context.Background(), FileSource{Path: path}, Options{FFprobePath: ffprobe})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Single-shot: the only segment starts at 0, so a segment-offset estimate would
	// report 0. The probed duration must survive.
	if res.DurationSeconds != 95 {
		t.Fatalf("DurationSeconds = %v, want 95 (probed)", res.DurationSeconds)
	}
}

// When ffprobe is unavailable the probe fails; DurationSeconds must be 0 so the
// caller knows the length is unknown and can fall back, not silently bill 0.
func TestTranscribe_NoProbe_DurationZero(t *testing.T) {
	path := writeTempAudio(t, 10)
	prov := &fakeProvider{maxFileSize: 1 << 30, responder: func(_ int, _ TranscribeRequest) (string, error) {
		return "hi", nil
	}}
	tr := New(prov)

	res, err := tr.Transcribe(context.Background(), FileSource{Path: path}, Options{FFprobePath: "/nonexistent/ffprobe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DurationSeconds != 0 {
		t.Fatalf("DurationSeconds = %v, want 0 when probe unavailable", res.DurationSeconds)
	}
}

// Retry lives on the chunked path (transcribeChunkWithRetry); these tests drive
// it through a single chunk so the budget logic is exercised end-to-end.
func TestTranscribe_RetryThenSuccess(t *testing.T) {
	path := writeTempAudio(t, 10)
	ffmpeg := fakeFFmpeg(t, 1)
	ffprobe := fakeFFprobe(t, 1200) // force chunked route
	var attempts int32
	prov := &fakeProvider{maxFileSize: 1 << 30, responder: func(_ int, _ TranscribeRequest) (string, error) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			return "", errors.New("transient")
		}
		return "recovered", nil
	}}
	tr := New(prov)

	res, err := tr.Transcribe(context.Background(), FileSource{Path: path}, Options{
		MaxParallel: 1, ChunkDuration: 300, FFmpegPath: ffmpeg, FFprobePath: ffprobe,
	})
	if err != nil {
		t.Fatalf("expected recovery, got error: %v", err)
	}
	if res.Segments[0].Text != "recovered" {
		t.Fatalf("got %q", res.Segments[0].Text)
	}
	if attempts < 3 {
		t.Fatalf("expected >=3 attempts, got %d", attempts)
	}
}

func TestTranscribe_RateLimitWidensBudget(t *testing.T) {
	path := writeTempAudio(t, 10)
	ffmpeg := fakeFFmpeg(t, 1)
	ffprobe := fakeFFprobe(t, 1200)
	var attempts int32
	prov := &fakeProvider{maxFileSize: 1 << 30, responder: func(_ int, _ TranscribeRequest) (string, error) {
		// Fail with 429 the first 4 times; default budget (3) would give up,
		// the widened rate-limit budget (5) must let attempt 5 succeed.
		if atomic.AddInt32(&attempts, 1) <= 4 {
			return "", &RateLimitError{StatusCode: 429, RetryAfter: time.Millisecond}
		}
		return "after backoff", nil
	}}
	tr := New(prov)

	res, err := tr.Transcribe(context.Background(), FileSource{Path: path}, Options{
		MaxParallel: 1, ChunkDuration: 300, FFmpegPath: ffmpeg, FFprobePath: ffprobe,
	})
	if err != nil {
		t.Fatalf("expected success within widened budget, got: %v", err)
	}
	if res.Segments[0].Text != "after backoff" {
		t.Fatalf("got %q", res.Segments[0].Text)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if d := ParseRetryAfter("120", now); d != 120*time.Second {
		t.Fatalf("delta-seconds: got %s", d)
	}
	if d := ParseRetryAfter("", now); d != 0 {
		t.Fatalf("empty: got %s", d)
	}
	if d := ParseRetryAfter("0", now); d != 0 {
		t.Fatalf("zero: got %s", d)
	}
}

func TestJoinText(t *testing.T) {
	got := JoinText([]Segment{{Text: "a"}, {Text: "b"}, {Text: "c"}})
	if got != "a b c" {
		t.Fatalf("got %q", got)
	}
}
