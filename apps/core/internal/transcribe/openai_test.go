package transcribe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIProviderRequestsTimedSegmentsAndWords(t *testing.T) {
	var form map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		form = r.MultipartForm.Value
		w.Write([]byte(`{"text":" bonjour le monde ","language":"french",
			"segments":[{"start":0,"end":1.5,"text":" bonjour"},{"start":1.5,"end":3,"text":" le monde"}],
			"words":[{"word":" bonjour","start":0.2,"end":0.9},{"word":"le","start":1.6,"end":1.8},{"word":"monde","start":1.8,"end":2.7},{"word":" ","start":2.7,"end":2.8},{"word":"tard","start":3.1,"end":3.4}]}`))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{URL: srv.URL, APIKey: "k", Model: "whisper-large-v3"})
	res, err := p.Transcribe(context.Background(), TranscribeRequest{FileName: "a.mp3", File: strings.NewReader("x"), Language: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"response_format": "verbose_json", "model": "whisper-large-v3", "language": "fr"} {
		if got := form[k]; len(got) != 1 || got[0] != want {
			t.Errorf("%s = %v, want %s", k, got, want)
		}
	}
	if got := form["timestamp_granularities[]"]; len(got) != 2 || got[0] != "segment" || got[1] != "word" {
		t.Errorf("granularities = %v, want segment and word", got)
	}
	if res.Text != "bonjour le monde" || res.Language != "fr" {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Segments) != 2 || res.Segments[1].Text != "le monde" || res.Segments[1].Start != 1.5 || res.Segments[1].End != 3 {
		t.Fatalf("segments = %+v", res.Segments)
	}
	first, second := res.Segments[0].Words, res.Segments[1].Words
	if len(first) != 1 || first[0] != (ProviderWord{Text: "bonjour", Start: 0.2, End: 0.9}) {
		t.Fatalf("first segment words = %+v", first)
	}
	if len(second) != 3 || second[0].Text != "le" || second[1] != (ProviderWord{Text: "monde", Start: 1.8, End: 2.7}) || second[2].Text != "tard" {
		t.Fatalf("second segment words = %+v: a word starting past the last segment stays with it, a blank one is dropped", second)
	}
}

func TestOpenAIProviderGivesUpOnWordsWhenTheServerRefusesThem(t *testing.T) {
	var asked [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		granularities := r.MultipartForm.Value["timestamp_granularities[]"]
		asked = append(asked, granularities)
		if len(granularities) > 1 {
			http.Error(w, `{"error":"unsupported granularity"}`, http.StatusUnprocessableEntity)
			return
		}
		w.Write([]byte(`{"text":"bonjour","segments":[{"start":0,"end":1,"text":"bonjour"}]}`))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(OpenAIConfig{URL: srv.URL})
	for i := 0; i < 2; i++ {
		res, err := p.Transcribe(context.Background(), TranscribeRequest{File: strings.NewReader("x")})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Segments) != 1 || res.Segments[0].Words != nil {
			t.Fatalf("segments = %+v", res.Segments)
		}
	}
	if len(asked) != 3 || len(asked[0]) != 2 || len(asked[1]) != 1 || len(asked[2]) != 1 {
		t.Fatalf("requests = %v: words are asked once, refused, and not asked again", asked)
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

func TestOpenAIProviderDoesNotRetryAnAuthOrQuotaRefusal(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(status)
		}))
		p := NewOpenAIProvider(OpenAIConfig{URL: srv.URL})
		if _, err := p.Transcribe(context.Background(), TranscribeRequest{File: strings.NewReader("x")}); err == nil {
			t.Errorf("status %d: no error", status)
		}
		srv.Close()
		if calls != 1 || p.wordsRefused.Load() {
			t.Errorf("status %d: %d calls, words given up: %v — a credential or quota problem is not about words", status, calls, p.wordsRefused.Load())
		}
	}
}
