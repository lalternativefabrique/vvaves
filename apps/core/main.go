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
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalternative/packages/go/appkeys"
	"github.com/lalternative/packages/go/audioreader"
	"github.com/lalternative/packages/go/eda/pkg/consumer"
	edalogger "github.com/lalternative/packages/go/eda/pkg/logger"
	"github.com/lalternative/packages/go/eda/pkg/natsbus"
	"github.com/lalternative/packages/go/membership"
	mpgx "github.com/lalternative/packages/go/membership/pgx"
	"github.com/lalternative/packages/go/svcauth"
	"github.com/lalternative/packages/go/tts"
	"github.com/lalternative/packages/go/websession"
	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/vvaves/sdk-go/signed"
	"github.com/lalternativefabrique/vvaves/core/internal/audio"
	"github.com/lalternativefabrique/vvaves/core/internal/config"
	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
	"github.com/lalternativefabrique/vvaves/core/internal/stt"
	keysapi "github.com/lalternativefabrique/vvaves/core/keys"
	"github.com/lalternativefabrique/vvaves/core/middleware"
	"github.com/lalternativefabrique/vvaves/core/pkg/db"
	"github.com/lalternativefabrique/vvaves/core/registry"
	registryinfra "github.com/lalternativefabrique/vvaves/core/registry/infrastructure"
)

func main() {
	cfg := config.Load()

	defer natsbus.CloseSharedConnection()

	newVoice := voiceMaker(cfg)
	speech := buildAudio(cfg, newVoice, cfg.TTSVoice, "")
	voices := map[string]httpapi.Voice{}
	for key, voiceID := range cfg.TTSVoices {
		lang, _, _ := strings.Cut(key, "/")
		if lang == "*" {
			lang = ""
		}
		voices[key] = buildAudio(cfg, newVoice, voiceID, lang)
	}

	pool := openPool(cfg)
	apps, keys := buildRegistry(cfg, pool)
	members := buildMembership(cfg, pool)
	lifecycle, stopLifecycle := context.WithCancel(context.Background())
	defer stopLifecycle()

	customerKeys := buildCustomerKeys(cfg)
	signingSecret := speakSigningSecret(cfg)
	deps := httpapi.Deps{
		Reader:        speech.Reader,
		Primer:        speech.Primer,
		Voices:        voices,
		Verifier:      signed.NewLookupVerifier(httpapi.WithSigningIssuer(keys.Keys, signingSecret)),
		Tokens:        buildTokens(cfg),
		CustomerKeys:  customerKeyVerifier(customerKeys),
		Unguarded:     cfg.SpeakUnguarded,
		SigningSecret: signingSecret,
		PublicURL:     cfg.SpeakPublicURL,
	}
	if whisper := stt.New(stt.Config{URL: cfg.STTURL, APIKey: cfg.STTAPIKey, Model: cfg.STTModel}); whisper != nil {
		deps.Transcriber = whisper
	} else {
		log.Print("vvaves: STT_API_KEY is unset, /transcribe answers 503")
	}
	if cfg.SpeakUnguarded {
		log.Print("vvaves: SPEAK_UNGUARDED, the speak routes answer anyone who reaches them")
	}

	mux := httpapi.New(deps)
	webSession := buildWebSession(cfg)
	if apps != nil {
		apps.RegisterRoutes(mux, "/api/v1", middleware.RequireAdmin(webSession, members))
	}
	keysapi.New(keyRelay(customerKeys)).RegisterRoutes(mux, "/api/keys", middleware.RequireAuth(webSession, members))
	if members != nil {
		me := middleware.RequireAuth(webSession, members)(members.MeHandler(identityOfSession))
		mux.Handle("GET /api/v1/me/membership", me)
		mux.Handle("DELETE /api/v1/me", me)
		go members.RunWorker(lifecycle, time.Second)
		go consumeDeletions(lifecycle, cfg, members)
	}

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
		DefaultScopes: []string{httpapi.ScopeSpeak, httpapi.ScopeTranscribe},
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

// buildWebSession names the person behind the admin API and the key routes
// from the access token urbangate issued them for vvaves. Nil without an
// issuer, and both then answer nobody.
func buildWebSession(cfg config.Config) *websession.Guard {
	if cfg.OIDCIssuerURL == "" {
		log.Print("vvaves: no OIDC_ISSUER_URL, the admin API and the key routes are closed")
		return nil
	}
	g, err := websession.New(websession.Config{
		Product:   cfg.OIDCAudience,
		Urbangate: cfg.OIDCIssuerURL,
	})
	if err != nil {
		log.Fatalf("vvaves: admin API issuer: %v", err)
	}
	log.Printf("vvaves: admin API tokens from %s", cfg.OIDCIssuerURL)
	return g
}

// ownerOfSession names the person a key is issued against: their identity at
// urbangate.
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

func openPool(cfg config.Config) *pgxpool.Pool {
	if cfg.DatabaseURL == "" {
		return nil
	}
	pool, err := db.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("vvaves: DATABASE_URL: %v", err)
	}
	return pool
}

