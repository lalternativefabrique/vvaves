package registry

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/lalternative/packages/go/eda/pkg/cqrs"

	"github.com/lalternativefabrique/vvaves/core/registry/application/list_apps"
	"github.com/lalternativefabrique/vvaves/core/registry/application/register_app"
	"github.com/lalternativefabrique/vvaves/core/registry/application/revoke_app"
	"github.com/lalternativefabrique/vvaves/core/registry/application/rotate_keys"
	"github.com/lalternativefabrique/vvaves/core/registry/domain"
)

// List godoc
// @Summary  Every registered application, key withheld
// @Tags     admin
// @Produce  json
// @Success  200  {object}  AppListDTO
// @Security BearerAuth
// @Router   /api/v1/admin/apps [get]
// @ID       listApps
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	res, err := cqrs.Ask[list_apps.Query, list_apps.Result](r.Context(), s.queries, list_apps.Query{})
	if err != nil {
		writeError(w, err)
		return
	}
	out := AppListDTO{Apps: make([]AppDTO, 0, len(res.Apps))}
	for _, v := range res.Apps {
		out.Apps = append(out.Apps, AppDTO{
			Name: v.Name, Active: v.Active, Last4: v.Last4, CreatedAt: v.CreatedAt,
			RotatedAt: v.RotatedAt, RevokedAt: v.RevokedAt, GraceUntil: v.GraceUntil,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// Register godoc
// @Summary  Admit an application and hand its key out once
// @Tags     admin
// @Accept   json
// @Produce  json
// @Param    body  body      RegisterRequest  true  "The issuer name the application signs as"
// @Success  201   {object}  CredentialsDTO
// @Failure  400   {object}  ErrorDTO
// @Security BearerAuth
// @Router   /api/v1/admin/apps [post]
// @ID       registerApp
func (s *Service) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorDTO{Error: "invalid body"})
		return
	}
	res, err := cqrs.Execute[register_app.Command, register_app.Result](r.Context(), s.commands, register_app.Command{Name: req.Name})
	if err != nil {
		writeError(w, err)
		return
	}
	s.keys.Invalidate()
	writeJSON(w, http.StatusCreated, credentials(res.App, res.Key))
}

// Rotate godoc
// @Summary  Mint an application a new key; the old one works for a day
// @Tags     admin
// @Produce  json
// @Param    name  path      string  true  "Application name"
// @Success  200   {object}  CredentialsDTO
// @Failure  400   {object}  ErrorDTO
// @Failure  404   {object}  ErrorDTO
// @Security BearerAuth
// @Router   /api/v1/admin/apps/{name}/rotate [post]
// @ID       rotateAppKey
func (s *Service) Rotate(w http.ResponseWriter, r *http.Request) {
	res, err := cqrs.Execute[rotate_keys.Command, rotate_keys.Result](r.Context(), s.commands, rotate_keys.Command{Name: r.PathValue("name")})
	if err != nil {
		writeError(w, err)
		return
	}
	s.keys.Invalidate()
	writeJSON(w, http.StatusOK, credentials(res.App, res.Key))
}

// Revoke godoc
// @Summary  End an application's access at once
// @Tags     admin
// @Param    name  path  string  true  "Application name"
// @Success  204
// @Failure  404   {object}  ErrorDTO
// @Security BearerAuth
// @Router   /api/v1/admin/apps/{name} [delete]
// @ID       revokeApp
func (s *Service) Revoke(w http.ResponseWriter, r *http.Request) {
	if _, err := cqrs.Execute[revoke_app.Command, revoke_app.Result](r.Context(), s.commands, revoke_app.Command{Name: r.PathValue("name")}); err != nil {
		writeError(w, err)
		return
	}
	s.keys.Invalidate()
	w.WriteHeader(http.StatusNoContent)
}

func credentials(a *domain.App, key string) CredentialsDTO {
	return CredentialsDTO{Name: a.Name, Key: key, GraceUntil: a.GraceUntil(time.Now().UTC())}
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cqrs.ErrNotFound):
		writeJSON(w, http.StatusNotFound, ErrorDTO{Error: err.Error()})
	case errors.Is(err, cqrs.ErrValidation):
		writeJSON(w, http.StatusBadRequest, ErrorDTO{Error: err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, ErrorDTO{Error: "registry: " + err.Error()})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
