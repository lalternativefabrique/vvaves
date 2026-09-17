// Package httpapi is vvaves's HTTP contract over the search, fetch and tts
// libraries.
//
// Handlers take interfaces rather than concrete backends, so the contract —
// status codes, validation, bounds — is tested without a SearXNG instance, a
// render service or a voice behind it.
package httpapi

import (
	"github.com/lalternativefabrique/vvaves/core/docs/contract"

	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/lalternative/packages/go/audioreader"
	"github.com/lalternative/packages/go/search"
	"github.com/lalternative/packages/go/search/fetch"

	"github.com/lalternativefabrique/vvaves/core/internal/crawl"
	"github.com/lalternativefabrique/vvaves/signed"
)

// Deps are the backends the handlers speak to. A nil Reader or Renderer means
// that feature is not configured, which the handlers report rather than
// working around.
//
// A nil Primer with a non-nil Reader is the ordinary shape of a vvaves with
// no bucket: readings still stream and are still served, they are just never
// kept, and nothing can be read ahead of time.
type Deps struct {
	Providers map[search.Category]search.Provider
	Renderer  fetch.Renderer
	Cache     fetch.Cache
	Reader    *audioreader.Reader
	Primer    *audioreader.Primer

	SearchDeadline   time.Duration
	RenderMaxTimeout time.Duration

	// CrawlStore and CrawlQueue back /crawl. Nil leaves it unconfigured;
	// /map needs neither, it answers within the request.
	CrawlStore crawl.Store
	CrawlQueue crawl.Queue
	// CrawlMaxRunes bounds each rendering of a crawled page.
	CrawlMaxRunes int

	// Verifier authenticates a /speak request that came straight from a
	// browser. Nil accepts none, which is what a deployment reachable only
	// from the cluster wants.
	Verifier *signed.Verifier
	// AppKeyIssuer names the service holding the key a request presents on
	// a call of its own. Nil accepts no such call.
	AppKeyIssuer func(key string) (string, bool)
	// Tokens verifies the bearer token a service obtained from the suite's
	// identity provider. Nil accepts no such call.
	Tokens BearerVerifier
	// Unguarded lets the speak routes answer with neither of the above: a
	// deployment reachable only from inside the cluster, or a laptop. It has
	// to be said; a vvaves that forgot its keys must refuse, not serve.
	Unguarded bool
	// AllowPrivateFetch lets /fetch and /render reach an address this
	// deployment holds privately. Nil — the ordinary case — refuses them, so
	// a caller cannot spend these routes reading the internal network.
	//
	// It exists because a test serves its fixture from 127.0.0.1, and because
	// a deployment whose whole reachable network is its own may legitimately
	// want it. Both have to say so: the refusal is the default, not the
	// setting.
	AllowPrivateFetch bool
}

func New(d Deps) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", handleHealth())
	mux.Handle("GET /openapi.json", contract.Handler())
	mux.Handle("POST /search", handleSearch(d))
	mux.Handle("POST /fetch", handleFetch(d))
	mux.Handle("POST /render", handleRender(d))
	mux.Handle("POST /map", handleMap(d))
	mux.Handle("POST /crawl", handleCrawl(d))
	mux.Handle("GET /crawl/{id}", handleCrawlStatus(d))
	mux.Handle("OPTIONS /speak", handleSpeakPreflight())
	mux.Handle("POST /speak", withCORS(handleSpeak(d)))
	mux.Handle("POST /speak/prime", handlePrime(d))
	mux.Handle("POST /speak/pregenerate", handlePregenerate(d))
	mux.Handle("POST /speak/exists", handleExists(d))
	return mux
}

// maxBody bounds a request body. Every endpoint here takes a small JSON
// object except /speak, whose text is the one field that can legitimately be
// large — a whole article.
const maxBody = 4 << 20

func decode(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// handleHealth godoc
// @Summary  Liveness
// @Tags     ops
// @Produce  json
// @Success  200  {object}  healthResponse
// @Router   /healthz [get]
// @ID       healthz
func handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{OK: true})
	}
}

// healthResponse is the liveness answer.
type healthResponse struct {
	OK bool `json:"ok"`
}

// errorResponse is what every handler here answers a failure with.
type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
