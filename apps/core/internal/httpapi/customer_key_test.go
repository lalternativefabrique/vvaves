package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/lalternative/packages/go/appkeys"
	"github.com/lalternative/packages/go/svcauth"

	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
)

type stubCustomerKeys struct {
	err    error
	scopes []string
}

func (s *stubCustomerKeys) Verify(_ context.Context, _ string, scopes ...string) (svcauth.Claims, error) {
	s.scopes = scopes
	return svcauth.Claims{}, s.err
}

const customerKey = "vvaves_key_eyJhbGciOiJSUzI1NiJ9.e30.sig"

const speakBody = `{"text":"` + longText + `","scope":"chat","id":"m1"}`

// A customer key is told from a service token by its prefix, and never
// reaches the identity provider's verifier: the two are signed by different
// keys and answer different questions.
func TestSpeakRoutesACustomerKeyByItsPrefix(t *testing.T) {
	keys := &stubCustomerKeys{}
	d := guardedDeps(t)
	d.Tokens = stubTokens{}
	d.CustomerKeys = keys
	rec := postBearer(t, d, "/speak", customerKey, speakBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if len(keys.scopes) != 1 || keys.scopes[0] != httpapi.ScopeSpeak {
		t.Fatalf("scopes asked = %v, want [%s]", keys.scopes, httpapi.ScopeSpeak)
	}
}

func TestSpeakRefusesARevokedCustomerKey(t *testing.T) {
	d := guardedDeps(t)
	d.CustomerKeys = &stubCustomerKeys{err: svcauth.ErrKeyRevoked}
	if rec := postBearer(t, d, "/speak", customerKey, speakBody); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestSpeakRefusesACustomerKeyWithoutTheScope(t *testing.T) {
	d := guardedDeps(t)
	d.CustomerKeys = &stubCustomerKeys{err: appkeys.ErrMissingScope}
	if rec := postBearer(t, d, "/speak", customerKey, speakBody); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestSpeakRefusesABadCustomerKey(t *testing.T) {
	d := guardedDeps(t)
	d.CustomerKeys = &stubCustomerKeys{err: svcauth.ErrInvalidToken}
	if rec := postBearer(t, d, "/speak", customerKey, speakBody); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// A list that was never loaded, or one too old to trust, says nothing about
// the key. Accepting would bring every revoked key back when a service
// restarts during an incident; refusing would tell a customer their valid
// key is invalid. 503 says neither, and a client retries on it.
func TestSpeakCannotJudgeACustomerKeyWithoutAFreshList(t *testing.T) {
	for _, cause := range []error{svcauth.ErrRevocationUnknown, appkeys.ErrListStale} {
		d := guardedDeps(t)
		d.CustomerKeys = &stubCustomerKeys{err: cause}
		if rec := postBearer(t, d, "/speak", customerKey, speakBody); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status on %v = %d, want 503", cause, rec.Code)
		}
	}
}

// A vvaves with no provisioner credential has no list, so a customer key
// that reaches it is a key it cannot judge, not a key it refuses.
func TestSpeakWithoutCustomerKeysCannotJudgeOne(t *testing.T) {
	d := tokenDeps(t)
	if rec := postBearer(t, d, "/speak", customerKey, speakBody); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestCustomerKeysAloneCountAsAGuard(t *testing.T) {
	d := audioDeps(&stubVoice{pieces: [][]byte{[]byte("aaa")}}, nil)
	d.Unguarded = false
	d.CustomerKeys = &stubCustomerKeys{err: errors.New("refused")}
	if rec := postBearer(t, d, "/speak", customerKey, speakBody); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
