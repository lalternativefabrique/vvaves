package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lalternative/packages/go/svcauth"
	"github.com/lalternative/packages/go/websession"
)

type stubVerifier struct {
	claims svcauth.Claims
	err    error
}

func (s stubVerifier) Verify(context.Context, string) (svcauth.Claims, error) {
	return s.claims, s.err
}

func callThrough(mw func(http.Handler) http.Handler) (int, User) {
	var got User
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = GetUser(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer h.e30.s")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, got
}

func guardFor(roles ...string) *websession.Guard {
	return websession.NewWith(stubVerifier{claims: svcauth.Claims{Subject: "8f3a", Roles: roles}}, "vvaves")
}

func TestThePersonIsNamedByTheirIdentity(t *testing.T) {
	code, u := callThrough(RequireAuth(guardFor("vvaves:user")))
	if code != http.StatusOK || u.IdentityID != "8f3a" {
		t.Fatalf("code=%d user=%+v", code, u)
	}
}

func TestNoGuardRefusesEveryone(t *testing.T) {
	if code, _ := callThrough(RequireAuth(nil)); code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", code)
	}
	if code, _ := callThrough(RequireAdmin(nil)); code != http.StatusUnauthorized {
		t.Fatalf("admin code = %d, want 401", code)
	}
}

func TestAUserIsRefusedTheAdminRoutes(t *testing.T) {
	if code, _ := callThrough(RequireAdmin(guardFor("vvaves:user"))); code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", code)
	}
}

func TestAnAdminReachesTheAdminRoutes(t *testing.T) {
	if code, u := callThrough(RequireAdmin(guardFor("vvaves:admin"))); code != http.StatusOK || u.Role != "admin" {
		t.Fatalf("code=%d user=%+v", code, u)
	}
}

func TestAnotherProductsAdminIsRefusedTheAdminRoutes(t *testing.T) {
	if code, _ := callThrough(RequireAdmin(guardFor("tornad:admin"))); code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", code)
	}
}

func TestAnUnreachableIdentityProviderIsA503(t *testing.T) {
	g := websession.NewWith(stubVerifier{err: websession.ErrUnavailable}, "vvaves")
	if code, _ := callThrough(RequireAuth(g)); code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", code)
	}
}

func TestAForgedTokenIsA401(t *testing.T) {
	g := websession.NewWith(stubVerifier{err: errors.New("bad signature")}, "vvaves")
	if code, _ := callThrough(RequireAuth(g)); code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", code)
	}
}
