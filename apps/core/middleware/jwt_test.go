package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func mint(t *testing.T, secret string, exp time.Time) string {
	t.Helper()
	return mintWith(t, secret, claims{
		Email: "ops@example", Name: "Ops",
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u-1", ExpiresAt: jwt.NewNumericDate(exp)},
	})
}

func mintWith(t *testing.T, secret string, cl claims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, cl)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func call(secret, header string) (int, User, bool) {
	var got User
	var seen bool
	h := RequireAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, seen = GetUser(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if header != "" {
		req.Header.Set("Authorization", "Bearer "+header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, got, seen
}

func TestAValidTokenResolvesTheUser(t *testing.T) {
	code, u, seen := call("s3cret", mint(t, "s3cret", time.Now().Add(time.Minute)))
	if code != http.StatusOK || !seen || u.ID != "u-1" || u.Email != "ops@example" {
		t.Fatalf("code=%d user=%+v seen=%v", code, u, seen)
	}
}

func TestAValidTokenCarriesTheProviderIdentity(t *testing.T) {
	exp := jwt.NewNumericDate(time.Now().Add(time.Minute))
	_, u, _ := call("s3cret", mintWith(t, "s3cret", claims{
		IdentityID:       "8f3a",
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u-1", ExpiresAt: exp},
	}))
	if u.IdentityID != "8f3a" {
		t.Fatalf("identityId = %q, want 8f3a", u.IdentityID)
	}
	_, u, _ = call("s3cret", mintWith(t, "s3cret", claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "u-1", ExpiresAt: exp},
	}))
	if u.IdentityID != "" {
		t.Fatalf("identityId = %q, want empty for an account never enrolled", u.IdentityID)
	}
}

func TestRefusedTokens(t *testing.T) {
	for name, header := range map[string]string{
		"missing":      "",
		"wrong secret": mint(t, "other", time.Now().Add(time.Minute)),
		"expired":      mint(t, "s3cret", time.Now().Add(-time.Minute)),
	} {
		if code, _, _ := call("s3cret", header); code != http.StatusUnauthorized {
			t.Errorf("%s: code = %d, want 401", name, code)
		}
	}
}

func TestNoSecretRefusesEveryone(t *testing.T) {
	if code, _, _ := call("", mint(t, "", time.Now().Add(time.Minute))); code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401 with no secret configured", code)
	}
}
