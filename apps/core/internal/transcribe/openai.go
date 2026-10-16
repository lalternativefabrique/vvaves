package transcribe

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

const defaultOpenAIMaxFileSize = 25 << 20

// OpenAIConfig points the provider at an OpenAI-compatible
// /audio/transcriptions endpoint.
type OpenAIConfig struct {
	URL         string
	APIKey      string
	Model       string
	MaxFileSize int64
}

// OpenAIProvider asks an OpenAI-compatible endpoint for timed segments.
type OpenAIProvider struct {
	cfg  OpenAIConfig
	http *http.Client
}

func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	if cfg.MaxFileSize <= 0 {
		cfg.MaxFileSize = defaultOpenAIMaxFileSize
	}
	return &OpenAIProvider{cfg: cfg, http: &http.Client{Timeout: 15 * time.Minute}}
}

func (p *OpenAIProvider) MaxFileSize() int64       { return p.cfg.MaxFileSize }
func (p *OpenAIProvider) NeedsWAVConversion() bool { return false }

func (p *OpenAIProvider) Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResult, error) {
	if p.cfg.URL == "" {
		return nil, fmt.Errorf("openai provider: no url configured")
	}
	if req.File == nil {
		return nil, fmt.Errorf("openai provider: audio file is required")
	}
	fileName := req.FileName
	if fileName == "" {
		fileName = "audio.m4a"
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", fileName)
	if err != nil {
		return nil, fmt.Errorf("openai provider: multipart file: %w", err)
	}
	if _, err := io.Copy(part, req.File); err != nil {
		return nil, fmt.Errorf("openai provider: copy audio: %w", err)
	}
	fields := [][2]string{
		{"response_format", "verbose_json"},
		{"timestamp_granularities[]", "segment"},
	}
	if p.cfg.Model != "" {
		fields = append(fields, [2]string{"model", p.cfg.Model})
	}
	if req.Language != "" {
		fields = append(fields, [2]string{"language", req.Language})
	}
	for _, f := range fields {
		if err := form.WriteField(f[0], f[1]); err != nil {
			return nil, fmt.Errorf("openai provider: field %s: %w", f[0], err)
		}
	}
	if err := form.Close(); err != nil {
		return nil, fmt.Errorf("openai provider: close multipart: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.URL, &body)
	if err != nil {
		return nil, fmt.Errorf("openai provider: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", form.FormDataContentType())
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}
	res, err := p.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai provider: call: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{RetryAfter: ParseRetryAfter(res.Header.Get("Retry-After"), time.Now()), StatusCode: res.StatusCode}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("openai provider: status %d: %s", res.StatusCode, bytes.TrimSpace(detail))
	}

	var payload struct {
		Text     string `json:"text"`
		Language string `json:"language"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("openai provider: decode: %w", err)
	}
	out := &TranscribeResult{
		Text:     strings.TrimSpace(payload.Text),
		Language: normalizeLanguage(payload.Language),
	}
	for _, s := range payload.Segments {
		out.Segments = append(out.Segments, ProviderSegment{Text: strings.TrimSpace(s.Text), Start: s.Start, End: s.End})
	}
	return out, nil
}

// verbose_json reports the language by name ("french") on OpenAI and by ISO
// code on most compatible servers; only a two-letter code is passed on.
func normalizeLanguage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if len(lang) == 2 {
		return lang
	}
	return languageNames[lang]
}

var languageNames = map[string]string{
	"english": "en", "french": "fr", "german": "de", "spanish": "es", "italian": "it",
	"portuguese": "pt", "dutch": "nl", "polish": "pl", "russian": "ru", "japanese": "ja",
	"chinese": "zh", "korean": "ko", "arabic": "ar",
}