// buildMembership keeps who is a member of vvaves (urbangate ADR 0013):
// opened by the first token, erased on the suite's deletion event. vvaves
// holds nothing else for a person, their keys live at urbangate.
func buildMembership(cfg config.Config, pool *pgxpool.Pool) *membership.Service {
	if pool == nil {
		log.Print("vvaves: no DATABASE_URL, the membership is off and every token is admitted")
		return nil
	}
	store := mpgx.New(pool)
	members, err := membership.New(store, membership.Product{
		Name:      cfg.OIDCAudience,
		Resources: membership.NewNoop(store),
	}, membership.Config{
		Logger: slog.Default(),
		OnStuck: func(m membership.Member, err error) {
			slog.Error("member stuck", "identity_id", m.IdentityID, "state", m.State, "attempts", m.Attempts, "error", err)
		},
	})
	if err != nil {
		log.Fatalf("vvaves: membership: %v", err)
	}
	return members
}

func identityOfSession(r *http.Request) (membership.Identity, bool) {
	u, ok := middleware.GetUser(r.Context())
	if !ok || u.IdentityID == "" {
		return membership.Identity{}, false
	}
	return membership.Identity{ID: u.IdentityID, Email: u.Email, Name: u.Name}, true
}

// consumeDeletions erases the members urbangate says are leaving, from the
// suite's shared bus (urbangate ADR 0006). A bus that cannot be reached must
// not take vvaves down: the connection keeps retrying in the background.
func consumeDeletions(ctx context.Context, cfg config.Config, members *membership.Service) {
	if cfg.SuiteNATSURL == "" {
		log.Print("vvaves: SUITE_NATS_URL is unset, account deletions requested through urbangate do not reach vvaves")
		return
	}
	opts := []nats.Option{nats.MaxReconnects(-1), nats.ReconnectWait(2 * time.Second), nats.Name("vvaves-core"), nats.RetryOnFailedConnect(true)}
	if cfg.SuiteNATSUser != "" {
		opts = append(opts, nats.UserInfo(cfg.SuiteNATSUser, cfg.SuiteNATSPassword))
	}
	bus, err := nats.Connect(cfg.SuiteNATSURL, opts...)
	if err != nil {
		log.Printf("vvaves: suite bus: %v (retrying in the background)", err)
	}
	defer bus.Drain()
	consumer.Run(ctx, bus, members.DeletionHandler(), consumer.Config{
		StreamName:        "EVENTS",
		DLQStreamName:     "DLQ",
		StreamProvisioned: true,
		Logger:            edalogger.NewJSONSlogLogger(slog.LevelInfo),
	})
}

