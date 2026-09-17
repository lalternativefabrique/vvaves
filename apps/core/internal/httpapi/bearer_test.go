package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lalternative/packages/go/svcauth"

	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
)

type stubTokens map[string]svcauth.Claims

func (s stubTokens) Verify(_ context.Context, raw string) (svcauth.Claims, error) {
	c, ok := s[raw]
	if !ok {
		return svcauth.Claims{}, errors.New("refused")
	}
	return c, nil
}

func tokenDeps(t *testing.T) httpapi.Deps {
	t.Helper()
	d := guardedDeps(t)
	d.Tokens = stubTokens{
		"speaks":   {Subject: "lalter-core", Audience: []string{"vvaves"}, Scopes: []string{httpapi.ScopeSpeak}},
		"searches": {Subject: "lalter-core", Audience: []string{"vvaves"}, Scopes: []string{"vvaves:search"}},
	}
	return d
}

func postBearer(t *testing.T, d httpapi.Deps, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	httpapi.New(d).ServeHTTP(rec, req)
	return rec
}

// A service that obtained a token from the suite's identity provider needs
// neither an app key nor a signature: the token names this vvaves.
func TestSpeakAcceptsABearerTokenWithTheSpeakScope(t *testing.T) {
	rec := postBearer(t, tokenDeps(t), "/speak", "speaks", `{"text":"`+longText+`","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
}

func TestSpeakRejectsABearerTokenWithoutTheSpeakScope(t *testing.T) {
	rec := postBearer(t, tokenDeps(t), "/speak", "searches", `{"text":"bonjour","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestPrimeAcceptsABearerToken(t *testing.T) {
	rec := postBearer(t, tokenDeps(t), "/speak/prime", "speaks", `{"text":"`+longText+`","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %q)", rec.Code, rec.Body.String())
	}
}

// A bearer header on a vvaves that trusts no issuer is not a credential, and
// the app key path must still be reached.
func TestBearerHeaderIsIgnoredWithoutAnIssuer(t *testing.T) {
	d := guardedDeps(t)
	rec := postBearer(t, d, "/speak", "speaks", `{"text":"bonjour","scope":"chat","id":"m1"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// Tokens alone are a guard: a vvaves trusting an issuer but holding no key
// must not report itself unguarded.
func TestTokensAloneCountAsAGuard(t *testing.T) {
	d := audioDeps(&stubVoice{pieces: [][]byte{[]byte("aaa")}}, nil)
	d.Unguarded = false
	d.Tokens = stubTokens{"speaks": {Subject: "s", Scopes: []string{httpapi.ScopeSpeak}}}
	if rec := postBearer(t, d, "/speak", "speaks", `{"text":"bonjour"}`); rec.Code != http.StatusOK {
		t.Fatalf("token: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if rec := post(t, httpapi.New(d), "/speak", `{"text":"bonjour"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("no credential: status = %d, want 403", rec.Code)
	}
}
