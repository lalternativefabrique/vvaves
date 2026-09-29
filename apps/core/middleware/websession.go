// Package middleware names the signed-in person behind the admin API and the
// key routes: the token urbangate issued them for vvaves, verified by
// go/websession, then their membership of vvaves (urbangate ADR 0013).
package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/lalternative/packages/go/membership"
	"github.com/lalternative/packages/go/websession"
)

type User = websession.User

// Members opens or answers a person's membership. membership.Service fits;
// nil admits every verified token.
type Members interface {
	Resolve(ctx context.Context, id membership.Identity) (membership.Member, error)
}

// RequireAuth answers 401 unless the request carries a token urbangate
// signed, 503 when urbangate cannot be asked, 403 for an erased member and
// 409 for one vvaves could not open. A nil guard refuses every request: an
// API nobody configured an issuer for is one nobody may call.
func RequireAuth(g *websession.Guard, members Members) func(http.Handler) http.Handler {
	if g == nil {
		return refuseEveryone
	}
	return func(next http.Handler) http.Handler {
		return g.Require(withMembership(members, next))
	}
}

// RequireAdmin is RequireAuth for the routes only this product's admins may
// reach: 403 for anyone else. The web proxy turns non-admins away too, but
// the core never relies on it.
func RequireAdmin(g *websession.Guard, members Members) func(http.Handler) http.Handler {
	if g == nil {
		return refuseEveryone
	}
	return func(next http.Handler) http.Handler {
		return g.RequireRole("admin")(withMembership(members, next))
	}
}

func withMembership(members Members, next http.Handler) http.Handler {
	if members == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := websession.UserFrom(r.Context())
		if !ok || u.IdentityID == "" {
			refuse(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		_, err := members.Resolve(r.Context(), membership.Identity{ID: u.IdentityID, Email: u.Email, Name: u.Name})
		switch {
		case errors.Is(err, membership.ErrErased):
			refuse(w, http.StatusForbidden, "account_erased")
			return
		case errors.Is(err, membership.ErrConflict):
			refuse(w, http.StatusConflict, "identifier_conflict")
			return
		case err != nil:
			refuse(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func refuseEveryone(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refuse(w, http.StatusUnauthorized, "unauthenticated")
	})
}

func refuse(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + code + `"}`))
}

// GetUser returns the user RequireAuth resolved for this request.
func GetUser(ctx context.Context) (User, bool) {
	return websession.UserFrom(ctx)
}
