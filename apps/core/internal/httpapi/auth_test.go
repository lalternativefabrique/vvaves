package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
	"github.com/lalternativefabrique/vvaves/signed"
)

const (
	testIssuer = "lalter"
	testKey    = "a-shared-secret"
)

func guardedDeps(t *testing.T) httpapi.Deps {
	t.Helper()
	d := audioDeps(&stubVoice{pieces: [][]byte{[]byte("aaa"), []byte("bb")}}, newMemStore())
	d.Unguarded = false
	d.Verifier = signed.NewVerifier(map[string]string{testIssuer: testKey})
	d.AppKeyIssuer = func(key string) (string, bool) {
		if key == "an-app-key" {
			return "lalter", true
		}
		return "", false
	}
	return d
}

func signedQuery(path, scope, id, text string, expires time.Time) string {
	q := signed.Sign(testIssuer, testKey, signed.Params{
		Scope: scope, ID: id, TextHash: signed.HashText(text), Expires: expires,
	})
	return path + "?" + q.Encode()
}

// A vvaves with no key configured refuses: an internet-facing one that
// forgot its keys would otherwise serve anyone a paid reading, and a bogus
// signature would not even be looked at.
func TestSpeakWithoutAnyAuthConfiguredRefuses(t *testing.T) {
	d := audioDeps(&stubVoice{pieces: [][]byte{[]byte("aaa")}}, nil)
	d.Unguarded = false
	h := httpapi.New(d)
	for name, path := range map[string]string{"bare": "/speak", "bogus signature": "/speak?exp=1&iss=synthiz&sig=x"} {
		if rec := post(t, h, path, `{"text":"bonjour"}`); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", name, rec.Code)
		}
	}
}

// A deployment reachable only from the cluster, or a laptop, says so and
// keeps answering: the network is its boundary, and demanding a credential
// to talk to itself would be one that exists only to be checked.
func TestSpeakStaysOpenWhenUnguardedIsSaid(t *testing.T) {
	rec := post(t, httpapi.New(audioDeps(&stubVoice{pieces: [][]byte{[]byte("aaa")}}, nil)), "/speak", `{"text":"bonjour"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

// main always wires a key lookup, even one that knows no key, so the flag
// has to win over a configured verifier or it would never apply in the
// binary itself.
func TestUnguardedWinsOverAConfiguredVerifier(t *testing.T) {
	d := guardedDeps(t)
	d.Unguarded = true
	rec := post(t, httpapi.New(d), "/speak?exp=1&iss=synthiz&sig=x", `{"text":"bonjour"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestSpeakRejectsAnUnsignedBrowserCall(t *testing.T) {
	rec := post(t, httpapi.New(guardedDeps(t)), "/speak", `{"text":"bonjour","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestSpeakAcceptsASignedCall(t *testing.T) {
	path := signedQuery("/speak", "chat", "m1", longText, time.Now().Add(5*time.Minute))
	rec := post(t, httpapi.New(guardedDeps(t)), path, `{"text":"`+longText+`","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
}

// The signature covers the text, so a link handed out for one reading must
// not authorise having anything else synthesized on the same account.
func TestSpeakRejectsASignatureForAnotherText(t *testing.T) {
	path := signedQuery("/speak", "chat", "m1", "bonjour", time.Now().Add(5*time.Minute))
	rec := post(t, httpapi.New(guardedDeps(t)), path, `{"text":"something else entirely","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// An expired link is worth telling apart: asking the application again fixes
// it, so a player can fetch a fresh URL instead of showing an error.
func TestSpeakReportsAnExpiredSignatureAs401(t *testing.T) {
	path := signedQuery("/speak", "chat", "m1", longText, time.Now().Add(-time.Second))
	rec := post(t, httpapi.New(guardedDeps(t)), path, `{"text":"`+longText+`","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// A service on the cluster's own network authenticates as itself, with no
// signature to make: it is not handing a browser anything.
func TestSpeakAcceptsAnAppKey(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/speak", strings.NewReader(`{"text":"`+longText+`","scope":"chat","id":"m1"}`))
	req.Header.Set(httpapi.HeaderKey, "an-app-key")
	httpapi.New(guardedDeps(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
}

func TestSpeakRejectsAnUnknownAppKey(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/speak", strings.NewReader(`{"text":"bonjour","scope":"chat","id":"m1"}`))
	req.Header.Set(httpapi.HeaderKey, "not-a-key")
	httpapi.New(guardedDeps(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// Priming and pregenerating both occupy the voice, so an unauthenticated
// caller must not be able to queue work behind a listener who is waiting.
func TestPrimeRejectsAnUnsignedCall(t *testing.T) {
	rec := post(t, httpapi.New(guardedDeps(t)), "/speak/prime", `{"text":"bonjour","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestPregenerateRejectsAnUnsignedCall(t *testing.T) {
	rec := post(t, httpapi.New(guardedDeps(t)), "/speak/pregenerate", `{"text":"bonjour","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// Exists reads no bytes, but it still reports whether a given text has been
// read — which is something about someone else's conversation.
func TestExistsRejectsAnUnsignedCall(t *testing.T) {
	rec := post(t, httpapi.New(guardedDeps(t)), "/speak/exists", `{"text":"bonjour","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// searchDeps is a guarded vvaves with a search backend behind it.
func TestASignatureDoesNotReachPrime(t *testing.T) {
	path := signedQuery("/speak/prime", "chat", "m1", longText, time.Now().Add(5*time.Minute))
	rec := post(t, httpapi.New(guardedDeps(t)), path, `{"text":"`+longText+`","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: a signed link reached prime", rec.Code)
	}
}

func TestASignatureDoesNotReachPregenerate(t *testing.T) {
	path := signedQuery("/speak/pregenerate", "chat", "m1", longText, time.Now().Add(5*time.Minute))
	rec := post(t, httpapi.New(guardedDeps(t)), path, `{"text":"`+longText+`","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: a signed link reached pregenerate", rec.Code)
	}
}

// Exists reads no bytes and starts no synthesis, but it is still about
// someone's own conversation, so a browser reaches it the same way it reaches
// /speak — with an app key, since its signature is only good for /speak.
func TestPrimeStillAcceptsAnAppKey(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/speak/prime", strings.NewReader(`{"text":"`+longText+`","scope":"chat","id":"m1"}`))
	req.Header.Set(httpapi.HeaderKey, "an-app-key")
	httpapi.New(guardedDeps(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %q)", rec.Code, rec.Body.String())
	}
}
