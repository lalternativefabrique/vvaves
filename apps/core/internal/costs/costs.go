// Package costs declares what vvaves spends at its speech providers to
// Lungor, always on vvaves's own app, naming the client that caused it.
// Lungor links a client to the app it belongs to (ADR cost-linked-customers).
package costs

import (
	"context"
	"time"
)

type Line struct {
	Code           string    `json:"code"`
	Quantity       int64     `json:"quantity"`
	OccurredAt     time.Time `json:"occurred_at"`
	IdempotencyKey string    `json:"idempotency_key"`
	ExternalUserID string    `json:"external_user_id,omitempty"`
	ClientName     string    `json:"client_name,omitempty"`
}

type Recorder interface {
	Record(ctx context.Context, l Line) error
}

type clientKey struct{}

func WithClient(ctx context.Context, client string) context.Context {
	return context.WithValue(ctx, clientKey{}, client)
}

func ClientFrom(ctx context.Context) string {
	c, _ := ctx.Value(clientKey{}).(string)
	return c
}
