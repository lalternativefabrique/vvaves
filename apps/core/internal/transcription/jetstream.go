package transcription

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
)

// JetStream publishes into the suite's EVENTS stream, so durable consumers
// receive the event even when they are down at the time.
type JetStream struct {
	js nats.JetStreamContext
}

func NewJetStream(nc *nats.Conn) (*JetStream, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	return &JetStream{js: js}, nil
}

func (p *JetStream) Publish(ctx context.Context, subject string, body []byte, msgID string) error {
	if _, err := p.js.Publish(subject, body, nats.MsgId(msgID), nats.Context(ctx)); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}
