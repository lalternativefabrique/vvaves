package transcribe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ffprobe reads this as a file with no streams at all, which is the shape of
// every input that is not media: a truncated download, an HTML error page
// saved as .mp3, a manifest.
func writeGarbage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "source.mp3")
	if err := os.WriteFile(path, []byte("not audio at all"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// With the gate off, an unmeasurable file falls through to the byte bound and
// is transcribed — the long-standing behaviour, which trades a cost risk for
// not refusing odd containers.
//
// With the gate on, it is refused before any provider call. That difference is
// the whole point of the option, so it is pinned here.
func TestRequireMeasurableAudio_GatesWhatTheByteBoundWouldAccept(t *testing.T) {
	if _, err := ffmpegBinary("", "ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	path := writeGarbage(t)

	_, err := Probe(context.Background(), path, "")
	if err == nil {
		t.Fatal("Probe accepted a file with no audio stream")
	}
	if !errors.Is(err, ErrNoAudioStream) && !errors.Is(err, ErrUnreadableMedia) && !errors.Is(err, ErrUnboundedDuration) {
		t.Errorf("error = %v, want one of the probe refusals", err)
	}
}

// The gate must not change what happens to a caller that did not ask for it:
// the legacy path keeps its fallback, and turning the option on is the only
// way to lose it.
func TestOptions_RequireMeasurableAudioDefaultsOff(t *testing.T) {
	var opts Options
	if opts.RequireMeasurableAudio {
		t.Error("RequireMeasurableAudio defaults to true; the legacy path would change behaviour")
	}
	if withDefaults(opts).RequireMeasurableAudio {
		t.Error("withDefaults turns the gate on")
	}
}
