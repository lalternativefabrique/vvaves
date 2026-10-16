package transcription

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lalternative/packages/go/busevents"

	"github.com/lalternativefabrique/vvaves/core/internal/costs"
	"github.com/lalternativefabrique/vvaves/core/internal/transcribe"
)

type Engine interface {
	Transcribe(ctx context.Context, src transcribe.Source, opts transcribe.Options) (*transcribe.Result, error)
}

type Publisher interface {
	Publish(ctx context.Context, subject string, body []byte, msgID string) error
}

type Config struct {
	Concurrency   int
	Timeout       time.Duration
	MaxDuration   time.Duration
	MaxBytes      int64
	ChunkParallel int
	QueueSize     int
	// CostCode is the Lungor item transcribed seconds are declared on; empty
	// declares nothing.
	CostCode string
}

func (c Config) withDefaults() Config {
	if c.Concurrency <= 0 {
		c.Concurrency = 2
	}
	if c.Timeout <= 0 {
		c.Timeout = time.Hour
	}
	if c.MaxDuration <= 0 {
		c.MaxDuration = 120 * time.Minute
	}
	if c.MaxBytes <= 0 {
		c.MaxBytes = 1 << 30
	}
	if c.ChunkParallel <= 0 {
		c.ChunkParallel = 2
	}
	if c.QueueSize <= 0 {
		c.QueueSize = 256
	}
	return c
}

type Service struct {
	store     Store
	engine    Engine
	publisher Publisher
	recorder  costs.Recorder
	cfg       Config
	http      *http.Client
	queue     chan string
	log       *slog.Logger
	now       func() time.Time
}

// New wires the service; a nil publisher announces nothing and a nil recorder
// declares no cost, and both leave the jobs readable by id.
func New(store Store, engine Engine, publisher Publisher, recorder costs.Recorder, cfg Config, log *slog.Logger) *Service {
	cfg = cfg.withDefaults()
	return &Service{
		store:     store,
		engine:    engine,
		publisher: publisher,
		recorder:  recorder,
		cfg:       cfg,
		http:      &http.Client{},
		queue:     make(chan string, cfg.QueueSize),
		log:       log,
		now:       time.Now,
	}
}

func (s *Service) Submit(ctx context.Context, r Request) (Job, error) {
	if err := r.Validate(); err != nil {
		return Job{}, err
	}
	if len(s.queue) >= cap(s.queue) {
		return Job{}, ErrBusy
	}
	j := Job{
		ID:        uuid.NewString(),
		App:       r.App,
		Reference: r.Reference,
		URL:       r.URL,
		Language:  r.Language,
		Status:    StatusPending,
		CreatedAt: s.now().UTC(),
	}
	if err := s.store.Create(ctx, j); err != nil {
		return Job{}, err
	}
	select {
	case s.queue <- j.ID:
	default:
		s.log.Warn("transcription queue full, the job waits for the next restart", "id", j.ID)
	}
	return j, nil
}

func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Job{}, ErrNotFound
	}
	return s.store.Get(ctx, id)
}

// Run resumes the jobs a previous process left pending, then works the queue
// until ctx ends. A job interrupted by the shutdown stays pending.
func (s *Service) Run(ctx context.Context) {
	pending, err := s.store.Pending(ctx)
	if err != nil {
		s.log.Error("pending transcriptions not resumed", "error", err)
	}
	go func() {
		for _, j := range pending {
			select {
			case s.queue <- j.ID:
			case <-ctx.Done():
				return
			}
		}
	}()
	done := make(chan struct{})
	for range s.cfg.Concurrency {
		go func() {
			defer func() { done <- struct{}{} }()
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-s.queue:
					s.process(ctx, id)
				}
			}
		}()
	}
	for range s.cfg.Concurrency {
		<-done
	}
}

