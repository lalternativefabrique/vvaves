package client

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lalternative/packages/go/audioreader"
	"github.com/lalternative/packages/go/tts"
)

// Config points a voice at a vvaves.
type Config struct {
	BaseURL string
	// Scope names where vvaves keeps the readings, so two applications
	// sharing one vvaves do not overwrite each other's cache. Empty leaves
	// the naming to vvaves's default.
	Scope string
	// Key authenticates server-to-server calls on a vvaves reachable from
	// the internet, where a signature only ever buys one listen. It is the
	// same key signed.NewSigner takes. Empty sends nothing, which an
	// internal-only vvaves accepts. A key carrying CustomerKeyPrefix is sent
	// as a bearer token instead.
	Key string
	// Authorize attaches a bearer token from the suite's identity provider
	// to each call, svcauth.ClientCredentials.Authorize typically. Set, it
	// takes the place of Key: vvaves reads the token first.
	Authorize func(*http.Request) error
	// Client defaults to one with no global timeout, same as OpenAIVoice: a
	// long reading can take minutes, and cancellation belongs to the context.
	Client *http.Client
}

// HeaderKey carries Key; the server reads the same name.
const HeaderKey = "X-Vvaves-Key"

// CustomerKeyPrefix marks a key urbangate issued: vvaves verifies those as a
// bearer credential, never on HeaderKey.
const CustomerKeyPrefix = "vvaves_key_"

// Voice reads text through vvaves instead of a speech service directly.
// Vvaves owns the synthesis, the cache and the store, so every application
// speaking through the same vvaves shares one paid reading of the same
// words, and can prime or pregenerate one ahead of any listener.
type Voice struct {
	cfg Config
}

var _ tts.Voice = (*Voice)(nil)

// New wires a voice against vvaves, or nil without a BaseURL: absent, not
// half-present, so a caller checks for nil and runs without audio.
func New(cfg Config) *Voice {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Client == nil {
		cfg.Client = &http.Client{}
	}
	return &Voice{cfg: cfg}
}

func (v *Voice) Speak(ctx context.Context, text string) ([]byte, string, error) {
	return v.SpeakNamed(ctx, "", text)
}

// SpeakNamed reads text under a name, so a reading primed or pregenerated
// earlier under that same name is the one served. An empty id has vvaves key
// the reading on the text alone, which is what Speak does.
func (v *Voice) SpeakNamed(ctx context.Context, id, text string) ([]byte, string, error) {
	resp, err := v.speak(ctx, id, text, false)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("tts: read body: %w", err)
	}
	if len(audio) == 0 {
		return nil, "", fmt.Errorf("tts: no audio for a %d-rune text", len([]rune(text)))
	}
	return audio, respMIME(resp), nil
}

func (v *Voice) SpeakStream(ctx context.Context, text string, emit func([]byte) error) (string, error) {
	return v.SpeakStreamNamed(ctx, "", text, emit)
}

// SpeakStreamNamed streams text under a name. It is the call that turns a
// primed opening into something heard: vvaves only serves an opening read
// ahead of time on the streaming path, so a caller that primed and then asks
// whole pays for the opening twice and waits for all of it.
func (v *Voice) SpeakStreamNamed(ctx context.Context, id, text string, emit func([]byte) error) (string, error) {
	resp, err := v.speak(ctx, id, text, true)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// A cache hit is served whole with its real content type even when the
	// request asked to stream — vvaves answers from the store before it
	// considers reading anything aloud. Only a paying listen carries frames.
	if resp.Header.Get("Content-Type") != audioreader.FramesContentType {
		audio, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("tts: read body: %w", err)
		}
		if len(audio) == 0 {
			return "", fmt.Errorf("tts: no audio for a %d-rune text", len([]rune(text)))
		}
		if err := emit(audio); err != nil {
			return "", err
		}
		return respMIME(resp), nil
	}

	var got bool
	for {
		var length [4]byte
		if _, err := io.ReadFull(resp.Body, length[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", fmt.Errorf("tts: read frame length: %w", err)
		}
		piece := make([]byte, binary.BigEndian.Uint32(length[:]))
		if _, err := io.ReadFull(resp.Body, piece); err != nil {
			return "", fmt.Errorf("tts: read frame: %w", err)
		}
		if len(piece) == 0 {
			continue
		}
		got = true
		if err := emit(piece); err != nil {
			return "", err
		}
	}
	if !got {
		return "", fmt.Errorf("tts: no audio for a %d-rune text", len([]rune(text)))
	}
	return tts.MIMEFor("mp3"), nil
}

// Pregenerate asks vvaves to read text in full and keep it, ahead of any
// listener. Vvaves acknowledges before the reading starts, so a nil return
// means scheduled, not stored; id is what lets the reading be asked for later
// under the same name.
func (v *Voice) Pregenerate(ctx context.Context, id, text string) error {
	resp, err := v.post(ctx, "/speak/pregenerate", map[string]any{
		"text":  text,
		"scope": v.cfg.Scope,
		"id":    id,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// PrimeOpening asks vvaves to read only the start of text and keep it, ahead
// of any listener: one request that buys the seconds before play, where
// Pregenerate pays for the whole text on the chance that someone listens.
// Vvaves acknowledges before the reading starts, so a nil return means
// scheduled, not stored. id is required: an opening nobody can name again is
// one no listener will ever be served.
func (v *Voice) PrimeOpening(ctx context.Context, id, text string) error {
	resp, err := v.post(ctx, "/speak/prime", map[string]any{
		"text":  text,
		"scope": v.cfg.Scope,
		"id":    id,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Exists reports whether a reading kept under id is ready to be served
// without paying for a synthesis: what a public page checks before offering
// a listen to a visitor who has no account to bill one to.
func (v *Voice) Exists(ctx context.Context, id, text string) (bool, error) {
	resp, err := v.post(ctx, "/speak/exists", map[string]any{
		"text":  text,
		"scope": v.cfg.Scope,
		"id":    id,
	})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var body struct {
		Ready bool `json:"ready"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, fmt.Errorf("tts: read body: %w", err)
	}
	return body.Ready, nil
}

func (v *Voice) speak(ctx context.Context, id, text string, stream bool) (*http.Response, error) {
	payload := map[string]any{
		"text":   text,
		"scope":  v.cfg.Scope,
		"stream": stream,
	}
	if id != "" {
		payload["id"] = id
	}
	return v.post(ctx, "/speak", payload)
}

func (v *Voice) post(ctx context.Context, path string, payload map[string]any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("tts: build request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.cfg.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tts: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := v.authorize(req); err != nil {
		return nil, fmt.Errorf("tts: authorize: %w", err)
	}
	resp, err := v.cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts: call: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("tts: status %d: %s", resp.StatusCode, bytes.TrimSpace(detail))
	}
	return resp, nil
}

func (v *Voice) authorize(req *http.Request) error {
	switch {
	case v.cfg.Authorize != nil:
		return v.cfg.Authorize(req)
	case strings.HasPrefix(v.cfg.Key, CustomerKeyPrefix):
		req.Header.Set("Authorization", "Bearer "+v.cfg.Key)
	case v.cfg.Key != "":
		req.Header.Set(HeaderKey, v.cfg.Key)
	}
	return nil
}

func respMIME(resp *http.Response) string {
	ct := resp.Header.Get("Content-Type")
	if ct == "" || strings.HasPrefix(ct, "text/") || strings.HasPrefix(ct, "application/json") {
		return tts.MIMEFor("mp3")
	}
	return ct
}
