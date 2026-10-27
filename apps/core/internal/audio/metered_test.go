package audio

import (
	"context"
	"errors"
	"testing"

	"github.com/lalternativefabrique/vvaves/core/internal/costs"
)

type fakeProvider struct {
	pieces int
	err    error
}

func (f fakeProvider) Synthesize(context.Context, string, string) ([]byte, string, error) {
	return []byte("a"), "audio/mpeg", f.err
}

func (f fakeProvider) SynthesizeStream(_ context.Context, _, _ string, emit func([]byte) error) (string, error) {
	for range f.pieces {
		if err := emit([]byte("a")); err != nil {
			return "", err
		}
	}
	return "audio/mpeg", f.err
}

type lines []costs.Line

func (l *lines) Record(_ context.Context, line costs.Line) error {
	*l = append(*l, line)
	return nil
}

func TestCostCodeNamesTheProviderAndModel(t *testing.T) {
	cases := map[[2]string]string{
		{"mistral", ""}:                          "mistral/voxtral-mini-tts-latest.chars",
		{"elevenlabs", ""}:                       "elevenlabs/eleven_flash_v2_5.chars",
		{"elevenlabs", "eleven_multilingual_v2"}: "elevenlabs/eleven_multilingual_v2.chars",
		{"", "piper"}:                            "",
	}
	for in, want := range cases {
		if got := CostCode(in[0], in[1]); got != want {
			t.Errorf("CostCode(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestMeteredCountsRunesNotBytes(t *testing.T) {
	var got lines
	p := Metered(fakeProvider{}, "c.chars", &got)
	if _, _, err := p.Synthesize(context.Background(), "élève", "lalter"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Quantity != 5 || got[0].ExternalUserID != "lalter" {
		t.Fatalf("lines = %+v", got)
	}
}

func TestMeteredTakesTheClientFromTheContextWithoutBillTo(t *testing.T) {
	var got lines
	p := Metered(fakeProvider{}, "c.chars", &got)
	_, _, _ = p.Synthesize(costs.WithClient(context.Background(), "synthiz"), "abc", "")
	if len(got) != 1 || got[0].ExternalUserID != "synthiz" {
		t.Fatalf("lines = %+v", got)
	}
}

func TestMeteredDeclaresNothingForAFailedCall(t *testing.T) {
	var got lines
	p := Metered(fakeProvider{err: errors.New("down")}, "c.chars", &got)
	_, _, _ = p.Synthesize(context.Background(), "abc", "x")
	_, _ = p.SynthesizeStream(context.Background(), "abc", "x", func([]byte) error { return nil })
	if len(got) != 0 {
		t.Fatalf("lines = %+v, want none", got)
	}
}

func TestMeteredDeclaresAStreamTheListenerLeft(t *testing.T) {
	var got lines
	p := Metered(fakeProvider{pieces: 2}, "c.chars", &got)
	left := errors.New("listener left")
	_, _ = p.SynthesizeStream(context.Background(), "abc", "x", func([]byte) error { return left })
	if len(got) != 1 {
		t.Fatalf("lines = %+v, want the paid pieces declared", got)
	}
}

func TestMeteredGivesEachCallItsOwnKey(t *testing.T) {
	var got lines
	p := Metered(fakeProvider{}, "c.chars", &got)
	_, _, _ = p.Synthesize(context.Background(), "abc", "x")
	_, _, _ = p.Synthesize(context.Background(), "abc", "x")
	if len(got) != 2 || got[0].IdempotencyKey == got[1].IdempotencyKey {
		t.Fatalf("lines = %+v, want two distinct keys", got)
	}
}

func TestMeteredIsOffWithoutACode(t *testing.T) {
	inner := fakeProvider{}
	if p := Metered(inner, "", &lines{}); p != inner {
		t.Fatal("want the provider unwrapped")
	}
}

func TestMeteredDeclaresTheEndUserThePayerNamed(t *testing.T) {
	var got lines
	p := Metered(fakeProvider{}, "mistral/x.chars", &got)
	payer := costs.Payer{Client: "ak_synthiz", EndUser: "u42"}
	if _, _, err := p.Synthesize(context.Background(), "hello", payer.String()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ExternalUserID != "ak_synthiz" || got[0].EndUserID != "u42" {
		t.Fatalf("lines = %+v", got)
	}
}
