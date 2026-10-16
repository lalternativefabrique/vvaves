package transcribe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIProviderRequestsTimedSegments(t *testing.T) {
	var form map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		form = r.MultipartForm.Value
		w.Write([]byte(`{"text":" bonjour le monde ","language":"french","segments":[{"start":0,"end":1.5,"text":" bonjour"},{"start":1.5,"end":3,"text":" le monde"}]}`))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{URL: srv.URL, APIKey: "k", Model: "whisper-large-v3"})
	res, err := p.Transcribe(context.Background(), TranscribeRequest{FileName: "a.mp3", File: strings.NewReader("x"), Language: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"response_format": "verbose_json", "timestamp_granularities[]": "segment", "model": "whisper-large-v3", "language": "fr"} {
		if got := form[k]; len(got) != 1 || got[0] != want {
			t.Errorf("%s = %v, want %s", k, got, want)
		}
	}
	if res.Text != "bonjour le monde" || res.Language != "fr" {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Segments) != 2 || res.Segments[1] != (ProviderSegment{Text: "le monde", Start: 1.5, End: 3}) {
		t.Fatalf("segments = %+v", res.Segments)
	}
}

func TestOpenAIProviderRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := NewOpenAIProvider(OpenAIConfig{URL: srv.URL}).Transcribe(context.Background(), TranscribeRequest{File: strings.NewReader("x")})
	var rle *RateLimitError
	if !errors.As(err, &rle) || rle.RetryAfter.Seconds() != 7 {
		t.Fatalf("err = %v", err)
	}
}
