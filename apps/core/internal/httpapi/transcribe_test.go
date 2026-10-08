package httpapi_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lalternative/packages/go/svcauth"

	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
)

type stubTranscriber struct {
	audio, language string
}

func (s *stubTranscriber) Transcribe(_ context.Context, _ string, audio io.Reader, language string) (string, error) {
	b, _ := io.ReadAll(audio)
	s.audio, s.language = string(b), language
	return "bonjour", nil
}

func postAudio(t *testing.T, d httpapi.Deps, header, value string, audio []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("audio", "dictation.webm")
	_, _ = part.Write(audio)
	_ = form.WriteField("language", "fr")
	_ = form.Close()
	req := httptest.NewRequest(http.MethodPost, "/transcribe", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	if header != "" {
		req.Header.Set(header, value)
	}
	rec := httptest.NewRecorder()
	httpapi.New(d).ServeHTTP(rec, req)
	return rec
}

func TestTranscribeWithAnAppKey(t *testing.T) {
	stub := &stubTranscriber{}
	d := guardedDeps(t)
	d.Transcriber = stub
	rec := postAudio(t, d, httpapi.HeaderKey, "an-app-key", []byte("RIFF"))
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"text":"bonjour"`)) {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if stub.audio != "RIFF" || stub.language != "fr" {
		t.Fatalf("forwarded %q %q", stub.audio, stub.language)
	}
}

func TestTranscribeRefusesAnonymousCalls(t *testing.T) {
	d := guardedDeps(t)
	d.Transcriber = &stubTranscriber{}
	if rec := postAudio(t, d, "", "", []byte("RIFF")); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestTranscribeAsksTheTranscribeScope(t *testing.T) {
	d := guardedDeps(t)
	d.Transcriber = &stubTranscriber{}
	d.Tokens = stubTokens{
		"speaks":      svcauth.Claims{Scopes: []string{httpapi.ScopeSpeak}},
		"transcribes": svcauth.Claims{Scopes: []string{httpapi.ScopeTranscribe}},
	}
	if rec := postAudio(t, d, "Authorization", "Bearer speaks", []byte("RIFF")); rec.Code != http.StatusForbidden {
		t.Fatalf("speak-only token: status = %d, want 403", rec.Code)
	}
	if rec := postAudio(t, d, "Authorization", "Bearer transcribes", []byte("RIFF")); rec.Code != http.StatusOK {
		t.Fatalf("transcribe token: status = %d, want 200", rec.Code)
	}
}

func TestTranscribeCustomerKeyAsksTheTranscribeScope(t *testing.T) {
	keys := &stubCustomerKeys{}
	d := guardedDeps(t)
	d.Transcriber = &stubTranscriber{}
	d.CustomerKeys = keys
	if rec := postAudio(t, d, "Authorization", "Bearer "+customerKey, []byte("RIFF")); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(keys.scopes) != 1 || keys.scopes[0] != httpapi.ScopeTranscribe {
		t.Fatalf("scopes asked = %v", keys.scopes)
	}
}

func TestTranscribeUnconfiguredIs503(t *testing.T) {
	if rec := postAudio(t, guardedDeps(t), httpapi.HeaderKey, "an-app-key", []byte("RIFF")); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestTranscribeRefusesEmptyAudio(t *testing.T) {
	d := guardedDeps(t)
	d.Transcriber = &stubTranscriber{}
	if rec := postAudio(t, d, httpapi.HeaderKey, "an-app-key", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
