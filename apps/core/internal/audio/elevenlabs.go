package audio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/lalternative/packages/go/tts"
)

const (
	elevenLabsBaseURL = "https://api.elevenlabs.io"
	elevenLabsModel   = "eleven_flash_v2_5"
	// Flash v2.5 takes 40,000 characters a request. Pieces stay well under so
	// a long text still reads in parallel, and since each one streams, their
	// size no longer delays the first sound.
	ElevenLabsMaxChars = 5000
)

// ElevenLabs is one account. Its plan caps concurrent requests across every
// voice, so the voices made from it share one set of slots: a request past
// the cap is refused with a 429, which would cut a reading short.
type ElevenLabs struct {
	baseURL string
	apiKey  string
	slots   chan struct{}
	next    http.RoundTripper
}

func NewElevenLabs(baseURL, apiKey string, maxConcurrent int) *ElevenLabs {
	if baseURL == "" {
		baseURL = elevenLabsBaseURL
	}
	return &ElevenLabs{
		baseURL: baseURL,
		apiKey:  apiKey,
		slots:   make(chan struct{}, max(maxConcurrent, 1)),
		next:    http.DefaultTransport,
	}
}

// Voice reads through /v1/text-to-speech/{voice_id}/stream, relaying its MP3
// as it is made. languageCode (ISO 639-1) enforces the language; empty lets
// the model detect it.
func (e *ElevenLabs) Voice(cfg tts.Config, languageCode string) (*tts.OpenAIVoice, error) {
	if cfg.Format == "" {
		cfg.Format = "mp3"
	}
	if cfg.Format != "mp3" {
		return nil, fmt.Errorf("elevenlabs: format %q cannot be streamed, only mp3", cfg.Format)
	}
	if cfg.Model == "" {
		cfg.Model = elevenLabsModel
	}
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = ElevenLabsMaxChars
	}
	cfg.BaseURL = e.baseURL
	cfg.APIKey = ""
	cfg.Client = &http.Client{Transport: elevenLabsTransport{account: e, languageCode: languageCode}}
	return tts.NewOpenAIVoice(cfg), nil
}

type elevenLabsTransport struct {
	account      *ElevenLabs
	languageCode string
}

func (t elevenLabsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	out, err := t.request(req)
	if err != nil {
		return nil, err
	}

	select {
	case t.account.slots <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	var once sync.Once
	release := func() { once.Do(func() { <-t.account.slots }) }

	resp, err := t.account.next.RoundTrip(out)
	if err != nil {
		release()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body = releasingBody{ReadCloser: resp.Body, release: release}
		return resp, nil
	}

	upstream := resp.Body
	pr, pw := io.Pipe()
	go func() {
		defer release()
		defer upstream.Close()
		pw.CloseWithError(framesFromMP3(upstream, pw))
	}()
	resp.Body = pr
	resp.ContentLength = -1
	resp.Header.Del("Content-Length")
	resp.Header.Set("Content-Type", tts.FramesContentType)
	return resp, nil
}

func (t elevenLabsTransport) request(req *http.Request) (*http.Request, error) {
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
	fields := map[string]string{"text": in.Input, "model_id": in.Model}
	if t.languageCode != "" {
		fields["language_code"] = t.languageCode
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: encode request: %w", err)
	}

	out := req.Clone(req.Context())
	out.URL.Path = "/v1/text-to-speech/" + url.PathEscape(in.Voice) + "/stream"
	out.URL.RawPath = ""
	out.URL.RawQuery = url.Values{"output_format": {"mp3_44100_128"}}.Encode()
	out.Body = io.NopCloser(bytes.NewReader(body))
	out.ContentLength = int64(len(body))
	out.Header.Del("Authorization")
	out.Header.Set("xi-api-key", t.account.apiKey)
	out.Header.Set("Accept", "audio/mpeg")
	return out, nil
}

type releasingBody struct {
	io.ReadCloser
	release func()
}

func (b releasingBody) Close() error {
	defer b.release()
	return b.ReadCloser.Close()
}
