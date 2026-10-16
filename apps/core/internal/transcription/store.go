package transcription

import (
	"context"
	"sync"
	"time"
)

type Store interface {
	Create(ctx context.Context, j Job) error
	Get(ctx context.Context, id string) (Job, error)
	Pending(ctx context.Context) ([]Job, error)
	Complete(ctx context.Context, id string, r Result, at time.Time) error
	Fail(ctx context.Context, id, reason string, at time.Time) error
}

// MemoryStore keeps jobs for a vvaves with no database: a restart forgets
// them, which main says out loud at boot.
type MemoryStore struct {
	mu   sync.Mutex
	jobs map[string]Job
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{jobs: map[string]Job{}}
}

func (s *MemoryStore) Create(_ context.Context, j Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return j, nil
}

func (s *MemoryStore) Pending(_ context.Context) ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Job
	for _, j := range s.jobs {
		if j.Status == StatusPending {
			out = append(out, j)
		}
	}
	return out, nil
}

func (s *MemoryStore) Complete(_ context.Context, id string, r Result, at time.Time) error {
	return s.finish(id, func(j *Job) {
		j.Status, j.Result, j.FinishedAt = StatusCompleted, &r, &at
	})
}

func (s *MemoryStore) Fail(_ context.Context, id, reason string, at time.Time) error {
	return s.finish(id, func(j *Job) {
		j.Status, j.Error, j.FinishedAt = StatusFailed, reason, &at
	})
}

func (s *MemoryStore) finish(id string, apply func(*Job)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return ErrNotFound
	}
	apply(&j)
	s.jobs[id] = j
	return nil
}
