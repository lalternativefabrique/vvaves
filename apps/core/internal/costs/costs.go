// Package costs declares what vvaves spends at its speech providers to
// Lungor, always on vvaves's own app, naming the client that caused it.
// Lungor links a client to the app it belongs to (ADR cost-linked-customers).
package costs

import (
	"context"
	"strings"
	"time"
)

type Line struct {
	Code           string    `json:"code"`
	Quantity       int64     `json:"quantity"`
	OccurredAt     time.Time `json:"occurred_at"`
	IdempotencyKey string    `json:"idempotency_key"`
	ExternalUserID string    `json:"external_user_id,omitempty"`
	EndUserID      string    `json:"end_user_id,omitempty"`
	ClientName     string    `json:"client_name,omitempty"`
}

type Recorder interface {
	Record(ctx context.Context, l Line) error
}

// Payer is who a cost is declared for: the client that called vvaves, and
// that client's own user when it named one.
type Payer struct {
	Client  string
	EndUser string
}

// payerSep cannot occur in either half: a client is a key id or an issuer,
// and EndUser is refused at the door when it holds a control character.
const payerSep = "\x1f"

// String packs p into audioreader's single billTo string, which carries it
// from the handler to the provider call untouched.
func (p Payer) String() string {
	if p.EndUser == "" {
		return p.Client
	}
	return p.Client + payerSep + p.EndUser
}

func ParsePayer(s string) Payer {
	client, user, _ := strings.Cut(s, payerSep)
	return Payer{Client: client, EndUser: user}
}

type clientKey struct{}

func WithClient(ctx context.Context, client string) context.Context {
	return context.WithValue(ctx, clientKey{}, client)
}

func ClientFrom(ctx context.Context) string {
	c, _ := ctx.Value(clientKey{}).(string)
	return c
}
