package transcription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGStore struct {
	pool *pgxpool.Pool
}

func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

func (s *PGStore) Create(ctx context.Context, j Job) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO transcription_job (id, app, reference, url, language, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		j.ID, j.App, j.Reference, j.URL, j.Language, j.Status, j.CreatedAt)
	if err != nil {
		return fmt.Errorf("create transcription %s: %w", j.ID, err)
	}
	return nil
}

const jobColumns = `id, app, reference, url, language, status, error, result, created_at, finished_at`

func (s *PGStore) Get(ctx context.Context, id string) (Job, error) {
	j, err := scanJob(s.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM transcription_job WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get transcription %s: %w", id, err)
	}
	return j, nil
}

func (s *PGStore) Pending(ctx context.Context) ([]Job, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+jobColumns+` FROM transcription_job WHERE status = $1 ORDER BY created_at`, StatusPending)
	if err != nil {
		return nil, fmt.Errorf("list pending transcriptions: %w", err)
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pending transcription: %w", err)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *PGStore) Complete(ctx context.Context, id string, r Result, at time.Time) error {
	body, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode transcription %s: %w", id, err)
	}
	return s.finish(ctx, id, `UPDATE transcription_job SET status = $2, result = $3, finished_at = $4 WHERE id = $1`, StatusCompleted, body, at)
}

func (s *PGStore) Fail(ctx context.Context, id, reason string, at time.Time) error {
	return s.finish(ctx, id, `UPDATE transcription_job SET status = $2, error = $3, finished_at = $4 WHERE id = $1`, StatusFailed, reason, at)
}

func (s *PGStore) finish(ctx context.Context, id, query string, args ...any) error {
	tag, err := s.pool.Exec(ctx, query, append([]any{id}, args...)...)
	if err != nil {
		return fmt.Errorf("finish transcription %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	var result []byte
	if err := row.Scan(&j.ID, &j.App, &j.Reference, &j.URL, &j.Language, &j.Status, &j.Error, &result, &j.CreatedAt, &j.FinishedAt); err != nil {
		return Job{}, err
	}
	if result != nil {
		j.Result = &Result{}
		if err := json.Unmarshal(result, j.Result); err != nil {
			return Job{}, fmt.Errorf("decode result: %w", err)
		}
	}
	return j, nil
}
