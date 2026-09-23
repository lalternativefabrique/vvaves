// Package middleware names the signed-in person behind the admin API, from
// the token urbangate issued them; go/websession verifies it.
package middleware

import (
	"context"
	"net/http"

	"github.com/lalternative/packages/go/websession"
)

type User = websession.User

// RequireAuth answers 401 unless the request carries a token urbangate
// signed. A nil guard refuses every request: an admin API nobody configured
// an issuer for is one nobody may call.
func RequireAuth(g *websession.Guard) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if g == nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthenticated"}`))
			})
		}
		return g.Require(next)
	}
}

// GetUser returns the user RequireAuth resolved for this request.
func GetUser(ctx context.Context) (User, bool) {
	return websession.UserFrom(ctx)
}
