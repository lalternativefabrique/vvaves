package audio

import (
	"context"

	"github.com/lalternative/packages/go/audioreader"
	"github.com/lalternative/packages/go/tts"
)

// provider adapts tts.Voice's Speak/SpeakStream names to the
// Synthesize/SynthesizeStream shape audioreader.Provider expects.
//
// The billTo argument is dropped here: Metered, wrapped around this provider,
// is what declares it.
type provider struct {
	voice tts.Voice
}

var _ audioreader.Provider = provider{}

// NewProvider returns nil when there is no voice, which is how the feature
// stays absent rather than half-present: audioreader.NewReader answers nil in
// turn, and the handlers report speech as unconfigured.
func NewProvider(voice tts.Voice) audioreader.Provider {
	if voice == nil {
		return nil
	}
	return provider{voice: voice}
}

func (p provider) Synthesize(ctx context.Context, text, _ string) ([]byte, string, error) {
	return p.voice.Speak(ctx, text)
}

func (p provider) SynthesizeStream(ctx context.Context, text, _ string, emit func([]byte) error) (string, error) {
	return p.voice.SpeakStream(ctx, text, emit)
}
