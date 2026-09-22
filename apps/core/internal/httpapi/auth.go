package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/lalternative/packages/go/appkeys"
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

// CustomerKeyVerifier checks a key urbangate issued to one of this product's
// customers: prefix, signature, revocation, then the scopes asked for.
// *appkeys.Keys is the one main wires.
type CustomerKeyVerifier interface {
	Verify(ctx context.Context, key string, scopes ...string) (svcauth.Claims, error)
}

// customerKeyPrefix tells a customer key from a service token on the same
// header: urbangate prefixes every key with the product it was issued for.
var customerKeyPrefix = appkeys.KeyPrefix("vvaves")

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
	if d.Verifier == nil && d.AppKeyIssuer == nil && d.Tokens == nil && d.CustomerKeys == nil {
		return ErrNoGuard
	}
	if raw, ok := svcauth.BearerToken(r); ok {
		if strings.HasPrefix(raw, customerKeyPrefix) {
			return d.guardCustomerKey(r.Context(), raw)
		}
		if d.Tokens != nil {
			return d.guardServiceToken(r.Context(), raw)
		}
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
func (d Deps) guardServiceToken(ctx context.Context, raw string) error {
	claims, err := d.Tokens.Verify(ctx, raw)
	if err != nil {
		return ErrBadToken
	}
	if !claims.HasScope(ScopeSpeak) {
		return ErrTokenLacksScope
	}
	return nil
}

// guardCustomerKey answers a key this product cannot judge with
// ErrRevocationUnknown, never with a refusal: no list, or a list too old, says
// nothing about the key, and telling a customer their valid key is invalid
// sends them rotating it during an outage.
func (d Deps) guardCustomerKey(ctx context.Context, raw string) error {
	if d.CustomerKeys == nil {
		return ErrRevocationUnknown
	}
	_, err := d.CustomerKeys.Verify(ctx, raw, ScopeSpeak)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, appkeys.ErrListStale), errors.Is(err, svcauth.ErrRevocationUnknown):
		return ErrRevocationUnknown
	case errors.Is(err, svcauth.ErrKeyRevoked):
		return ErrKeyRevoked
	case errors.Is(err, appkeys.ErrMissingScope):
		return ErrTokenLacksScope
	default:
		return ErrBadToken
	}
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

// ErrKeyRevoked is a customer key whose signature verifies but which the
// issuer has withdrawn.
var ErrKeyRevoked = errors.New("speak: key revoked")

// ErrRevocationUnknown is a customer key that cannot be judged: no revocation
// list has been loaded, or the copy held is too old to trust.
var ErrRevocationUnknown = errors.New("speak: no revocation list to judge the key by")

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
	case errors.Is(err, ErrRevocationUnknown):
		writeError(w, http.StatusServiceUnavailable, "the key cannot be judged yet, retry")
	default:
		writeError(w, http.StatusForbidden, "not authorised to read this text")
	}
}
