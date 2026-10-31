// Package registry is the bounded context of the applications allowed to
// speak through vvaves: who they are, the key they sign and call with, and
// the admin API that admits, rotates and revokes them.
package registry

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lalternativefabrique/eventkit/pkg/cqrs"
	"github.com/lalternativefabrique/eventkit/pkg/di"
	"github.com/lalternativefabrique/eventkit/pkg/logger"
	"github.com/lalternativefabrique/eventkit/pkg/obs"

	"github.com/lalternativefabrique/vvaves/core/registry/application/list_apps"
	"github.com/lalternativefabrique/vvaves/core/registry/application/register_app"
	"github.com/lalternativefabrique/vvaves/core/registry/application/revoke_app"
	"github.com/lalternativefabrique/vvaves/core/registry/application/rotate_keys"
	"github.com/lalternativefabrique/vvaves/core/registry/infrastructure"
)

// Service is the context's HTTP-facing facade.
type Service struct {
	commands *cqrs.CommandRouter
	queries  *cqrs.QueryRouter
	keys     *KeySource
}

// NewService wires the context on pool. cipher seals the keys at rest; env
// holds the "issuer:key" pairs read from the environment, still honoured
// under the registry's own entries.
func NewService(pool *pgxpool.Pool, cipher *infrastructure.Cipher, env map[string][]string) (*Service, error) {
	reg := di.New()

	di.Provide[logger.Logger](reg, func(_ *di.Resolver) (logger.Logger, error) {
		return logger.NewJSONSlogLogger(slog.LevelInfo), nil
	})
	di.Provide(reg, func(_ *di.Resolver) (*infrastructure.Repository, error) {
		return infrastructure.NewRepository(pool), nil
	})
	di.Provide(reg, func(rv *di.Resolver) (*cqrs.CommandRouter, error) {
		repo := di.MustFrom[*infrastructure.Repository](rv)
		log := di.MustFrom[logger.Logger](rv)
		router := cqrs.NewCommandRouter()
		registerCommand(router, log, register_app.NewHandler(repo, cipher).Handle)
		registerCommand(router, log, rotate_keys.NewHandler(repo, cipher).Handle)
		registerCommand(router, log, revoke_app.NewHandler(repo).Handle)
		return router, nil
	})
	di.Provide(reg, func(rv *di.Resolver) (*cqrs.QueryRouter, error) {
		repo := di.MustFrom[*infrastructure.Repository](rv)
		router := cqrs.NewQueryRouter()
		cqrs.RegisterQueryHandler[list_apps.Query, list_apps.Result](router, list_apps.NewHandler(repo))
		return router, nil
	})

	repo := di.MustResolve[*infrastructure.Repository](reg)
	return &Service{
		commands: di.MustResolve[*cqrs.CommandRouter](reg),
		queries:  di.MustResolve[*cqrs.QueryRouter](reg),
		keys:     NewKeySource(repo, cipher, env),
	}, nil
}

func registerCommand[C any, R any](router *cqrs.CommandRouter, log logger.Logger, handle cqrs.TypedCommandHandlerFunc[C, R]) {
	mw := cqrs.Chain(
		cqrs.RecoveryMiddleware[C, R](),
		obs.LoggingMiddleware[C, R](log),
	)
	cqrs.RegisterCommandHandler[C, R](router, cqrs.TypedCommandHandlerFunc[C, R](mw(handle)))
}

// Keys is what the speak guard verifies against.
func (s *Service) Keys() *KeySource { return s.keys }

// RegisterRoutes mounts the admin API under prefix, every route behind guard.
func (s *Service) RegisterRoutes(mux *http.ServeMux, prefix string, guard func(http.Handler) http.Handler) {
	mux.Handle("GET "+prefix+"/admin/apps", guard(http.HandlerFunc(s.List)))
	mux.Handle("POST "+prefix+"/admin/apps", guard(http.HandlerFunc(s.Register)))
	mux.Handle("POST "+prefix+"/admin/apps/{name}/rotate", guard(http.HandlerFunc(s.Rotate)))
	mux.Handle("DELETE "+prefix+"/admin/apps/{name}", guard(http.HandlerFunc(s.Revoke)))
}
