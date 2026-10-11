package costs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxBackoff = 10 * time.Minute

type Outbox struct {
	pool *pgxpool.Pool
}

func NewOutbox(pool *pgxpool.Pool) *Outbox {
	return &Outbox{pool: pool}
}

func (o *Outbox) Record(ctx context.Context, l Line) error {
	_, err := o.pool.Exec(ctx, `
		INSERT INTO cost_outbox (code, quantity, occurred_at, idempotency_key, external_user_id, client_name)
		VALUES ($1, $2, $3, $4, $5, COALESCE((SELECT name FROM key_client WHERE key_id = $5), ''))
		ON CONFLICT (idempotency_key) DO NOTHING`,
		l.Code, l.Quantity, l.OccurredAt, l.IdempotencyKey, l.ExternalUserID)
	if err != nil {
		return fmt.Errorf("enqueue cost %s: %w", l.IdempotencyKey, err)
	}
	return nil
}

type pending struct {
	id       int64
	attempts int
	line     Line
}

// claim holds its rows locked until settle commits, so two replicas never
// send the same row; Lungor's idempotency covers a crash between the two.
func (o *Outbox) claim(ctx context.Context, limit int) (pgx.Tx, []pending, error) {
	tx, err := o.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id, attempts, code, quantity, occurred_at, idempotency_key, external_user_id, client_name
		FROM cost_outbox
		WHERE sent_at IS NULL AND parked_at IS NULL AND next_attempt_at <= now()
		ORDER BY id
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pending, error) {
		var p pending
		err := r.Scan(&p.id, &p.attempts, &p.line.Code, &p.line.Quantity, &p.line.OccurredAt, &p.line.IdempotencyKey, &p.line.ExternalUserID, &p.line.ClientName)
		return p, err
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	return tx, out, nil
}

func markSent(ctx context.Context, tx pgx.Tx, ids []int64) error {
	_, err := tx.Exec(ctx, `UPDATE cost_outbox SET sent_at = now() WHERE id = ANY($1)`, ids)
	return err
}

func park(ctx context.Context, tx pgx.Tx, id int64, why string) error {
	_, err := tx.Exec(ctx, `UPDATE cost_outbox SET parked_at = now(), last_error = $2 WHERE id = $1`, id, why)
	return err
}

func retry(ctx context.Context, tx pgx.Tx, p pending, why string) error {
	_, err := tx.Exec(ctx, `
		UPDATE cost_outbox SET attempts = attempts + 1, last_error = $2, next_attempt_at = now() + make_interval(secs => $3)
		WHERE id = $1`, p.id, why, backoff(p.attempts).Seconds())
	return err
}

func backoff(attempts int) time.Duration {
	d := 5 * time.Second << min(attempts, 10)
	return min(d, maxBackoff)
}
