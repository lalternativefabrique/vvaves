package httpapi

import "net/http"

// /speak is the one route a browser calls, on another origin than the page
// that handed it the URL. What authorises the call is the signature in that
// URL, not a cookie, so the origin carries nothing worth checking: any origin
// may ask, without credentials, and a curl could replay the URL anyway.
// Every other route answers no preflight, which keeps a browser off them the
// way guardSpeak keeps a signature off them.
const (
	corsAllowMethods  = "POST"
	corsAllowHeaders  = "Content-Type, Range"
	corsExposeHeaders = "Accept-Ranges, Content-Length, Content-Range, Content-Type, X-Tts-Cache, X-Tts-Voice"
	corsMaxAge        = "86400"
)

func handleSpeakPreflight() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", corsAllowMethods)
		h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
		h.Set("Access-Control-Max-Age", corsMaxAge)
		w.WriteHeader(http.StatusNoContent)
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
		next.ServeHTTP(w, r)
	})
}
