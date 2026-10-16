package transcribe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeSizedAudio writes a file of exactly n bytes, so a test can drive the
// size bound without producing real audio.
func writeSizedAudio(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatalf("write temp audio: %v", err)
	}
	return path
}

// The gap this closes: an unprobeable file used to skip the duration cap
// entirely and go straight to STT, however large. Size is the only figure left
// once ffprobe fails, and it is enough to refuse the absurd.
func TestTranscribe_UnmeasurableAndOversized_RejectsBeforeSTT(t *testing.T) {
	prov := &countingProvider{}
	tr := New(prov)

	_, err := tr.Transcribe(context.Background(), FileSource{Path: writeSizedAudio(t, 4096)}, Options{
		FFprobePath:              "/nonexistent/ffprobe", // probe fails → duration unknown
		MaxDurationSeconds:       7200,
		MaxBytesWhenUnmeasurable: 1024,
	})

	if !errors.Is(err, ErrUnmeasurable) {
		t.Fatalf("error = %v, want ErrUnmeasurable", err)
	}
	if errors.Is(err, ErrTooLong) {
		t.Error("reported as ErrTooLong; the two must stay distinct — nothing was measured")
	}
	if prov.calls != 0 {
		t.Errorf("provider called %d time(s), want 0", prov.calls)
	}
}

// An unmeasurable file under the size bound is still transcribed. Refusing
// everything ffprobe cannot read would fail legitimate audio in an unusual
// container, which is the failure this guard is designed NOT to cause.
func TestTranscribe_UnmeasurableButSmall_StillTranscribes(t *testing.T) {
	prov := &countingProvider{}
	tr := New(prov)

	res, err := tr.Transcribe(context.Background(), FileSource{Path: writeSizedAudio(t, 512)}, Options{
		FFprobePath:              "/nonexistent/ffprobe",
		MaxDurationSeconds:       7200,
		MaxBytesWhenUnmeasurable: 1024,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called — a small unmeasurable file must still be transcribed")
	}
	// The caller has to be able to tell "unknown" from "measured 0".
	if res.ProbeError == nil {
		t.Error("ProbeError = nil, want the probe failure surfaced so the caller can log it")
	}
}

// The size bound is a FALLBACK. A file whose duration was read is judged on
// that duration alone: bytes are a poor proxy for length, and a high-bitrate
// short file must not be refused for being heavy.
func TestTranscribe_MeasurableAndOversized_IgnoresSizeBound(t *testing.T) {
	prov := &countingProvider{}
	tr := New(prov)

	res, err := tr.Transcribe(context.Background(), FileSource{Path: writeSizedAudio(t, 4096)}, Options{
		FFprobePath:              fakeFFprobe(t, 60), // 1 min, well under the cap
		MaxSingleShotDuration:    99999,
		MaxDurationSeconds:       7200,
		MaxBytesWhenUnmeasurable: 1024, // exceeded, but must not apply
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called — a measured file must not be judged on its size")
	}
	if res.ProbeError != nil {
		t.Errorf("ProbeError = %v, want nil when the duration was measured", res.ProbeError)
	}
}

// Opt-in: callers that leave the bound unset keep accepting any unmeasurable
// file, so adding the field breaks no existing caller.
func TestTranscribe_NoSizeBound_AcceptsUnmeasurable(t *testing.T) {
	prov := &countingProvider{}
	tr := New(prov)

	if _, err := tr.Transcribe(context.Background(), FileSource{Path: writeSizedAudio(t, 1<<20)}, Options{
		FFprobePath:        "/nonexistent/ffprobe",
		MaxDurationSeconds: 7200,
	}); err != nil {
		t.Fatalf("unexpected error with no size bound set: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called — an unset bound must reject nothing")
	}
}
