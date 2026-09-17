package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"time"

	"github.com/lalternative/packages/go/audioreader"
)

type speakRequest struct {
	Text string `json:"text"`
	// Scope and ID name where a reading is kept. Both optional: an ID lets a
	// caller prime a reading before anyone asks for it, since priming means
	// naming ahead of time what will be listened to. Without one the reading
	// is still cached, keyed by the text alone — a second listen of the same
	// words finds it, which no caller has to opt into.
	Scope  string `json:"scope"`
	ID     string `json:"id"`
	Stream bool   `json:"stream"`
}

const defaultScope = "speak"

// primeTimeout bounds work nobody is waiting for. Detached from the request
// that asked for it, which is answered before the reading starts.
const primeTimeout = 2 * time.Minute

// request builds the reading this asks for, defaulting the names it leaves
// out. An absent ID becomes the text's own hash, which makes the reading
// content-addressed and nothing else: it cannot be primed, because a caller
// that does not name a text now cannot name the same one later.
func (r speakRequest) request() audioreader.Request {
	scope, id := r.Scope, r.ID
	if scope == "" {
		scope = defaultScope
	}
	if id == "" {
		sum := sha256.Sum256([]byte(r.Text))
		id = hex.EncodeToString(sum[:])[:16]
	}
	return audioreader.Request{Scope: scope, ID: id, Text: r.Text}
}

// decodeSpeak reads and validates a speak-shaped body, reporting whether the
// handler should carry on.
func decodeSpeak(w http.ResponseWriter, r *http.Request) (speakRequest, bool) {
	var req speakRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return req, false
	}
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return req, false
	}
	return req, true
}

// handleSpeak serves a reading: from the cache when one is kept, otherwise by
// reading the text aloud and keeping the result.
//
// A cached reading is served whole, with a Content-Length and byte ranges, so
// a second listen starts at once and can be seeked. Only the listen that pays
// for the reading streams, and only when it asks to.
// handleSpeak godoc
// @Summary  Read a text aloud, from the store when it is already there
// @Description Answers audio bytes, not JSON. With stream=false the whole
// @Description reading comes back with a Content-Length and ranges; with
// @Description stream=true each piece is emitted as it is read, length-prefixed
// @Description under application/x-lalter-audio-frames.
// @Tags     speak
// @Accept   json
// @Produce  audio/mpeg
// @Param    body  body      speakRequest  true  "text to read"
// @Success  200   {file}    binary
// @Failure  400   {object}  errorResponse
// @Failure  401   {object}  errorResponse
// @Failure  503   {object}  errorResponse
// @Security ServiceKey
// @Security BearerAuth
// @Router   /speak [post]
// @ID       speak
func handleSpeak(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Reader == nil {
			writeError(w, http.StatusServiceUnavailable, "speech is not configured")
			return
		}
		req, ok := decodeSpeak(w, r)
		if !ok {
			return
		}

		ar := req.request()
		if err := d.guardSpeak(r, ar.Scope, ar.ID, ar.Text); err != nil {
			writeAuthError(w, err)
			return
		}
		// audioreader reads the stream flag off the query string; the body is
		// where this API has always carried it. Both are honoured so a caller
		// can ask either way.
		if req.Stream && !audioreader.WantsStream(r) {
			q := r.URL.Query()
			q.Set("stream", "1")
			r = r.Clone(r.Context())
			r.URL.RawQuery = q.Encode()
		}

		d.Reader.Serve(w, r, ar, map[string]any{"scope": ar.Scope, "id": ar.ID})
	}
}

