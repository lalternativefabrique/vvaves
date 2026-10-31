// Package revoke_app is the write-side use case: end an application's access.
package revoke_app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lalternativefabrique/eventkit/pkg/cqrs"

	"github.com/lalternativefabrique/vvaves/core/registry/infrastructure"
)

type Command struct {
	Name string
}

type Result struct{}

type Handler struct {
	repo *infrastructure.Repository
	now  func() time.Time
}

func NewHandler(repo *infrastructure.Repository) *Handler {
	return &Handler{repo: repo, now: time.Now}
}

func (h *Handler) Handle(ctx context.Context, cmd Command) (Result, error) {
	a, err := h.repo.Get(ctx, cmd.Name)
	if errors.Is(err, infrastructure.ErrNotFound) {
		return Result{}, fmt.Errorf("%w: %s", cqrs.ErrNotFound, cmd.Name)
	}
	if err != nil {
		return Result{}, err
	}
	a.Revoke(h.now().UTC())
	return Result{}, h.repo.Save(ctx, a)
}
