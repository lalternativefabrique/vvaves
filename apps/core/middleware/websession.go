// Package middleware names the signed-in person behind the admin API and the
// key routes: the token urbangate issued them for vvaves, verified by
// go/websession.
package middleware

import (
	"context"
	"net/http"

	"github.com/lalternative/packages/go/websession"
)

type User = websession.User

// RequireAuth answers 401 unless the request carries a token urbangate
// signed, and 503 when urbangate cannot be asked. A nil guard refuses every
// request: an API nobody configured an issuer for is one nobody may call.
func RequireAuth(g *websession.Guard) func(http.Handler) http.Handler {
	if g == nil {
		return refuseEveryone
	}
	return g.Require
}

// RequireAdmin is RequireAuth for the routes only this product's admins may
// reach: 403 for anyone else. The web proxy turns non-admins away too, but
// the core never relies on it.
func RequireAdmin(g *websession.Guard) func(http.Handler) http.Handler {
	if g == nil {
		return refuseEveryone
	}
	return g.RequireRole("admin")
}

func refuseEveryone(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthenticated"}`))
	})
}

// GetUser returns the user RequireAuth resolved for this request.
func GetUser(ctx context.Context) (User, bool) {
	return websession.UserFrom(ctx)
}
