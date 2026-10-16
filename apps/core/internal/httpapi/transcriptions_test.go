package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lalternative/packages/go/svcauth"

	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
	"github.com/lalternativefabrique/vvaves/core/internal/transcription"
)

type stubTranscriptions struct {
	submitted []transcription.Request
	jobs      map[string]transcription.Job
}

func (s *stubTranscriptions) Submit(_ context.Context, r transcription.Request) (transcription.Job, error) {
	if err := r.Validate(); err != nil {
		return transcription.Job{}, err
	}
	s.submitted = append(s.submitted, r)
	return transcription.Job{ID: "job-1"}, nil
}

func (s *stubTranscriptions) Get(_ context.Context, id string) (transcription.Job, error) {
	j, ok := s.jobs[id]
	if !ok {
		return transcription.Job{}, transcription.ErrNotFound
	}
	return j, nil
}

func transcriptionsDeps(t *testing.T, stub *stubTranscriptions) httpapi.Deps {
	d := guardedDeps(t)
	d.Tokens = stubTokens{
		"app-a":  svcauth.Claims{ClientID: "a", Scopes: []string{httpapi.ScopeTranscribe}},
		"app-b":  svcauth.Claims{ClientID: "b", Scopes: []string{httpapi.ScopeTranscribe}},
		"speaks": svcauth.Claims{ClientID: "a", Scopes: []string{httpapi.ScopeSpeak}},
	}
	if stub != nil {
		d.Transcriptions = stub
	}
	return d
}

func call(d httpapi.Deps, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	httpapi.New(d).ServeHTTP(rec, req)
	return rec
}

func TestCreateTranscription(t *testing.T) {
	stub := &stubTranscriptions{}
	rec := call(transcriptionsDeps(t, stub), http.MethodPost, "/transcriptions", "app-a",
		`{"url":"https://bucket.example/a.mp3","language":"fr","reference":"ep-1"}`)
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"id":"job-1"`) {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	want := transcription.Request{App: "a", URL: "https://bucket.example/a.mp3", Language: "fr", Reference: "ep-1"}
	if len(stub.submitted) != 1 || stub.submitted[0] != want {
		t.Fatalf("submitted %+v", stub.submitted)
	}
}

func TestCreateTranscriptionRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		token, body string
		stub        *stubTranscriptions
		want        int
	}{
		"anonymous":      {"", `{"url":"https://x/a.mp3","reference":"r"}`, &stubTranscriptions{}, http.StatusForbidden},
		"speak scope":    {"speaks", `{"url":"https://x/a.mp3","reference":"r"}`, &stubTranscriptions{}, http.StatusForbidden},
		"plain http":     {"app-a", `{"url":"http://x/a.mp3","reference":"r"}`, &stubTranscriptions{}, http.StatusBadRequest},
		"no reference":   {"app-a", `{"url":"https://x/a.mp3"}`, &stubTranscriptions{}, http.StatusBadRequest},
		"not json":       {"app-a", `nope`, &stubTranscriptions{}, http.StatusBadRequest},
		"not configured": {"app-a", `{"url":"https://x/a.mp3","reference":"r"}`, nil, http.StatusServiceUnavailable},
	} {
		if rec := call(transcriptionsDeps(t, tc.stub), http.MethodPost, "/transcriptions", tc.token, tc.body); rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d (%s)", name, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestCreateTranscriptionRefusesASignedURL(t *testing.T) {
	d := transcriptionsDeps(t, &stubTranscriptions{})
	path := signedQuery("/transcriptions", httpapi.ScopeTranscribe, "id", "text", time.Now().Add(time.Minute))
	if rec := call(d, http.MethodPost, path, "", `{"url":"https://x/a.mp3","reference":"r"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestGetTranscriptionOnlyForItsApp(t *testing.T) {
	stub := &stubTranscriptions{jobs: map[string]transcription.Job{
		"job-1": {ID: "job-1", App: "a", Reference: "ep-1", Status: transcription.StatusCompleted, Result: &transcription.Result{Text: "bonjour"}},
	}}
	d := transcriptionsDeps(t, stub)
	rec := call(d, http.MethodGet, "/transcriptions/job-1", "app-a", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"text":"bonjour"`) || !strings.Contains(rec.Body.String(), `"status":"completed"`) {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if rec := call(d, http.MethodGet, "/transcriptions/job-1", "app-b", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("other app: status = %d", rec.Code)
	}
	if rec := call(d, http.MethodGet, "/transcriptions/nope", "app-a", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: status = %d", rec.Code)
	}
}
