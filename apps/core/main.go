// Command vvaves is the HTTP facade over this platform's search, page
// extraction, JavaScript rendering and speech backends, and the registry of
// the applications allowed to speak through it.
//
// @title           Vvaves
// @version         0.3.0
// @description     Search, fetch, render and speak for every product, plus the admin API of the applications registry.
// @description
// @description     The web and speak routes answer at the root; the admin API is
// @description     under /api/v1, which is why no global base path is declared.
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
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lalternative/packages/go/audioreader"
	"github.com/lalternative/packages/go/eda/pkg/logger"
	"github.com/lalternative/packages/go/eda/pkg/natsbus"
	"github.com/lalternative/packages/go/search"
	"github.com/lalternative/packages/go/search/brave"
	"github.com/lalternative/packages/go/search/fetch"
	"github.com/lalternative/packages/go/search/searxng"
	"github.com/lalternative/packages/go/svcauth"
	"github.com/lalternative/packages/go/tts"

	"github.com/lalternativefabrique/vvaves/core/internal/audio"
	"github.com/lalternativefabrique/vvaves/core/internal/challenge"
	"github.com/lalternativefabrique/vvaves/core/internal/config"
	"github.com/lalternativefabrique/vvaves/core/internal/crawl"
	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
	"github.com/lalternativefabrique/vvaves/core/internal/pagecache"
	"github.com/lalternativefabrique/vvaves/core/internal/render"
	"github.com/lalternativefabrique/vvaves/core/middleware"
	"github.com/lalternativefabrique/vvaves/core/pkg/db"
	"github.com/lalternativefabrique/vvaves/core/registry"
	registryinfra "github.com/lalternativefabrique/vvaves/core/registry/infrastructure"
	"github.com/lalternativefabrique/vvaves/signed"
)

func main() {
	cfg := config.Load()

	// A page refused on our egress is refused whatever headers we send, so a
	// proxy that was meant to be configured and is not means /fetch quietly
	// reads a fraction of the web it is asked for. Fatal rather than a
	// warning: the value is a deployment's decision, and a typo in it must
	// not look like a working service.
	if err := fetch.UseProxy(cfg.FetchProxy); err != nil {
		log.Fatalf("vvaves: FETCH_PROXY: %v", err)
	}
	log.Printf("vvaves: page fetch proxy %s", fetch.ProxyState())

	browser, err := render.New(cfg.ChromiumPath, cfg.FetchProxy)
	if err != nil {
		log.Fatalf("vvaves: FETCH_PROXY: %v", err)
	}
	defer browser.Close()

	defer natsbus.CloseSharedConnection()

	reader, primer := buildAudio(cfg)

	apps, keys := buildRegistry(cfg)

	crawlStore, crawlQueue := buildCrawl(cfg)

	deps := httpapi.Deps{
		Providers:         buildProviders(cfg),
		Renderer:          browser,
		Cache:             challenge.GuardCache(buildPageCache(cfg)),
		CrawlStore:        crawlStore,
		CrawlQueue:        crawlQueue,
		CrawlMaxRunes:     cfg.CrawlMaxRunes,
		Reader:            reader,
		Primer:            primer,
		SearchDeadline:    cfg.SearchDeadline,
		RenderMaxTimeout:  cfg.RenderMaxTimeout,
		Verifier:          signed.NewLookupVerifier(keys.Keys),
		AppKeyIssuer:      keys.IssuerOf,
		Tokens:            buildTokens(cfg),
		Unguarded:         cfg.SpeakUnguarded,
		AllowPrivateFetch: cfg.FetchAllowPrivate,
	}
	if cfg.FetchAllowPrivate {
		log.Print("vvaves: FETCH_ALLOW_PRIVATE, /fetch and /render may reach private addresses")
	}
	if cfg.SpeakUnguarded {
		log.Print("vvaves: SPEAK_UNGUARDED, the speak routes answer anyone who reaches them")
	}

	mux := httpapi.New(deps)
	if apps != nil {
		apps.RegisterRoutes(mux, "/api/v1", middleware.RequireAuth(cfg.JWTSecret))
	}

	srv := &http.Server{Addr: cfg.Addr, Handler: mux}

	crawlCtx, stopCrawls := context.WithCancel(context.Background())
	defer stopCrawls()
	go func() {
		if err := httpapi.Crawler(deps).Run(crawlCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("vvaves: crawl runner stopped: %v", err)
		}
	}()

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

// buildProviders wires one provider per category. Brave has no academic index
// of its own, so it backs up only the general category — where a self-hosted
// SearXNG actually breaks, an upstream engine being rate-limited or blocked.
func buildProviders(cfg config.Config) map[search.Category]search.Provider {
	if cfg.SearxngURL == "" {
		return nil
	}
	providers := map[search.Category]search.Provider{
		search.CategoryGeneral:  searxng.New(cfg.SearxngURL, nil),
		search.CategoryAcademic: searxng.New(cfg.SearxngURL, nil),
	}
	if cfg.BraveAPIKey != "" {
		providers[search.CategoryGeneral] = search.WithFallback(
			providers[search.CategoryGeneral], brave.New(cfg.BraveAPIKey, nil))
	}
	return providers
}

// buildPageCache shares fetched pages across replicas through NATS when a
// broker is configured, and keeps them in this process otherwise. A broker
// that is configured but unreachable is fatal: someone asked for a shared
// cache, and each replica quietly refetching the same pages would hide that.
//
// The connection is natsbus's process-wide one, which reads NATS_URL itself;
// cfg.NatsURL is the same value and only decides whether to connect at all.
func buildPageCache(cfg config.Config) fetch.Cache {
	if cfg.NatsURL == "" {
		return fetch.NewMemoryCache(cfg.FetchCacheTTL)
	}
	natsbus.SetLogger(logger.NewJSONSlogLogger(slog.LevelInfo))
	nc, _, err := natsbus.GetSharedConnection()
	if err != nil {
		log.Fatalf("vvaves: NATS_URL: %v", err)
	}
	cache, err := pagecache.NewNATS(nc, cfg.FetchCacheTTL, cfg.FetchCacheMaxBytes)
	if err != nil {
		log.Fatalf("vvaves: page cache: %v", err)
	}
	log.Printf("vvaves: page cache shared through %s", cfg.NatsURL)
	return cache
}

// buildCrawl keeps crawl jobs in NATS when a broker is configured, so any
// replica runs a job and any replica answers for it; without one they stay
// in this process, which a laptop is fine with.
func buildCrawl(cfg config.Config) (crawl.Store, crawl.Queue) {
	if cfg.NatsURL == "" {
		return crawl.NewMemoryStore(), crawl.NewMemoryQueue()
	}
	nc, _, err := natsbus.GetSharedConnection()
	if err != nil {
		log.Fatalf("vvaves: NATS_URL: %v", err)
	}
	store, err := crawl.NewNATSStore(nc, cfg.CrawlMaxBytes)
	if err != nil {
		log.Fatalf("vvaves: crawl store: %v", err)
	}
	queue, err := crawl.NewNATSQueue(nc)
	if err != nil {
		log.Fatalf("vvaves: crawl queue: %v", err)
	}
	return store, queue
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
