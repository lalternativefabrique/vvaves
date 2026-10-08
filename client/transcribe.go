package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// Transcribe turns a recording into text through vvaves's /transcribe. The
// key or token needs the vvaves:transcribe scope; language may be empty.
func (v *Voice) Transcribe(ctx context.Context, filename string, audio io.Reader, language string) (string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("audio", filename)
	if err != nil {
		return "", fmt.Errorf("stt: build request: %w", err)
	}
	if _, err := io.Copy(part, audio); err != nil {
		return "", fmt.Errorf("stt: build request: %w", err)
	}
	if language != "" {
		_ = form.WriteField("language", language)
	}
	if err := form.Close(); err != nil {
		return "", fmt.Errorf("stt: build request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.cfg.BaseURL+"/transcribe", &body)
	if err != nil {
		return "", fmt.Errorf("stt: build request: %w", err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	if err := v.authorize(req); err != nil {
		return "", fmt.Errorf("stt: authorize: %w", err)
	}
	resp, err := v.cfg.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt: call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("stt: status %d: %s", resp.StatusCode, bytes.TrimSpace(detail))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("stt: decode: %w", err)
	}
	return out.Text, nil
}
