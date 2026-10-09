package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/lalternative/packages/vvaves/sdk-go/signed"
)

// SigningIssuer names vvaves itself as the signer of a URL: a customer key is
// verified but never stored, so no signature can be checked against it, and
// vvaves signs on the application's behalf instead.
const SigningIssuer = "vvaves"

// SigningTTL matches the lifetime of a URL an application signs itself.
const SigningTTL = 30 * time.Minute

// WithSigningIssuer extends a registry lookup with vvaves's own secret.
func WithSigningIssuer(lookup func(issuer string) []string, secret string) func(issuer string) []string {
	return func(issuer string) []string {
		if issuer == SigningIssuer {
			if secret == "" {
				return nil
			}
			return []string{secret}
		}
		if lookup == nil {
			return nil
		}
		return lookup(issuer)
	}
}

type signResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// handleSign godoc
// @Summary  Sign a /speak URL a browser can play
// @Description For an application holding a customer key, which cannot sign
// @Description URLs itself. Takes a server credential, never a signature.
// @Tags     speak
// @Accept   json
// @Produce  json
// @Param    body  body      speakRequest  true  "reading to authorise"
// @Success  200   {object}  signResponse
// @Failure  400   {object}  errorResponse
// @Failure  401   {object}  errorResponse
// @Failure  503   {object}  errorResponse
// @Security BearerAuth
// @Router   /speak/sign [post]
// @ID       signSpeak
func handleSign(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeSpeak(w, r)
		if !ok {
			return
		}
		ar := req.request()
		if err := d.guardSpeak(r, ar.Scope, ar.ID, ar.Text); err != nil {
			writeAuthError(w, err)
			return
		}
		if d.SigningSecret == "" {
			writeError(w, http.StatusServiceUnavailable, "this vvaves has no signing secret")
			return
		}
		expires := time.Now().Add(SigningTTL)
		q, err := signed.Sign(SigningIssuer, d.SigningSecret, signed.Params{
			Scope: ar.Scope, ID: ar.ID, TextHash: signed.HashText(ar.Text), Expires: expires,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, signResponse{
			URL:       publicBase(r, d.PublicURL) + speakPath + "?" + q.Encode(),
			ExpiresAt: time.Unix(expires.Unix(), 0).UTC(),
		})
	}
}

func publicBase(r *http.Request, configured string) string {
	if base := strings.TrimRight(strings.TrimSpace(configured), "/"); base != "" {
		return base
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
		if r.TLS != nil {
			proto = "https"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return proto + "://" + host
}
