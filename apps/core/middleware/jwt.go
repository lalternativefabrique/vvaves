// Package middleware verifies the JWT the admin web app mints from its
// Better Auth session. Vvaves never signs tokens; it checks the HS256 token
// against JWT_SECRET and reads sub, email and name.
package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type User struct {
	ID    string
	Email string
	Name  string
	// IdentityID is the person's id at the suite's identity provider, the
	// owner a customer key is issued against. Empty for an account that never
	// signed in through it.
	IdentityID string
}

type claims struct {
	Email      string `json:"email"`
	Name       string `json:"name"`
	IdentityID string `json:"identityId"`
	jwt.RegisteredClaims
}

type ctxKey struct{}

// RequireAuth answers 401 unless the request carries a valid token, as a
// Bearer header or a `token` cookie. An empty secret refuses every request:
// an admin API nobody configured a secret for is one nobody may call.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	key := []byte(secret)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := tokenFromRequest(r)
			if secret == "" || raw == "" {
				unauthenticated(w)
				return
			}
			u, err := parse(raw, key)
			if err != nil {
				unauthenticated(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
		})
	}
}

// GetUser returns the user RequireAuth resolved for this request.
func GetUser(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

func unauthenticated(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"unauthenticated"}`))
}

func tokenFromRequest(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if cookie, err := r.Cookie("token"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return ""
}

func parse(raw string, key []byte) (User, error) {
	var cl claims
	tok, err := jwt.ParseWithClaims(raw, &cl, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !tok.Valid || cl.Subject == "" {
		return User{}, errors.New("invalid token")
	}
	return User{ID: cl.Subject, Email: cl.Email, Name: cl.Name, IdentityID: cl.IdentityID}, nil
}
