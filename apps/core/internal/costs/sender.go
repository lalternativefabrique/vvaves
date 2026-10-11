package costs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	senderBatch    = 100
	senderInterval = 2 * time.Second
)

var errRejected = errors.New("lungor rejected the cost lines")

// Lungor posts lines to POST /api/v1/metering/costs with vvaves's own app key,
// never naming an app_id: every cost is vvaves's.
type Lungor struct {
	URL    string
	AppKey string
	HTTP   *http.Client
}

func (l Lungor) Post(ctx context.Context, lines []Line) error {
	body, err := json.Marshal(map[string]any{"lines": lines})
	if err != nil {
		return fmt.Errorf("%w: encode: %v", errRejected, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(l.URL, "/")+"/api/v1/metering/costs", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+l.AppKey)
	req.Header.Set("Content-Type", "application/json")
	httpc := l.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 15 * time.Second}
	}
	res, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	return classify(res.StatusCode, strings.TrimSpace(string(msg)))
}

// classify parks what Lungor refuses for the lines themselves (an unknown or
// unpriced code) and retries the rest: 401 is vvaves's own key, 429 a limit.
func classify(status int, body string) error {
	switch {
	case status < 300:
		return nil
	case status == http.StatusBadRequest, status == http.StatusForbidden, status == http.StatusNotFound,
		status == http.StatusConflict, status == http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: %d %s", errRejected, status, body)
	default:
		return fmt.Errorf("lungor answered %d: %s", status, body)
	}
}

type Sender struct {
	Outbox *Outbox
	Lungor Lungor
	Log    *slog.Logger
}

func (s Sender) Run(ctx context.Context) {
	t := time.NewTicker(senderInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for {
				n, err := s.runOnce(ctx)
				if err != nil {
					s.Log.Warn("costs: claim failed", "error", err)
				}
				if err != nil || n < senderBatch {
					break
				}
			}
		}
	}
}

func (s Sender) runOnce(ctx context.Context) (int, error) {
	tx, rows, err := s.Outbox.claim(ctx, senderBatch)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if len(rows) == 0 {
		return 0, nil
	}

	err = s.Lungor.Post(ctx, linesOf(rows))
	// One bad line refuses the whole batch: sent alone, the others go through.
	if errors.Is(err, errRejected) && len(rows) > 1 {
		for _, p := range rows {
			if sErr := s.settle(ctx, tx, []pending{p}, s.Lungor.Post(ctx, []Line{p.line})); sErr != nil {
				return len(rows), sErr
			}
		}
	} else if sErr := s.settle(ctx, tx, rows, err); sErr != nil {
		return len(rows), sErr
	}
	return len(rows), tx.Commit(ctx)
}

func (s Sender) settle(ctx context.Context, tx pgx.Tx, rows []pending, err error) error {
	switch {
	case err == nil:
		ids := make([]int64, len(rows))
		for i, p := range rows {
			ids[i] = p.id
		}
		return markSent(ctx, tx, ids)
	case errors.Is(err, errRejected):
		for _, p := range rows {
			s.Log.Error("costs: line parked", "idempotency_key", p.line.IdempotencyKey, "code", p.line.Code, "error", err)
			if pErr := park(ctx, tx, p.id, err.Error()); pErr != nil {
				return pErr
			}
		}
		return nil
	default:
		s.Log.Warn("costs: declaration failed, will retry", "rows", len(rows), "error", err)
		for _, p := range rows {
			if rErr := retry(ctx, tx, p, err.Error()); rErr != nil {
				return rErr
			}
		}
		return nil
	}
}

func linesOf(rows []pending) []Line {
	out := make([]Line, len(rows))
	for i, p := range rows {
		out[i] = p.line
	}
	return out
}
