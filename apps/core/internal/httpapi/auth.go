package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/lalternative/packages/go/svcauth"

	"github.com/lalternativefabrique/vvaves/client"
	"github.com/lalternativefabrique/vvaves/signed"
)

const HeaderKey = client.HeaderKey

// ScopeSpeak is the OAuth2 scope a service's token must carry to have text
// read: a token meant for another part of the suite must not reach the voice.
const ScopeSpeak = "vvaves:speak"


// BearerVerifier checks a token a service obtained from the suite's identity
// provider. svcauth.Verifier is the one main wires.
type BearerVerifier interface {
	Verify(ctx context.Context, raw string) (svcauth.Claims, error)
}

// guardSpeak refuses a /speak request that authenticates as neither.
//
// Unguarded answers everyone whatever keys exist: main wires a key lookup
// even when no key is configured, so the flag is read on its own rather than
// inferred from a nil. Without it and without a key it refuses everything: a
// vvaves that reaches the internet with no key is not an internal one, it
// is an open voice, and silence about a missing key must not look like a
// working service.
func (d Deps) guardSpeak(r *http.Request, scope, id, text string) error {
	if d.Unguarded {
		return nil
	}
	if d.Verifier == nil && d.AppKeyIssuer == nil && d.Tokens == nil {
		return ErrNoGuard
	}
	if raw, ok := svcauth.BearerToken(r); ok && d.Tokens != nil {
		claims, err := d.Tokens.Verify(r.Context(), raw)
		if err != nil {
			return ErrBadToken
		}
		if !claims.HasScope(ScopeSpeak) {
			return ErrTokenLacksScope
		}
		return nil
	}
	if key := r.Header.Get(HeaderKey); key != "" && d.AppKeyIssuer != nil {
		if _, ok := d.AppKeyIssuer(key); ok {
			return nil
		}
	}
	// A signature buys one reading, never the work of making one nobody is
	// waiting for. The signed fields name what to read, not where it was
	// sent, and a front door that routes by path prefix hands /speak/prime
	// the same URL — so a link good for playing a reply would otherwise also
	// pregenerate whole articles, on a voice that reads one at a time.
	if r.URL.Path != speakPath {
		return ErrSignatureNotAcceptedHere
	}
	return d.Verifier.Verify(r.URL.Query(), scope, id, text)
}




// speakPath is the only route a signature authorises: the one that serves a
// listener the audio they asked for.
const speakPath = "/speak"

// ErrSignatureNotAcceptedHere is a signed request to an endpoint that only
// services may call.
var ErrSignatureNotAcceptedHere = errors.New("signed: this endpoint takes an app key, not a signature")

// ErrNoGuard is a speak request on a vvaves with no key configured and no
// leave to run without one.
var ErrNoGuard = errors.New("speak: no key configured, and not unguarded")

// ErrBadToken is a bearer token the identity provider did not sign for this
// service, or one that has expired.
var ErrBadToken = errors.New("speak: bearer token refused")

// ErrTokenLacksScope is a valid token that was not granted the speak scope.
var ErrTokenLacksScope = errors.New("speak: token lacks the " + ScopeSpeak + " scope")

// writeAuthError answers a failed guard.
//
// An expired link is told apart from a rejected one: asking the application
// again fixes the first and nothing fixes the second, and a player that knows
// the difference can fetch a fresh URL rather than showing an error to
// someone whose only mistake was leaving the page open.
func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, signed.ErrExpired):
		writeError(w, http.StatusUnauthorized, "signature expired")
	default:
		writeError(w, http.StatusForbidden, "not authorised to read this text")
	}
}
