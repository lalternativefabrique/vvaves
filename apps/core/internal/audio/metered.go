package audio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"
	"unicode/utf8"

	"github.com/lalternative/packages/go/audioreader"

	"github.com/lalternativefabrique/vvaves/core/internal/costs"
)

// CostCode names the Lungor cost item a reading through provider and model is
// declared on, or "" for the self-hosted voice, which costs nothing per call.
func CostCode(provider, model string) string {
	if model == "" {
		switch provider {
		case "mistral":
			model = mistralModel
		case "elevenlabs":
			model = elevenLabsModel
		}
	}
	if provider == "" || model == "" {
		return ""
	}
	return provider + "/" + model + ".chars"
}

type metered struct {
	next     audioreader.Provider
	code     string
	recorder costs.Recorder
	now      func() time.Time
}

// Metered declares the characters of every call that reaches the provider.
// A cache hit never calls the provider, so it is never declared.
//
// The client comes from billTo, set by the reader, or from ctx: the primer
// passes no billTo, and the reader synthesizes on a background context.
func Metered(next audioreader.Provider, code string, recorder costs.Recorder) audioreader.Provider {
	if next == nil || code == "" || recorder == nil {
		return next
	}
	return metered{next: next, code: code, recorder: recorder, now: time.Now}
}

func (m metered) Synthesize(ctx context.Context, text, billTo string) ([]byte, string, error) {
	at := m.now()
	audio, mime, err := m.next.Synthesize(ctx, text, billTo)
	if err == nil {
		m.declare(ctx, text, billTo, at)
	}
	return audio, mime, err
}

// SynthesizeStream declares a reading the listener left halfway too: the
// pieces already read were paid for.
func (m metered) SynthesizeStream(ctx context.Context, text, billTo string, emit func([]byte) error) (string, error) {
	at := m.now()
	emitted := false
	mime, err := m.next.SynthesizeStream(ctx, text, billTo, func(piece []byte) error {
		emitted = true
		return emit(piece)
	})
	if err == nil || emitted {
		m.declare(ctx, text, billTo, at)
	}
	return mime, err
}

func (m metered) declare(ctx context.Context, text, billTo string, at time.Time) {
	n := utf8.RuneCountInString(text)
	if n == 0 {
		return
	}
	client := billTo
	if client == "" {
		client = costs.ClientFrom(ctx)
	}
	line := costs.Line{
		Code:           m.code,
		Quantity:       int64(n),
		OccurredAt:     at.UTC(),
		IdempotencyKey: callID() + ".chars",
		ExternalUserID: client,
	}
	// Detached: the reading is done, and a listener who left must not cancel
	// the record of what it cost.
	if err := m.recorder.Record(context.WithoutCancel(ctx), line); err != nil {
		log.Printf("vvaves: cost not recorded, %d chars on %s for %q: %v", n, m.code, client, err)
	}
}

func callID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
