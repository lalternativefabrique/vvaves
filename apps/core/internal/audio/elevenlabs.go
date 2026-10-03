package audio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/lalternative/packages/go/tts"
)

const elevenLabsBaseURL = "https://api.elevenlabs.io"

// NewElevenLabsVoice reads through ElevenLabs' /v1/text-to-speech/{voice_id}.
// It answers raw audio like the OpenAI protocol, but takes the voice in the
// path, the key in xi-api-key and the format in the query; the transport
// rewrites the request so tts.OpenAIVoice keeps the cutting and ordering.
func NewElevenLabsVoice(cfg tts.Config) (*tts.OpenAIVoice, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = elevenLabsBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = "eleven_multilingual_v2"
	}
	if cfg.Format == "" {
		cfg.Format = "mp3"
	}
	outputFormat, ok := elevenLabsFormats[cfg.Format]
	if !ok {
		return nil, fmt.Errorf("elevenlabs: unsupported format %q", cfg.Format)
	}
	next := http.DefaultTransport
	if cfg.Client != nil && cfg.Client.Transport != nil {
		next = cfg.Client.Transport
	}
	cfg.Client = &http.Client{Transport: elevenLabsTransport{next: next, apiKey: cfg.APIKey, outputFormat: outputFormat}}
	cfg.APIKey = ""
	return tts.NewOpenAIVoice(cfg), nil
}

var elevenLabsFormats = map[string]string{
	"mp3":  "mp3_44100_128",
	"opus": "opus_48000_128",
}

type elevenLabsTransport struct {
	next         http.RoundTripper
	apiKey       string
	outputFormat string
}

func (t elevenLabsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: read request: %w", err)
	}
	var in struct {
		Model string `json:"model"`
		Input string `json:"input"`
		Voice string `json:"voice"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("elevenlabs: decode request: %w", err)
	}
	body, err := json.Marshal(map[string]string{"text": in.Input, "model_id": in.Model})
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: encode request: %w", err)
	}

	out := req.Clone(req.Context())
	out.URL.Path = "/v1/text-to-speech/" + url.PathEscape(in.Voice)
	out.URL.RawPath = ""
	out.URL.RawQuery = url.Values{"output_format": {t.outputFormat}}.Encode()
	out.Body = io.NopCloser(bytes.NewReader(body))
	out.ContentLength = int64(len(body))
	out.Header.Del("Authorization")
	out.Header.Set("xi-api-key", t.apiKey)
	out.Header.Set("Accept", "audio/*")
	return t.next.RoundTrip(out)
}