// handlePrime reads the opening of a text before anyone asks for it, so the
// first listen starts on audio that is already made while the rest is read
// behind it.
//
// It answers 202 without waiting: nobody is listening yet, and the caller
// that asked has a reply to finish writing. A failure is logged rather than
// reported, because nothing is broken without it — the first listener waits
// exactly as they did before.
// handlePrime godoc
// @Summary  Read a text's opening ahead of time, so the first listen starts at once
// @Description Answers 202 without waiting: nobody is listening yet. Takes an
// @Description application key — a browser signature is good for /speak alone.
// @Tags     speak
// @Accept   json
// @Param    body  body      speakRequest  true  "text whose opening to read"
// @Success  202
// @Failure  400   {object}  errorResponse
// @Failure  401   {object}  errorResponse
// @Failure  503   {object}  errorResponse
// @Security ServiceKey
// @Security BearerAuth
// @Router   /speak/prime [post]
// @ID       primeSpeak
func handlePrime(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Primer == nil {
			writeError(w, http.StatusServiceUnavailable, "priming needs both speech and a store")
			return
		}
		req, ok := decodeSpeak(w, r)
		if !ok {
			return
		}
		if req.ID == "" {
			writeError(w, http.StatusBadRequest, "id is required to prime: a reading nobody can name again cannot be found later")
			return
		}

		ar := req.request()
		if err := d.guardSpeak(r, ar.Scope, ar.ID, ar.Text); err != nil {
			writeAuthError(w, err)
			return
		}
		go func() {
			// Detached: this outlives the 202 by design, and the request's
			// context is torn down the moment that answer lands.
			ctx, cancel := context.WithTimeout(context.Background(), primeTimeout)
			defer cancel()
			if err := d.Primer.PrimeOpening(ctx, ar.Scope, ar.ID, ar.Text); err != nil {
				log.Printf("vvaves: prime %s/%s: %v", ar.Scope, ar.ID, err)
			}
		}()
		w.WriteHeader(http.StatusAccepted)
	}
}

// handlePregenerate reads a text in full and caches it, so every listen after
// it is served from the store.
//
// For a reading that will be heard more than once — a published page — where
// paying the whole synthesis up front is amortised. A reading heard at most
// once wants /speak/prime instead, which pays for the opening alone.
// handlePregenerate godoc
// @Summary  Read a whole text ahead of time and keep it
// @Description For a text expected to be heard more than once. It holds a
// @Description synthesis slot, so prefer /speak/prime for a text that may never
// @Description be played. Takes an application key.
// @Tags     speak
// @Accept   json
// @Param    body  body      speakRequest  true  "text to read in full"
// @Success  202
// @Failure  400   {object}  errorResponse
// @Failure  401   {object}  errorResponse
// @Failure  503   {object}  errorResponse
// @Security ServiceKey
// @Security BearerAuth
// @Router   /speak/pregenerate [post]
// @ID       pregenerateSpeak
func handlePregenerate(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Reader == nil {
			writeError(w, http.StatusServiceUnavailable, "speech is not configured")
			return
		}
		req, ok := decodeSpeak(w, r)
		if !ok {
			return
		}

		ar := req.request()
		if err := d.guardSpeak(r, ar.Scope, ar.ID, ar.Text); err != nil {
			writeAuthError(w, err)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), primeTimeout)
			defer cancel()
			if err := d.Reader.Pregenerate(ctx, ar); err != nil {
				log.Printf("vvaves: pregenerate %s/%s: %v", ar.Scope, ar.ID, err)
			}
		}()
		w.WriteHeader(http.StatusAccepted)
	}
}

// existsResponse reports whether a reading is already in the store.
type existsResponse struct {
	Ready bool `json:"ready"`
}

// handleExists reports whether a reading is already stored, without reading
// its bytes — for a caller deciding whether to offer a play button, which
// would otherwise download the whole file to answer a yes or no.
// handleExists godoc
// @Summary  Whether a reading is already stored, without reading its bytes
// @Tags     speak
// @Accept   json
// @Produce  json
// @Param    body  body      speakRequest  true  "reading to look for"
// @Success  200   {object}  existsResponse
// @Failure  400   {object}  errorResponse
// @Failure  401   {object}  errorResponse
// @Security ServiceKey
// @Security BearerAuth
// @Router   /speak/exists [post]
// @ID       speakExists
func handleExists(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Reader == nil {
			writeError(w, http.StatusServiceUnavailable, "speech is not configured")
			return
		}
		req, ok := decodeSpeak(w, r)
		if !ok {
			return
		}
		ar := req.request()
		if err := d.guardSpeak(r, ar.Scope, ar.ID, ar.Text); err != nil {
			writeAuthError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, existsResponse{
			Ready: d.Reader.Exists(r.Context(), ar),
		})
	}
}
