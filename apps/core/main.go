// Command vvaves is the HTTP facade over this platform's speech backend, and
// the registry of the applications allowed to speak through it.
//
// @title           Vvaves
// @version         0.3.0
// @description     Read text aloud for every product, plus the admin API of the applications registry.
// @description
// @description     The speak routes answer at the root; the admin API is under
// @description     /api/v1, which is why no global base path is declared.
// @BasePath        /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @securityDefinitions.apikey ServiceKey
// @in header
// @name X-Vvaves-Key
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lalternative/packages/go/appkeys"
	"github.com/lalternative/packages/go/audioreader"
	"github.com/lalternative/packages/go/eda/pkg/natsbus"
	"github.com/lalternative/packages/go/svcauth"
	"github.com/lalternative/packages/go/tts"

	"github.com/lalternativefabrique/vvaves/core/internal/audio"
	"github.com/lalternativefabrique/vvaves/core/internal/config"
	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
	keysapi "github.com/lalternativefabrique/vvaves/core/keys"
	"github.com/lalternativefabrique/vvaves/core/middleware"
	"github.com/lalternativefabrique/vvaves/core/pkg/db"
	"github.com/lalternativefabrique/vvaves/core/registry"
	registryinfra "github.com/lalternativefabrique/vvaves/core/registry/infrastructure"
	"github.com/lalternativefabrique/vvaves/signed"
)

