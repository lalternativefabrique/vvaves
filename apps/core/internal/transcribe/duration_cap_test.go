package transcribe

import (
	"context"
	"errors"
	"testing"
)

// countingProvider records how many times the STT backend was actually called,
// which is the whole point of the cap: rejecting late still costs money, so the
// assertion is "the provider was never reached", not merely "an error came out".
type countingProvider struct {
	calls int
}

func (p *countingProvider) MaxFileSize() int64       { return 1 << 30 }
func (p *countingProvider) NeedsWAVConversion() bool { return false }

func (p *countingProvider) Transcribe(context.Context, TranscribeRequest) (*TranscribeResult, error) {
	p.calls++
	return &TranscribeResult{Text: "hi"}, nil
}

// Over-long audio must be refused BEFORE any provider call. A cap that fires
// after transcription protects the invoice but not the bill we already paid.
func TestTranscribe_OverCap_RejectsBeforePayingForSTT(t *testing.T) {
	path := writeTempAudio(t, 10)
	ffprobe := fakeFFprobe(t, 9000) // 2h30
	prov := &countingProvider{}
	tr := New(prov)

	_, err := tr.Transcribe(context.Background(), FileSource{Path: path}, Options{
		FFprobePath:        ffprobe,
		MaxDurationSeconds: 7200, // 2h
	})

	if !errors.Is(err, ErrTooLong) {
		t.Fatalf("error = %v, want ErrTooLong", err)
	}
	if prov.calls != 0 {
		t.Errorf("provider called %d time(s), want 0 — STT was paid for on rejected audio", prov.calls)
	}
}

// Exactly at the cap is accepted: the limit is a maximum, not an exclusive
// bound, so a file advertised as "2h max" must not fail at 2h.
func TestTranscribe_ExactlyAtCap_Transcribes(t *testing.T) {
	path := writeTempAudio(t, 10)
	ffprobe := fakeFFprobe(t, 7200)
	prov := &countingProvider{}
	tr := New(prov)

	if _, err := tr.Transcribe(context.Background(), FileSource{Path: path}, Options{
		FFprobePath:           ffprobe,
		MaxSingleShotDuration: 99999, // keep it single-shot, one provider call
		MaxDurationSeconds:    7200,
	}); err != nil {
		t.Fatalf("unexpected error at exactly the cap: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called — audio at the cap must still be transcribed")
	}
}

// The cap is opt-in: callers that never set it keep transcribing any length,
// so adding the field cannot silently break an existing caller.
func TestTranscribe_NoCap_TranscribesAnyLength(t *testing.T) {
	path := writeTempAudio(t, 10)
	ffprobe := fakeFFprobe(t, 36000) // 10h
	prov := &countingProvider{}
	tr := New(prov)

	if _, err := tr.Transcribe(context.Background(), FileSource{Path: path}, Options{
		FFprobePath:           ffprobe,
		MaxSingleShotDuration: 99999,
	}); err != nil {
		t.Fatalf("unexpected error with no cap set: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called — an unset cap must not reject anything")
	}
}

// An unmeasurable file falls through rather than being rejected. Refusing what
// ffprobe could not read would turn a probe hiccup into a transcription
// failure, trading a cost risk for a correctness one.
func TestTranscribe_UnknownDuration_FallsThrough(t *testing.T) {
	path := writeTempAudio(t, 10)
	prov := &countingProvider{}
	tr := New(prov)

	if _, err := tr.Transcribe(context.Background(), FileSource{Path: path}, Options{
		FFprobePath:        "/nonexistent/ffprobe",
		MaxDurationSeconds: 7200,
	}); err != nil {
		t.Fatalf("unexpected error when duration is unknown: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called — an unmeasurable file must not be rejected by the cap")
	}
}
