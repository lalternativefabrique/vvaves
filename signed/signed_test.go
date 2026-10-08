package signed

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	issuer = "lalter"
	key    = "a-shared-secret"
)

func verifier(t *testing.T, now time.Time) *Verifier {
	t.Helper()
	v := NewVerifier(map[string]string{issuer: key})
	if v == nil {
		t.Fatal("NewVerifier returned nil with a key configured")
	}
	v.now = func() time.Time { return now }
	return v
}

func TestSignedRequestVerifies(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := Sign(issuer, key, Params{
		Scope: "chat-message", ID: "msg-42",
		TextHash: HashText("bonjour"),
		Expires:  now.Add(5 * time.Minute),
	})

	if err := verifier(t, now).Verify(q, "chat-message", "msg-42", "bonjour"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestNewVerifierWithoutKeysIsNil(t *testing.T) {
	if NewVerifier(nil) != nil {
		t.Error("NewVerifier(nil) is not nil")
	}
	if NewVerifier(map[string]string{"lalter": ""}) != nil {
		t.Error("an empty key was accepted")
	}
}

// A nil Verifier is a deployment that accepts no browser call at all, so it
// must reject rather than wave everything through.
func TestNilVerifierRejects(t *testing.T) {
	var v *Verifier
	if err := v.Verify(url.Values{}, "s", "i", "t"); !errors.Is(err, ErrNoSignature) {
		t.Fatalf("Verify on nil = %v, want ErrNoSignature", err)
	}
}

func TestUnsignedRequestIsRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if err := verifier(t, now).Verify(url.Values{}, "chat-message", "msg-42", "bonjour"); !errors.Is(err, ErrNoSignature) {
		t.Fatalf("Verify(unsigned) = %v, want ErrNoSignature", err)
	}
}

func TestExpiredSignatureIsRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := Sign(issuer, key, Params{
		Scope: "chat-message", ID: "msg-42",
		TextHash: HashText("bonjour"),
		Expires:  now.Add(-time.Second),
	})

	if err := verifier(t, now).Verify(q, "chat-message", "msg-42", "bonjour"); !errors.Is(err, ErrExpired) {
		t.Fatalf("Verify(expired) = %v, want ErrExpired", err)
	}
}

func TestUnknownIssuerIsRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := Sign("someone-else", key, Params{
		Scope: "chat-message", ID: "msg-42",
		TextHash: HashText("bonjour"),
		Expires:  now.Add(time.Minute),
	})

	if err := verifier(t, now).Verify(q, "chat-message", "msg-42", "bonjour"); !errors.Is(err, ErrUnknownIssue) {
		t.Fatalf("Verify(unknown issuer) = %v, want ErrUnknownIssue", err)
	}
}

func TestSignatureFromAnotherKeyIsRejected(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := Sign(issuer, "not-the-key", Params{
		Scope: "chat-message", ID: "msg-42",
		TextHash: HashText("bonjour"),
		Expires:  now.Add(time.Minute),
	})

	if err := verifier(t, now).Verify(q, "chat-message", "msg-42", "bonjour"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify(forged) = %v, want ErrBadSignature", err)
	}
}

// The whole point of covering the text: a signature handed out for one
// reading must not authorise having anything else read aloud, which would
// turn a link into free synthesis on someone else's account.
func TestSignatureDoesNotCoverAnotherText(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := Sign(issuer, key, Params{
		Scope: "chat-message", ID: "msg-42",
		TextHash: HashText("bonjour"),
		Expires:  now.Add(time.Minute),
	})

	if err := verifier(t, now).Verify(q, "chat-message", "msg-42", "read this instead"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify(swapped text) = %v, want ErrBadSignature", err)
	}
}

func TestSignatureDoesNotCoverAnotherMessage(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := Sign(issuer, key, Params{
		Scope: "chat-message", ID: "msg-42",
		TextHash: HashText("bonjour"),
		Expires:  now.Add(time.Minute),
	})

	if err := verifier(t, now).Verify(q, "chat-message", "msg-43", "bonjour"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify(swapped id) = %v, want ErrBadSignature", err)
	}
}

func TestSignatureDoesNotCoverAnotherScope(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := Sign(issuer, key, Params{
		Scope: "chat-message", ID: "msg-42",
		TextHash: HashText("bonjour"),
		Expires:  now.Add(time.Minute),
	})

	if err := verifier(t, now).Verify(q, "published-page", "msg-42", "bonjour"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify(swapped scope) = %v, want ErrBadSignature", err)
	}
}