func main() {
	cfg := config.Load()

	defer natsbus.CloseSharedConnection()

	reader, primer := buildAudio(cfg)

	apps, keys := buildRegistry(cfg)

	customerKeys := buildCustomerKeys(cfg)
	deps := httpapi.Deps{
		Reader:       reader,
		Primer:       primer,
		Verifier:     signed.NewLookupVerifier(keys.Keys),
		AppKeyIssuer: keys.IssuerOf,
		Tokens:       buildTokens(cfg),
		CustomerKeys: customerKeyVerifier(customerKeys),
		Unguarded:    cfg.SpeakUnguarded,
	}
	if cfg.SpeakUnguarded {
		log.Print("vvaves: SPEAK_UNGUARDED, the speak routes answer anyone who reaches them")
	}

	mux := httpapi.New(deps)
	webAuth := middleware.RequireAuth(cfg.JWTSecret)
	if apps != nil {
		apps.RegisterRoutes(mux, "/api/v1", webAuth)
	}
	keysapi.New(keyRelay(customerKeys)).RegisterRoutes(mux, "/api/keys", webAuth)

	srv := &http.Server{Addr: cfg.Addr, Handler: mux}

	go func() {
		log.Printf("vvaves: listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("vvaves: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("vvaves: shutdown: %v", err)
	}
}

// buildRegistry opens the applications registry when a database is
// configured. Without one, the environment pairs are all that speaks, which
// is what a vvaves deployed before the registry existed still runs on.
// buildTokens trusts the suite's identity provider for bearer tokens naming
// this vvaves. Nil when none is configured: the interface stays nil rather
// than holding a typed nil that would pass the guard's nil check.
func buildTokens(cfg config.Config) httpapi.BearerVerifier {
	if cfg.OIDCIssuerURL == "" {
		log.Print("vvaves: no OIDC_ISSUER_URL, bearer tokens are not accepted")
		return nil
	}
	v, err := svcauth.New([]svcauth.Issuer{svcauth.Hydra(cfg.OIDCIssuerURL, cfg.OIDCAudience)})
	if err != nil {
		log.Fatalf("vvaves: OIDC_ISSUER_URL: %v", err)
	}
	log.Printf("vvaves: bearer tokens from %s for audience %q", cfg.OIDCIssuerURL, cfg.OIDCAudience)
	return v
}

const revocationRetry = 30 * time.Second

// buildCustomerKeys wires the keys urbangate issues to this product's
// customers: verified offline against urbangate's own key set, refused when
// the revocation list says so. Nil when no provisioner credential is
// configured, and the guard then accepts no customer key.
func buildCustomerKeys(cfg config.Config) *appkeys.Keys {
	if cfg.ProvisionerClientSecret == "" {
		log.Print("vvaves: no URBANGATE_PROVISIONER_CLIENT_SECRET, customer keys are not accepted")
		return nil
	}
	if cfg.OIDCIssuerURL == "" {
		log.Fatal("vvaves: URBANGATE_PROVISIONER_CLIENT_SECRET needs OIDC_ISSUER_URL")
	}
	keys, err := appkeys.New(appkeys.Config{
		Product:   cfg.OIDCAudience,
		Urbangate: cfg.OIDCIssuerURL,
		Provisioner: svcauth.HydraClientCredentials(
			cfg.OIDCIssuerURL,
			cfg.ProvisionerClientID,
			cfg.ProvisionerClientSecret,
			[]string{"urbangate"},
			[]string{"urbangate:keys:issue"},
		),
		DefaultScopes: []string{httpapi.ScopeSpeak},
		OwnerOf:       ownerOfSession,
	})
	if err != nil {
		log.Fatalf("vvaves: customer keys: %v", err)
	}
	// An identity provider that cannot be reached is not a reason to stop
	// serving: the guard answers 503 to customer keys until the list has
	// loaded, and every other credential keeps working. Run returns when the
	// first load fails, so it is retried until it holds.
	go func() {
		for {
			err := keys.Run(context.Background())
			if err == nil {
				return
			}
			log.Printf("vvaves: revocation list: %v (retrying in %s, customer keys refused meanwhile)", err, revocationRetry)
			time.Sleep(revocationRetry)
		}
	}()
	log.Printf("vvaves: customer keys verified against %s", cfg.OIDCIssuerURL)
	return keys
}

// ownerOfSession names the person a key is issued against: the provider
// identity the web put in its token, never the local user id.
func ownerOfSession(r *http.Request) (string, bool) {
	u, ok := middleware.GetUser(r.Context())
	if !ok || u.IdentityID == "" {
		return "", false
	}
	return u.IdentityID, true
}

// customerKeyVerifier keeps the guard's nil check honest: a nil *Keys must
// not become a non-nil interface holding it.
func customerKeyVerifier(keys *appkeys.Keys) httpapi.CustomerKeyVerifier {
	if keys == nil {
		return nil
	}
	return keys
}

// keyRelay serves the key page's three calls from here, with the provisioner
// credential that never leaves this process. Without one, the page is told
// so rather than left waiting.
func keyRelay(keys *appkeys.Keys) http.Handler {
	if keys == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"no_credential"}`))
		})
	}
	return keys.Relay()
}

func buildRegistry(cfg config.Config) (*registry.Service, *registry.KeySource) {
	if cfg.DatabaseURL == "" {
		log.Print("vvaves: no DATABASE_URL, the applications registry is off")
		return nil, registry.NewKeySource(nil, nil, cfg.Keys)
	}
	cipher, err := registryinfra.NewCipherFromBase64(cfg.RegistryEncryptionKey)
	if err != nil {
		log.Fatalf("vvaves: REGISTRY_ENCRYPTION_KEY: %v", err)
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("vvaves: DATABASE_URL: %v", err)
	}
	apps, err := registry.NewService(pool, cipher, cfg.Keys)
	if err != nil {
		log.Fatalf("vvaves: registry: %v", err)
	}
	return apps, apps.Keys()
}

// buildAudio wires the reader that serves readings and the primer that reads
// their openings ahead of time.
//
// Three shapes, and each degrades into the one below it. With a voice and a
// bucket, a reading is kept and its opening can be made before anyone asks.
// With a voice alone, every listen pays for its own reading — worse, but it
// works, and it is what a vvaves with no S3 configured has always done.
// With no voice, both are nil and the speak endpoints report themselves
// unconfigured rather than failing at the first byte.
//
// A store that is configured but broken is fatal here rather than silently
// dropped: someone asked for a cache, and starting without one would hide
// that behind a bill nobody notices until it arrives.
func buildAudio(cfg config.Config) (*audioreader.Reader, *audioreader.Primer) {
	provider := audio.NewProvider(buildVoice(cfg))
	if provider == nil {
		return nil, nil
	}

	store, err := audio.NewStoreFromEnv()
	if err != nil {
		log.Fatalf("vvaves: %v", err)
	}
	if store == nil {
		log.Print("vvaves: no S3 bucket configured, every reading will be paid for")
		return audioreader.NewReader(provider, nil, cfg.AudioOpeningChars, nil), nil
	}

	// The same opening size on both: they each split the text themselves, and
	// two different sizes have the halves meet somewhere other than the same
	// cut — a word read twice, or one skipped.
	return audioreader.NewReader(provider, store, cfg.AudioOpeningChars, nil),
		audioreader.NewPrimer(provider, store, cfg.AudioOpeningChars, nil)
}

// buildVoice returns nil when no speech service is configured, which the
// speak handlers report rather than pretending to a disabled mode.
func buildVoice(cfg config.Config) tts.Voice {
	if cfg.TTSURL == "" {
		return nil
	}
	return tts.NewOpenAIVoice(tts.Config{
		BaseURL:     cfg.TTSURL,
		APIKey:      cfg.TTSAPIKey,
		Model:       cfg.TTSModel,
		VoiceID:     cfg.TTSVoice,
		Format:      cfg.TTSFormat,
		MaxChars:    cfg.TTSMaxChars,
		Concurrency: cfg.TTSConcurrency,
	})
}
