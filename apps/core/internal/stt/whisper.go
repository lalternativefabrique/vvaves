// Package stt turns recorded speech into text through an OpenAI-compatible
// /audio/transcriptions endpoint.
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultURL   = "https://oai.endpoints.kepler.ai.cloud.ovh.net/v1/audio/transcriptions"
	DefaultModel = "whisper-large-v3-turbo"
)

type Config struct {
	URL    string
	APIKey string
	Model  string
}

type Whisper struct {
	cfg  Config
	http *http.Client
}

// New returns nil without an API key, so main mounts no transcription
// rather than one that always fails.
func New(cfg Config) *Whisper {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil
	}
	if cfg.URL == "" {
		cfg.URL = DefaultURL
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	return &Whisper{cfg: cfg, http: &http.Client{Timeout: 2 * time.Minute}}
}

func (w *Whisper) Transcribe(ctx context.Context, filename string, audio io.Reader, language string) (string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, audio); err != nil {
		return "", err
	}
	_ = form.WriteField("model", w.cfg.Model)
	_ = form.WriteField("response_format", "json")
	if language != "" {
		_ = form.WriteField("language", language)
	}
	if err := form.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.URL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+w.cfg.APIKey)
	res, err := w.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt: call: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return "", fmt.Errorf("stt: status %d: %s", res.StatusCode, bytes.TrimSpace(detail))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("stt: decode: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}
