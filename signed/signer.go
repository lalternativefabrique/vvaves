package signed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lalternativefabrique/vvaves/client"
)

// Signer hands a browser a URL onto vvaves's /speak for one reading.
//
// The application decides who may hear what, signs that one reading, and the
// bytes go straight from vvaves to the browser: relaying tens of seconds of
// audio through the application's own server is what this avoids.
type Signer struct {
	publicURL   string
	internalURL string
	issuer      string
	key         string
	ttl         time.Duration
	http        *http.Client
}

// SignerConfig points at the vvaves a browser fetches audio from.
type SignerConfig struct {
	// PublicURL is the origin the browser reaches vvaves on, which is not
	// the address the application calls it on from inside the cluster.
	PublicURL string
	// InternalURL is where the application calls vvaves to have a URL signed
	// for a customer key. Defaults to PublicURL.
	InternalURL string
	// Issuer names this application in the signature, so vvaves knows which
	// secret to check it against.
	Issuer string
	// Key is the application's vvaves key, the one it presents on its own
	// calls too; the signing key is derived from it.
	Key string
	// TTL is how long a handed-out URL stays valid: long enough to press play
	// on a reply that has been sitting on screen, short enough that a link
	// copied out of the network tab stops working. Defaults to 30 minutes.
	TTL time.Duration
	// Client calls /speak/sign. Defaults to one with a 10 second timeout.
	Client *http.Client
}

const defaultTTL = 30 * time.Minute

// NewSigner returns nil without a public URL, a key, or an issuer for a key
// signed locally: an unsigned URL is one vvaves refuses, so handing one out
// would only produce a player that fails on every press.
func NewSigner(cfg SignerConfig) *Signer {
	public := strings.TrimRight(strings.TrimSpace(cfg.PublicURL), "/")
	key := strings.TrimSpace(cfg.Key)
	remote := strings.HasPrefix(key, client.CustomerKeyPrefix)
	if public == "" || key == "" || (!remote && strings.TrimSpace(cfg.Issuer) == "") {
		return nil
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}
	internal := strings.TrimRight(strings.TrimSpace(cfg.InternalURL), "/")
	if internal == "" {
		internal = public
	}
	httpClient := cfg.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Signer{publicURL: public, internalURL: internal, issuer: cfg.Issuer, key: cfg.Key, ttl: ttl, http: httpClient}
}

// Sign returns where the browser may fetch this reading, and until when.
//
// vvaves verifies a customer key but never holds it, so it cannot check a
// signature derived from one: for such a key vvaves signs the URL itself.
// Any other key signs locally, as URL does.
func (s *Signer) Sign(ctx context.Context, scope, id, text string) (string, time.Time, error) {
	if !strings.HasPrefix(s.key, client.CustomerKeyPrefix) {
		u, expires := s.URL(scope, id, text)
		return u, expires, nil
	}
	body, err := json.Marshal(map[string]string{"scope": scope, "id": id, "text": text})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("signed: encode sign request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.internalURL+"/speak/sign", bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("signed: build sign request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.key)
	resp, err := s.http.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("signed: sign request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("signed: vvaves refused to sign: %s", resp.Status)
	}
	var out struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", time.Time{}, fmt.Errorf("signed: decode sign response: %w", err)
	}
	if out.URL == "" {
		return "", time.Time{}, fmt.Errorf("signed: vvaves answered no url")
	}
	return out.URL, out.ExpiresAt, nil
}

// URL signs locally, which only a key vvaves stores can verify: a customer
// key needs Sign.
//
// The signature covers the text, so the URL authorises this reading and
// nothing else: it cannot be spent having some other text synthesized.
func (s *Signer) URL(scope, id, text string) (string, time.Time) {
	expires := time.Now().Add(s.ttl)
	q := Sign(s.issuer, s.key, Params{
		Scope: scope, ID: id, TextHash: HashText(text), Expires: expires,
	})
	return s.publicURL + "/speak?" + q.Encode(), expires
}

// PublicOrigin is where the browser fetches audio, for a caller that needs to
// name it: a CSP's connect-src, say.
func (s *Signer) PublicOrigin() string {
	if s == nil {
		return ""
	}
	if u, err := url.Parse(s.publicURL); err == nil && u.Host != "" {
		return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	}
	return s.publicURL
}