// Moving the expiry must invalidate the signature, or a link would never
// really expire: a listener could push the deadline out by editing the URL.
func TestExtendingTheExpiryBreaksTheSignature(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := Sign(issuer, key, Params{
		Scope: "chat-message", ID: "msg-42",
		TextHash: HashText("bonjour"),
		Expires:  now.Add(-time.Hour),
	})
	q.Set(QueryExpires, "9999999999")

	if err := verifier(t, now).Verify(q, "chat-message", "msg-42", "bonjour"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify(extended) = %v, want ErrBadSignature", err)
	}
}

// Two applications must not be able to verify each other's links.
func TestIssuersAreIsolated(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	v := NewVerifier(map[string]string{"lalter": "key-a", "partage": "key-b"})
	v.now = func() time.Time { return now }

	q := Sign("lalter", "key-a", Params{
		Scope: "chat-message", ID: "msg-42",
		TextHash: HashText("bonjour"),
		Expires:  now.Add(time.Minute),
	})
	if err := v.Verify(q, "chat-message", "msg-42", "bonjour"); err != nil {
		t.Fatalf("Verify(own issuer): %v", err)
	}

	// The same signature, relabelled as the other application's.
	q.Set(QueryIssuer, "partage")
	if err := v.Verify(q, "chat-message", "msg-42", "bonjour"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify(relabelled) = %v, want ErrBadSignature", err)
	}
}

// The fields are joined before being signed, so a separator that could appear
// inside a value would let two different sets of parameters sign the same.
func TestFieldsCannotBeShiftedAcrossTheSeparator(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	expires := now.Add(time.Minute)

	a := sign([]byte(key), Params{Scope: "chat", ID: "message-42", TextHash: "h", Expires: expires})
	b := sign([]byte(key), Params{Scope: "chat-message", ID: "42", TextHash: "h", Expires: expires})
	if a == b {
		t.Fatal("two different parameter sets produced the same signature")
	}
}

// A lookup verifier accepts every secret an issuer currently holds, which is
// what lets a rotated-out key keep verifying for its grace period, and
// refuses an issuer the lookup no longer knows.
func TestLookupVerifierAcceptsEveryCurrentSecret(t *testing.T) {
	now := time.Now()
	keys := map[string][]string{issuer: {"new-secret", key}}
	v := NewLookupVerifier(func(name string) []string { return keys[name] })
	old := Sign(issuer, key, Params{Scope: "s", ID: "id", TextHash: HashText("text"), Expires: now.Add(time.Hour)})
	fresh := Sign(issuer, "new-secret", Params{Scope: "s", ID: "id", TextHash: HashText("text"), Expires: now.Add(time.Hour)})
	for name, q := range map[string]url.Values{"previous": old, "current": fresh} {
		if err := v.Verify(q, "s", "id", "text"); err != nil {
			t.Errorf("%s secret refused: %v", name, err)
		}
	}
	delete(keys, issuer)
	if err := v.Verify(fresh, "s", "id", "text"); !errors.Is(err, ErrUnknownIssue) {
		t.Fatalf("revoked issuer: err = %v, want ErrUnknownIssue", err)
	}
}

func TestFieldsTheCanonicalFormCannotSeparateAreRefused(t *testing.T) {
	v := NewVerifier(map[string]string{"app": "k"})
	exp := time.Now().Add(time.Minute)
	q := Sign("app", "k", Params{Scope: "a\nid=b", TextHash: HashText("x"), Expires: exp})
	if err := v.Verify(q, "a\nid=b", "", "x"); !errors.Is(err, ErrBadSignature) {
		t.Errorf("newline in scope: %v, want ErrBadSignature", err)
	}
	for _, p := range []Params{{ID: "x\x00"}, {Scope: strings.Repeat("s", MaxFieldLen+1)}} {
		if !errors.Is(p.Validate(), ErrInvalidField) {
			t.Errorf("Validate(%q,%q) accepted", p.Scope, p.ID)
		}
	}
	if err := (Params{Scope: "synthiz", ID: "reply-42"}).Validate(); err != nil {
		t.Errorf("ordinary fields refused: %v", err)
	}
}
