package audio

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/lalternative/packages/go/tts"
)

const mistralBaseURL = "https://api.mistral.ai"

// NewMistralVoice reads through Mistral's /v1/audio/speech, which differs
// from the OpenAI protocol tts.OpenAIVoice speaks only on the wire: the voice
// is sent as voice_id, and the audio comes back base64-encoded in a JSON
// body. Translating that in the transport keeps the cutting, the parallel
// reading and the ordering in the one place that already gets them right.
func NewMistralVoice(cfg tts.Config) *tts.OpenAIVoice {
	if cfg.BaseURL == "" {
		cfg.BaseURL = mistralBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = "voxtral-mini-tts-latest"
	}
	next := http.DefaultTransport
	if cfg.Client != nil && cfg.Client.Transport != nil {
		next = cfg.Client.Transport
	}
	cfg.Client = &http.Client{Transport: mistralTransport{next: next}}
	return tts.NewOpenAIVoice(cfg)
}

type mistralTransport struct {
	next http.RoundTripper
}

func (t mistralTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	format, out, err := mistralRequest(req)
	if err != nil {
		return nil, err
	}
	resp, err := t.next.RoundTrip(out)
	if err != nil || resp.StatusCode != http.StatusOK {
		return resp, err
	}
	if err := decodeMistralAudio(resp, format); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

func mistralRequest(req *http.Request) (format string, out *http.Request, err error) {
	raw, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return "", nil, fmt.Errorf("mistral: read request: %w", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", nil, fmt.Errorf("mistral: decode request: %w", err)
	}
	fields["voice_id"] = fields["voice"]
	delete(fields, "voice")
	fields["stream"] = false
	format, _ = fields["response_format"].(string)

	body, err := json.Marshal(fields)
	if err != nil {
		return "", nil, fmt.Errorf("mistral: encode request: %w", err)
	}
	out = req.Clone(req.Context())
	out.Body = io.NopCloser(bytes.NewReader(body))
	out.ContentLength = int64(len(body))
	out.Header.Set("Accept", "application/json")
	return format, out, nil
}

func decodeMistralAudio(resp *http.Response, format string) error {
	var payload struct {
		AudioData string `json:"audio_data"`
	}
	err := json.NewDecoder(resp.Body).Decode(&payload)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("mistral: decode response: %w", err)
	}
	audio, err := base64.StdEncoding.DecodeString(payload.AudioData)
	if err != nil {
		return fmt.Errorf("mistral: decode audio_data: %w", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(audio))
	resp.ContentLength = int64(len(audio))
	resp.Header.Set("Content-Type", tts.MIMEFor(format))
	resp.Header.Set("Content-Length", strconv.Itoa(len(audio)))
	return nil
}
