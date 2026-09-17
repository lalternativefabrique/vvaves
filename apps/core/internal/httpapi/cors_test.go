package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
)

func options(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodOptions, path, nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A browser on another origin asks before it POSTs. The answer names what a
// player sends, a JSON body and a range, and lets it cache the answer a day.
func TestSpeakAnswersAPreflightForAnyOrigin(t *testing.T) {
	rec := options(t, httpapi.New(guardedDeps(t)), "/speak")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	for name, want := range map[string]string{
		"Access-Control-Allow-Origin":  "*",
		"Access-Control-Allow-Methods": "POST",
		"Access-Control-Allow-Headers": "Content-Type, Range",
		"Access-Control-Max-Age":       "86400",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Error("credentials are allowed: the signature is the credential, a cookie must not be")
	}
}

// The routes only a service may call answer no preflight at all: a browser
// never gets past the first request.
func TestOnlySpeakAnswersAPreflight(t *testing.T) {
	h := httpapi.New(guardedDeps(t))
	for _, path := range []string{"/speak/prime", "/speak/pregenerate", "/speak/exists"} {
		if rec := options(t, h, path); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("OPTIONS %s = %d, want 405", path, rec.Code)
		}
	}
}

// The audio itself carries the origin header, and exposes what a player
// needs to read a duration and seek on a cached reading.
func TestSpeakResponseIsReadableAcrossOrigins(t *testing.T) {
	path := signedQuery("/speak", "chat", "m1", longText, time.Now().Add(5*time.Minute))
	rec := post(t, httpapi.New(guardedDeps(t)), path, `{"text":"`+longText+`","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
	exposed := rec.Header().Get("Access-Control-Expose-Headers")
	for _, name := range []string{"Accept-Ranges", "Content-Length", "Content-Range", "X-Tts-Cache"} {
		if !strings.Contains(exposed, name) {
			t.Errorf("Expose-Headers = %q, want it to name %s", exposed, name)
		}
	}
}