func (s *Service) process(ctx context.Context, id string) {
	j, err := s.store.Get(ctx, id)
	if err != nil {
		s.log.Error("transcription not loaded", "id", id, "error", err)
		return
	}
	if j.Status != StatusPending {
		return
	}
	jobCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	res, err := s.engine.Transcribe(jobCtx, transcribe.ObjectSource{
		Storage:  urlFetcher{client: s.http, url: j.URL},
		Key:      j.ID,
		Ext:      extensionOf(j.URL),
		MaxBytes: s.cfg.MaxBytes,
	}, transcribe.Options{
		Language:                 j.Language,
		MaxParallel:              s.cfg.ChunkParallel,
		MaxDurationSeconds:       s.cfg.MaxDuration.Seconds(),
		MaxBytesWhenUnmeasurable: s.cfg.MaxBytes,
		RequireMeasurableAudio:   true,
	})
	if ctx.Err() != nil {
		return
	}
	finished := s.now().UTC()
	if err != nil {
		s.fail(ctx, j, err, finished)
		return
	}
	result := toResult(res, j.Language)
	if err := s.store.Complete(ctx, j.ID, result, finished); err != nil {
		s.log.Error("transcription result not stored", "id", j.ID, "error", err)
		return
	}
	s.meter(ctx, j, result, finished)
	s.publish(ctx, SubjectCompleted, Completed{
		Meta:            busevents.Meta{EventID: j.ID + ".completed", OccurredAt: finished},
		TranscriptionID: j.ID,
		App:             j.App,
		Reference:       j.Reference,
		Language:        result.Language,
		DurationSeconds: result.DurationSeconds,
		Text:            result.Text,
		Segments:        result.Segments,
	})
}

func (s *Service) fail(ctx context.Context, j Job, cause error, at time.Time) {
	reason := publicReason(cause)
	s.log.Warn("transcription failed", "id", j.ID, "app", j.App, "error", cause)
	if err := s.store.Fail(ctx, j.ID, reason, at); err != nil {
		s.log.Error("transcription failure not stored", "id", j.ID, "error", err)
		return
	}
	s.publish(ctx, SubjectFailed, Failed{
		Meta:            busevents.Meta{EventID: j.ID + ".failed", OccurredAt: at},
		TranscriptionID: j.ID,
		App:             j.App,
		Reference:       j.Reference,
		Error:           reason,
	})
}

// publicReason is what the caller is told: the underlying error may quote
// ffmpeg's stderr or a provider's body.
func publicReason(err error) string {
	switch {
	case errors.Is(err, transcribe.ErrTooLong):
		return "audio exceeds the maximum duration"
	case errors.Is(err, transcribe.ErrUnmeasurable), errors.Is(err, transcribe.ErrNoAudioStream), errors.Is(err, transcribe.ErrUnboundedDuration):
		return "audio could not be read"
	case errors.Is(err, context.DeadlineExceeded):
		return "transcription timed out"
	case errors.Is(err, errFetch):
		return "audio could not be fetched"
	default:
		return "transcription failed"
	}
}

func (s *Service) meter(ctx context.Context, j Job, r Result, at time.Time) {
	if s.recorder == nil || s.cfg.CostCode == "" || r.DurationSeconds <= 0 {
		return
	}
	line := costs.Line{
		Code:           s.cfg.CostCode,
		Quantity:       int64(math.Ceil(r.DurationSeconds)),
		OccurredAt:     at,
		IdempotencyKey: j.ID + ".seconds",
		ExternalUserID: j.App,
	}
	if err := s.recorder.Record(ctx, line); err != nil {
		s.log.Error("transcription cost not recorded", "id", j.ID, "seconds", line.Quantity, "error", err)
	}
}

const publishAttempts = 3

func (s *Service) publish(ctx context.Context, subject string, e busevents.Event) {
	if s.publisher == nil {
		return
	}
	body, msgID, err := busevents.Encode(e)
	if err != nil {
		s.log.Error("transcription event not encoded", "subject", subject, "error", err)
		return
	}
	for attempt := 1; ; attempt++ {
		err = s.publisher.Publish(ctx, subject, body, msgID)
		if err == nil {
			return
		}
		if attempt == publishAttempts || ctx.Err() != nil {
			break
		}
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	s.log.Error("transcription event not published", "subject", subject, "event_id", msgID, "error", err)
}

func toResult(r *transcribe.Result, hint string) Result {
	out := Result{Language: r.Language, DurationSeconds: r.DurationSeconds, Segments: []Segment{}}
	if out.Language == "" {
		out.Language = hint
	}
	texts := make([]string, 0, len(r.Segments))
	for _, seg := range r.Segments {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		out.Segments = append(out.Segments, Segment{StartSeconds: seg.Start, EndSeconds: seg.End, Text: text})
		texts = append(texts, text)
	}
	out.Text = strings.Join(texts, " ")
	return out
}
