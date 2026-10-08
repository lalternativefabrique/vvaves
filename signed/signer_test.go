package signed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewSignerNeedsAURLAnIssuerAndAKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  SignerConfig
	}{
		{"nothing", SignerConfig{}},
		{"no key", SignerConfig{PublicURL: "https://audio.example", Issuer: issuer}},
		{"no url", SignerConfig{Issuer: issuer, Key: key}},
		{"no issuer", SignerConfig{PublicURL: "https://audio.example", Key: key}},
		{"blank", SignerConfig{PublicURL: "  ", Issuer: " ", Key: "  "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewSigner(tc.cfg); got != nil {
				t.Error("NewSigner returned non-nil with nothing to hand out")
			}
		})
	}
}

// The URL is what the server will check, so it has to verify against the
// same secret: this is the contract between the two sides.
func TestSignedURLVerifies(t *testing.T) {
	s := NewSigner(SignerConfig{PublicURL: "https://audio.example/", Issuer: issuer, Key: key})

	raw, expires := s.URL("chat-message", "msg-42", "bonjour")
	if !strings.HasPrefix(raw, "https://audio.example/speak?") {
		t.Fatalf("URL = %q, want it to point at /speak on the public origin", raw)
	}
	if time.Until(expires) <= 0 {
		t.Fatal("the URL expired before it was handed out")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v := NewVerifier(map[string]string{issuer: key})
	if err := v.Verify(parsed.Query(), "chat-message", "msg-42", "bonjour"); err != nil {
		t.Fatalf("the server would refuse the URL we handed out: %v", err)
	}
}

func TestSignedURLDoesNotAuthoriseAnotherText(t *testing.T) {
	s := NewSigner(SignerConfig{PublicURL: "https://audio.example", Issuer: issuer, Key: key})

	raw, _ := s.URL("chat-message", "msg-42", "bonjour")
	parsed, _ := url.Parse(raw)

	v := NewVerifier(map[string]string{issuer: key})
	if err := v.Verify(parsed.Query(), "chat-message", "msg-42", "something else"); err == nil {
		t.Fatal("the URL authorised a text it was not signed for")
	}
}

func TestSignedURLExpiresOnTheConfiguredTTL(t *testing.T) {
	s := NewSigner(SignerConfig{PublicURL: "https://audio.example", Issuer: issuer, Key: key, TTL: time.Minute})

	_, expires := s.URL("chat-message", "msg-42", "bonjour")
	if d := time.Until(expires); d > time.Minute+time.Second || d < 50*time.Second {
		t.Errorf("expiry in %v, want about a minute", d)
	}
}

func TestPublicOriginDropsThePath(t *testing.T) {
	s := NewSigner(SignerConfig{PublicURL: "https://audio.example/base", Issuer: issuer, Key: key})
	if got := s.PublicOrigin(); got != "https://audio.example" {
		t.Errorf("PublicOrigin = %q", got)
	}
	var none *Signer
	if got := none.PublicOrigin(); got != "" {
		t.Errorf("nil signer origin = %q, want empty", got)
	}
}

func TestSignSendsACustomerKeyToVvaves(t *testing.T) {
	const customer = "vvaves_key_abc.def.ghi"
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		w.Write([]byte(`{"url":"https://audio.example/speak?iss=vvaves","expires_at":"2030-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()
	s := NewSigner(SignerConfig{PublicURL: "https://audio.example", InternalURL: srv.URL, Key: customer})
	u, exp, err := s.Sign(context.Background(), "chat", "m1", "bonjour")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer "+customer || gotPath != "/speak/sign" {
		t.Fatalf("auth = %q, path = %q", gotAuth, gotPath)
	}
	if u != "https://audio.example/speak?iss=vvaves" || exp.Year() != 2030 {
		t.Fatalf("url = %q, expires = %v", u, exp)
	}
}

func TestSignReportsARefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	s := NewSigner(SignerConfig{PublicURL: srv.URL, Key: "vvaves_key_x"})
	if _, _, err := s.Sign(context.Background(), "chat", "m1", "bonjour"); err == nil {
		t.Fatal("want an error")
	}
}

func TestSignKeepsALocalKeyLocal(t *testing.T) {
	s := NewSigner(SignerConfig{PublicURL: "http://127.0.0.1:1", Issuer: issuer, Key: key})
	u, _, err := s.Sign(context.Background(), "chat", "m1", "bonjour")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(u)
	if err := NewVerifier(map[string]string{issuer: key}).Verify(parsed.Query(), "chat", "m1", "bonjour"); err != nil {
		t.Fatalf("local signature: %v", err)
	}
}
