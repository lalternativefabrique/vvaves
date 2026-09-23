// Package keys exposes the customer key page's calls on the core, so they
// enter the contract like every other route. The work is go/appkeys' relay;
// this is where it is named, documented and mounted.
package keys

import (
	"net/http"
	"strings"
)

// KeyDTO is one key a customer holds for this product. The secret is never
// listed: it exists in the answer to its creation and nowhere else.
type KeyDTO struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Audience  []string `json:"audience"`
	Scopes    []string `json:"scopes"`
	CreatedAt *string  `json:"createdAt"`
	ExpiresAt *string  `json:"expiresAt"`
}

// KeyListDTO is the caller's keys.
type KeyListDTO struct {
	Keys []KeyDTO `json:"keys"`
}

// CreateKeyRequest names the key. Scopes default to what the product's
// contract declares.
type CreateKeyRequest struct {
	Label  string   `json:"label"`
	Scopes []string `json:"scopes,omitempty"`
}

// CreatedKeyDTO is the only place the key's secret ever appears.
type CreatedKeyDTO struct {
	KeyDTO
	Secret string `json:"secret"`
}

// ErrorDTO names what went wrong: sign_in_required, label, scope,
// no_credential, unavailable, or one of urbangate's own repairs.
type ErrorDTO struct {
	Error string `json:"error"`
}

// Service mounts the relay under the web app's authentication.
type Service struct {
	relay http.Handler
}

func New(relay http.Handler) *Service {
	return &Service{relay: relay}
}

// RegisterRoutes mounts the three calls at prefix, each behind guard.
func (s *Service) RegisterRoutes(mux *http.ServeMux, prefix string, guard func(http.Handler) http.Handler) {
	prefix = strings.TrimRight(prefix, "/")
	strip := func(h http.HandlerFunc) http.Handler { return guard(http.StripPrefix(prefix, h)) }
	mux.Handle("GET "+prefix, strip(s.ListKeys))
	mux.Handle("POST "+prefix, strip(s.CreateKey))
	mux.Handle("DELETE "+prefix+"/{id}", strip(s.RevokeKey))
}

// ListKeys godoc
// @Summary  List the caller's keys for this product
// @Tags     keys
// @Produce  json
// @Success  200  {object}  KeyListDTO
// @Failure  401  {object}  ErrorDTO
// @Failure  503  {object}  ErrorDTO
// @Security BearerAuth
// @Router   /api/keys [get]
// @ID       listKeys
func (s *Service) ListKeys(w http.ResponseWriter, r *http.Request) { s.relay.ServeHTTP(w, r) }

// CreateKey godoc
// @Summary  Issue a key the caller's application will present to this product
// @Description The key is signed by urbangate and returned once; it is stored nowhere.
// @Tags     keys
// @Accept   json
// @Produce  json
// @Param    body  body      CreateKeyRequest  true  "Label, and optionally the scopes"
// @Success  200   {object}  CreatedKeyDTO
// @Failure  401   {object}  ErrorDTO
// @Failure  422   {object}  ErrorDTO
// @Failure  503   {object}  ErrorDTO
// @Security BearerAuth
// @Router   /api/keys [post]
// @ID       createKey
func (s *Service) CreateKey(w http.ResponseWriter, r *http.Request) { s.relay.ServeHTTP(w, r) }

// RevokeKey godoc
// @Summary  Revoke one of the caller's keys
// @Description Recorded before the key is dropped: a 503 left the key working, and retrying is safe.
// @Tags     keys
// @Produce  json
// @Param    id  path  string  true  "The key's id"
// @Success  204
// @Failure  401  {object}  ErrorDTO
// @Failure  403  {object}  ErrorDTO
// @Failure  503  {object}  ErrorDTO
// @Security BearerAuth
// @Router   /api/keys/{id} [delete]
// @ID       revokeKey
func (s *Service) RevokeKey(w http.ResponseWriter, r *http.Request) { s.relay.ServeHTTP(w, r) }
