package httpapi

import (
	"context"
	"net/http"
	"time"
)

type renderRequest struct {
	URL       string `json:"url"`
	TimeoutMS int64  `json:"timeout_ms"`
}

type renderResponse struct {
	HTML     string `json:"html"`
	FinalURL string `json:"final_url"`
}

const (
	defaultRenderTimeout = 8 * time.Second
	minRenderTimeout     = time.Second
)

// FinalURLRenderer is a Renderer that also reports the URL actually loaded,
// which a redirect can make differ from the one asked for. fetch.Renderer
// only needs the HTML, so /render asks for more than the library does.
type FinalURLRenderer interface {
	RenderPage(ctx context.Context, url string, timeout time.Duration) (html, finalURL string, err error)
}

// handleRender godoc
// @Summary  The page's HTML after its JavaScript has run
// @Tags     web
// @Accept   json
// @Produce  json
// @Param    body  body      renderRequest  true  "page to render"
// @Success  200   {object}  renderResponse
// @Failure  400   {object}  errorResponse
// @Failure  502   {object}  errorResponse
// @Failure  503   {object}  errorResponse
// @Security ServiceKey
// @Security BearerAuth
// @Router   /render [post]
// @ID       renderPage
func handleRender(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		full, ok := d.Renderer.(FinalURLRenderer)
		if d.Renderer == nil || !ok {
			writeError(w, http.StatusServiceUnavailable, "rendering is not configured")
			return
		}
		if err := d.guardService(r); err != nil {
			writeAuthError(w, err)
			return
		}

		var req renderRequest
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if msg := validateURL(req.URL, d.AllowPrivateFetch); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}

		html, finalURL, err := full.RenderPage(r.Context(), req.URL, renderTimeout(d, req.TimeoutMS))
		if err != nil {
			// A page that never goes network-idle, refuses the connection or
			// 4xx/5xxs is routine on the open web — the caller treats a render
			// error as "fall back to the static result", not as fatal.
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, renderResponse{HTML: html, FinalURL: finalURL})
	}
}

// renderTimeout clamps what a caller asks for. An unbounded render is a
// request that never finishes, which on a shared browser starves every other
// render queued behind it.
func renderTimeout(d Deps, requestedMS int64) time.Duration {
	asked := defaultRenderTimeout
	if requestedMS > 0 {
		asked = time.Duration(requestedMS) * time.Millisecond
	}
	if asked > d.RenderMaxTimeout {
		return d.RenderMaxTimeout
	}
	if asked < minRenderTimeout {
		return minRenderTimeout
	}
	return asked
}
