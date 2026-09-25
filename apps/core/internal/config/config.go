// Package config reads vvaves's settings from the environment.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lalternative/packages/go/tts"
)

type Config struct {
	Addr string

	// NatsURL, read from NATS_URL, points the page cache at a JetStream KV
	// bucket every replica shares. Empty keeps the cache in this process.
	// FetchCacheMaxBytes bounds the shared bucket, read from
	// FETCH_CACHE_MAX_BYTES; JetStream evicts the oldest pages past it.
	// CrawlMaxRunes bounds each rendering of a crawled page, read from
	// CRAWL_MAX_RUNES. CrawlMaxBytes bounds the bucket crawled pages wait in,
	// read from CRAWL_MAX_BYTES.

	TTSURL         string
	TTSAPIKey      string
	TTSModel       string
	TTSVoice       string
	TTSFormat      string
	TTSMaxChars    int
	TTSConcurrency int

	AudioOpeningChars int

	// Keys are the applications' keys read from SPEAK_KEYS as "issuer:key"
	// pairs, an issuer repeatable. One key does both jobs: presented on
	// X-Vvaves-Key by the application's server, and the root the signatures
	// on its browser-bound /speak URLs derive from. Empty admits nobody the
	// registry does not, which is what a deployment reachable only from the
	// cluster wants.
	Keys map[string][]string

	// DatabaseURL opens the registry of applications and the admin's own
	// accounts. Empty runs without either: the environment pairs above are
	// then all that speaks, as before the registry existed.
	DatabaseURL string
	// OIDCIssuerURL is the suite's identity provider, read from
	// OIDC_ISSUER_URL. A service presents a bearer token it obtained there
	// instead of an app key; the token must name OIDCAudience. Empty accepts
	// no token.
	OIDCIssuerURL string
	// OIDCAudience is the name this vvaves answers to in a token's aud,
	// read from OIDC_AUDIENCE, "vvaves" by default.
	OIDCAudience string
	// ProvisionerClientID and ProvisionerClientSecret are this product's
	// credential at the identity provider, read from
	// URBANGATE_PROVISIONER_CLIENT_ID and URBANGATE_PROVISIONER_CLIENT_SECRET.
	// They read the list of revoked customer keys; no secret accepts no
	// customer key at all.
	ProvisionerClientID     string
	ProvisionerClientSecret string
	// SpeakUnguarded lets the speak routes answer with no key at all, read
	// from SPEAK_UNGUARDED=true. For a vvaves nothing outside the cluster
	// reaches, and for a laptop; never for one behind a public name.
	SpeakUnguarded bool
	// RegistryEncryptionKey seals the applications' keys at rest, a base64
	// 32-byte key. Required with a database: a registry that stores secrets
	// in the clear is one that must not start.
	RegistryEncryptionKey string
}

func Load() Config {
	return Config{
		Addr: env("LISTEN_ADDR", ":8080"),

		TTSURL:    os.Getenv("PIPER_URL"),
		TTSAPIKey: os.Getenv("TTS_API_KEY"),
		TTSModel:  os.Getenv("TTS_MODEL"),
		TTSVoice:  os.Getenv("TTS_VOICE"),
		TTSFormat: env("TTS_FORMAT", "mp3"),
		// WholeText sends every text as one request: the speech server takes any
		// length, cuts by sentence itself and streams each one as it is read,
		// so a cut here only adds requests, and each one a prompt prefill and
		// a seam. A positive value cuts at that many characters, which a
		// hosted endpoint would need; 120 was the measured optimum when Piper
		// answered a request only once it had read all of it.
		TTSMaxChars: envInt("TTS_MAX_CHARS", tts.WholeText),
		// One request now covers a reading, and the speech server takes one
		// slot per request: reading pieces concurrently would spend a
		// listener's slots on their own text.
		TTSConcurrency: envInt("TTS_CONCURRENCY", 1),

		// Not TTSMaxChars, though both are a number of characters. That one is
		// how much text goes to the speech server at once; this is how much of
		// a text counts as its opening, and the primer and the reader must
		// agree on it exactly — they each split the text themselves, and a
		// disagreement has the two halves meet somewhere other than the same
		// cut, reading a word twice or skipping one.
		AudioOpeningChars: envInt("AUDIO_OPENING_CHARS", 800),

		Keys: envPairs("SPEAK_KEYS"),

		DatabaseURL:           os.Getenv("DATABASE_URL"),
		RegistryEncryptionKey: os.Getenv("REGISTRY_ENCRYPTION_KEY"),
		SpeakUnguarded:        os.Getenv("SPEAK_UNGUARDED") == "true",
		OIDCIssuerURL:         os.Getenv("OIDC_ISSUER_URL"),
		OIDCAudience:          envString("OIDC_AUDIENCE", "vvaves"),

		ProvisionerClientID:     envString("URBANGATE_PROVISIONER_CLIENT_ID", "vvaves-provisioner"),
		ProvisionerClientSecret: os.Getenv("URBANGATE_PROVISIONER_CLIENT_SECRET"),
	}
}

// envPairs reads "issuer:key,issuer:key" into a map. A malformed entry
// is dropped rather than guessed at: a key read wrong is a key that rejects
// every signature made with it, and silence about it would look like the
// application signing incorrectly.
func envPairs(key string) map[string][]string {
	out := map[string][]string{}
	for _, entry := range strings.Split(os.Getenv(key), ",") {
		issuer, secret, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok || issuer == "" || secret == "" {
			continue
		}
		out[issuer] = append(out[issuer], secret)
	}
	return out
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if v, err := strconv.ParseInt(os.Getenv(key), 10, 64); err == nil && v > 0 {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if ms, err := strconv.Atoi(os.Getenv(key)); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return fallback
}
