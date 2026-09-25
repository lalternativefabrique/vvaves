package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lalternative/packages/go/svcauth"
	"github.com/lalternative/packages/go/websession"
)

type stubVerifier struct{ claims svcauth.Claims }

func (s stubVerifier) Verify(context.Context, string) (svcauth.Claims, error) {
	return s.claims, nil
}

func call(g *websession.Guard) (int, User, bool) {
	var got User
	var seen bool
	h := RequireAuth(g)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, seen = GetUser(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer h.e30.s")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, got, seen
}

func TestThePersonIsReadOffUrbangatesToken(t *testing.T) {
	g := websession.NewWith(stubVerifier{claims: svcauth.Claims{Subject: "8f3a", Roles: []string{"vvaves:admin"}}}, "vvaves")
	code, u, seen := call(g)
	if code != http.StatusOK || !seen || u.IdentityID != "8f3a" || u.Role != "admin" {
		t.Fatalf("code=%d seen=%v user=%+v", code, seen, u)
	}
}

func TestNoGuardRefusesEveryone(t *testing.T) {
	if code, _, seen := call(nil); code != http.StatusUnauthorized || seen {
		t.Fatalf("code=%d seen=%v", code, seen)
	}
}