func buildRegistry(cfg config.Config, pool *pgxpool.Pool) (*registry.Service, *registry.KeySource) {
	if pool == nil {
		log.Print("vvaves: no DATABASE_URL, the applications registry is off")
		return nil, registry.NewKeySource(nil, nil, cfg.Keys)
	}
	cipher, err := registryinfra.NewCipherFromBase64(cfg.RegistryEncryptionKey)
	if err != nil {
		log.Fatalf("vvaves: REGISTRY_ENCRYPTION_KEY: %v", err)
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
func buildAudio(cfg config.Config, newVoice func(voiceID, lang string) tts.Voice, voiceID, lang string) httpapi.Voice {
	provider := audio.NewProvider(newVoice(voiceID, lang))
	if provider == nil {
		return httpapi.Voice{}
	}

	store, err := audio.NewStoreFromEnv(cacheNamespace(cfg, voiceID, lang))
	if err != nil {
		log.Fatalf("vvaves: %v", err)
	}
	if store == nil {
		log.Print("vvaves: no S3 bucket configured, every reading will be paid for")
		return httpapi.Voice{Reader: audioreader.NewReader(provider, nil, cfg.AudioOpeningChars, nil)}
	}

	// The same opening size on both: they each split the text themselves, and
	// two different sizes have the halves meet somewhere other than the same
	// cut — a word read twice, or one skipped.
	return httpapi.Voice{
		Reader: audioreader.NewReader(provider, store, cfg.AudioOpeningChars, nil),
		Primer: audioreader.NewPrimer(provider, store, cfg.AudioOpeningChars, nil),
	}
}

// voiceMaker returns how to build a voice for a language, nil when no
// speech service is configured, which the speak handlers report rather than
// pretending to a disabled mode.
func voiceMaker(cfg config.Config) func(voiceID, lang string) tts.Voice {
	base := tts.Config{
		APIKey:      cfg.TTSAPIKey,
		Model:       cfg.TTSModel,
		Format:      cfg.TTSFormat,
		MaxChars:    cfg.TTSMaxChars,
		Concurrency: cfg.TTSConcurrency,
	}
	if cfg.TTSProvider != "" && cfg.TTSAPIKey == "" {
		log.Fatalf("vvaves: TTS_PROVIDER=%s needs TTS_API_KEY", cfg.TTSProvider)
	}

	switch cfg.TTSProvider {
	case "":
		return func(voiceID, _ string) tts.Voice {
			if cfg.TTSURL == "" {
				return nil
			}
			voiceCfg := base
			voiceCfg.BaseURL = cfg.TTSURL
			voiceCfg.VoiceID = voiceID
			return tts.NewOpenAIVoice(voiceCfg)
		}
	case "elevenlabs":
		account := audio.NewElevenLabs("", cfg.TTSAPIKey, cfg.TTSProviderConcurrency)
		return func(voiceID, lang string) tts.Voice {
			voiceCfg := base
			voiceCfg.VoiceID = requireVoice(cfg, voiceID)
			if voiceCfg.MaxChars == tts.WholeText {
				voiceCfg.MaxChars = audio.ElevenLabsMaxChars
			}
			voice, err := account.Voice(voiceCfg, lang)
			if err != nil {
				log.Fatalf("vvaves: %v", err)
			}
			return voice
		}
	case "mistral":
		return func(voiceID, _ string) tts.Voice {
			voiceCfg := base
			voiceCfg.VoiceID = requireVoice(cfg, voiceID)
			// Mistral answers a request only once it has read all of it, so
			// WholeText would keep a listener waiting on the whole page.
			if voiceCfg.MaxChars == tts.WholeText {
				voiceCfg.MaxChars = mistralMaxChars
			}
			return audio.NewMistralVoice(voiceCfg)
		}
	default:
		log.Fatalf("vvaves: unknown TTS_PROVIDER %q", cfg.TTSProvider)
		return nil
	}
}

func requireVoice(cfg config.Config, voiceID string) string {
	if voiceID == "" {
		log.Fatalf("vvaves: TTS_PROVIDER=%s needs TTS_VOICE", cfg.TTSProvider)
	}
	return voiceID
}

// Mistral reads best under ~300 words per request.
const mistralMaxChars = 1000

// cacheNamespace keeps the self-hosted voice's readings where they always
// were, and puts any other's under everything that changes the audio: the
// provider, model, voice and enforced language.
func cacheNamespace(cfg config.Config, voiceID, lang string) string {
	if cfg.TTSProvider == "" && voiceID == cfg.TTSVoice {
		return ""
	}
	ns := cfg.TTSProvider + "/"
	if cfg.TTSModel != "" {
		ns += cfg.TTSModel + "/"
	}
	ns += voiceID + "/"
	if lang != "" {
		ns += lang + "/"
	}
	return ns
}

// speakSigningSecret falls back to a secret derived from the registry's key,
// under its own label, so a deployment signs without a new secret to manage.
func speakSigningSecret(cfg config.Config) string {
	if cfg.SpeakSigningSecret != "" {
		return cfg.SpeakSigningSecret
	}
	if cfg.RegistryEncryptionKey == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(cfg.RegistryEncryptionKey))
	mac.Write([]byte("vvaves/speak-signing/v1"))
	return hex.EncodeToString(mac.Sum(nil))
}
