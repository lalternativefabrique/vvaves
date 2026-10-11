package costs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Clients keeps the name of every application key and registers it with
// Lungor as a client of vvaves, so the admin can link it to its app before it
// costs anything. A key is never removed: a revoked one keeps its history.
type Clients struct {
	pool *pgxpool.Pool
}

func NewClients(pool *pgxpool.Pool) *Clients {
	return &Clients{pool: pool}
}

func (c *Clients) Known(ctx context.Context, keyID, name string) error {
	if keyID == "" || strings.TrimSpace(name) == "" {
		return nil
	}
	_, err := c.pool.Exec(ctx, `
		INSERT INTO key_client (key_id, name) VALUES ($1, $2)
		ON CONFLICT (key_id) DO UPDATE SET name = EXCLUDED.name, changed_at = now()
		WHERE key_client.name <> EXCLUDED.name`, keyID, strings.TrimSpace(name))
	if err != nil {
		return fmt.Errorf("record key %s: %w", keyID, err)
	}
	return nil
}

func (c *Clients) Revoked(ctx context.Context, keyID string) error {
	_, err := c.pool.Exec(ctx, `
		UPDATE key_client SET revoked_at = now(), changed_at = now()
		WHERE key_id = $1 AND revoked_at IS NULL`, keyID)
	if err != nil {
		return fmt.Errorf("revoke key %s: %w", keyID, err)
	}
	return nil
}

type clientRow struct {
	keyID     string
	name      string
	revokedAt *time.Time
	changedAt time.Time
}

func (c *Clients) unsynced(ctx context.Context, limit int) ([]clientRow, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT key_id, name, revoked_at, changed_at FROM key_client
		WHERE synced_at IS NULL OR synced_at < changed_at
		ORDER BY changed_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (clientRow, error) {
		var cr clientRow
		err := r.Scan(&cr.keyID, &cr.name, &cr.revokedAt, &cr.changedAt)
		return cr, err
	})
}

// synced records the version that was sent, so a change made while it was in
// flight is sent again.
func (c *Clients) synced(ctx context.Context, cr clientRow) error {
	_, err := c.pool.Exec(ctx, `UPDATE key_client SET synced_at = $2 WHERE key_id = $1`, cr.keyID, cr.changedAt)
	return err
}

type clientBody struct {
	Name      string     `json:"name"`
	RevokedAt *time.Time `json:"revoked_at"`
}

func (l Lungor) PutClient(ctx context.Context, externalUserID, name string, revokedAt *time.Time) error {
	body, err := json.Marshal(clientBody{Name: name, RevokedAt: revokedAt})
	if err != nil {
		return fmt.Errorf("%w: encode: %v", errRejected, err)
	}
	u := strings.TrimRight(l.URL, "/") + "/api/v1/metering/clients/" + url.PathEscape(externalUserID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+l.AppKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := l.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	// An upsert has nothing to be missing: a 404 is a Lungor without the route
	// yet, and the registration waits for it.
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusMethodNotAllowed {
		return fmt.Errorf("lungor has no client registry yet: %d", res.StatusCode)
	}
	return classify(res.StatusCode, strings.TrimSpace(string(msg)))
}

// syncClients marks a refused registration synced: sending it again changes
// nothing, and the next change to the key sends it anew.
func (s Sender) syncClients(ctx context.Context) {
	if s.Clients == nil {
		return
	}
	rows, err := s.Clients.unsynced(ctx, senderBatch)
	if err != nil {
		s.Log.Warn("costs: list unsynced clients", "error", err)
		return
	}
	for _, cr := range rows {
		err := s.Lungor.PutClient(ctx, cr.keyID, cr.name, cr.revokedAt)
		switch {
		case err == nil:
		case errors.Is(err, errRejected):
			s.Log.Error("costs: client registration refused", "key_id", cr.keyID, "error", err)
		default:
			s.Log.Warn("costs: client registration failed, will retry", "key_id", cr.keyID, "error", err)
			return
		}
		if err := s.Clients.synced(ctx, cr); err != nil {
			s.Log.Warn("costs: mark client synced", "key_id", cr.keyID, "error", err)
		}
	}
}
