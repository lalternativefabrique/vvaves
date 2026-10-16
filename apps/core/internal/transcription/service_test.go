package transcription

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/lalternativefabrique/vvaves/core/internal/costs"
	"github.com/lalternativefabrique/vvaves/core/internal/transcribe"
)

type stubEngine struct {
	err     error
	fetched chan string
}

func (e stubEngine) Transcribe(ctx context.Context, src transcribe.Source, opts transcribe.Options) (*transcribe.Result, error) {
	path, cleanup, err := src.Localize(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	body, _ := os.ReadFile(path)
	e.fetched <- string(body)
	if e.err != nil {
		return nil, e.err
	}
	return &transcribe.Result{
		DurationSeconds: 61.2,
		Language:        "fr",
		Segments: []transcribe.Segment{
			{Text: " bonjour ", Start: 0, End: 1.5, HasTiming: true},
			{Text: "le monde", Start: 1.5, End: 3, HasTiming: true},
		},
	}, nil
}

type published struct {
	subject, msgID string
	body           []byte
}

type stubPublisher struct {
	msgs chan published
}

func (p *stubPublisher) Publish(_ context.Context, subject string, body []byte, msgID string) error {
	p.msgs <- published{subject: subject, body: body, msgID: msgID}
	return nil
}

type stubRecorder struct{ lines chan costs.Line }

func (r stubRecorder) Record(_ context.Context, l costs.Line) error {
	r.lines <- l
	return nil
}

func audioServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "RIFF")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newService(engine Engine, pub Publisher, rec costs.Recorder, store Store) *Service {
	return New(store, engine, pub, rec, Config{CostCode: "stt/whisper-large-v3.seconds"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func await[T any](t *testing.T, ch chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		var zero T
		return zero
	}
}

func TestSubmitValidates(t *testing.T) {
	s := newService(stubEngine{}, nil, nil, NewMemoryStore())
	for name, r := range map[string]Request{
		"no reference":   {URL: "https://a.example/x.mp3"},
		"long reference": {URL: "https://a.example/x.mp3", Reference: string(make([]byte, 201))},
		"plain http":     {URL: "http://a.example/x.mp3", Reference: "r"},
		"file":           {URL: "file:///etc/passwd", Reference: "r"},
		"relative":       {URL: "/x.mp3", Reference: "r"},
	} {
		if _, err := s.Submit(context.Background(), r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestCompletedJobIsStoredMeteredAndAnnounced(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine := stubEngine{fetched: make(chan string, 1)}
	pub := &stubPublisher{msgs: make(chan published, 1)}
	rec := stubRecorder{lines: make(chan costs.Line, 1)}
	store := NewMemoryStore()
	s := newService(engine, pub, rec, store)
	go s.Run(ctx)

	j, err := s.Submit(ctx, Request{App: "app-1", URL: audioServer(t).URL + "/a.mp3", Reference: "ep-42"})
	if err != nil {
		t.Fatal(err)
	}
	if got := await(t, engine.fetched); got != "RIFF" {
		t.Fatalf("fetched %q", got)
	}
	msg := await(t, pub.msgs)
	if msg.subject != "events.vvaves.transcription.completed" || msg.msgID != j.ID+".completed" {
		t.Fatalf("published %s %s", msg.subject, msg.msgID)
	}
	var e map[string]any
	if err := json.Unmarshal(msg.body, &e); err != nil {
		t.Fatal(err)
	}
	if e["id"] != j.ID || e["app"] != "app-1" || e["reference"] != "ep-42" || e["language"] != "fr" ||
		e["duration_seconds"] != 61.2 || e["text"] != "bonjour le monde" || e["event_id"] == nil {
		t.Fatalf("event = %v", e)
	}
	segs := e["segments"].([]any)
	if len(segs) != 2 || segs[1].(map[string]any)["start_seconds"] != 1.5 {
		t.Fatalf("segments = %v", segs)
	}
	line := await(t, rec.lines)
	if line.Quantity != 62 || line.ExternalUserID != "app-1" || line.IdempotencyKey != j.ID+".seconds" {
		t.Fatalf("cost = %+v", line)
	}
	stored, _ := s.Get(ctx, j.ID)
	if stored.Status != StatusCompleted || stored.Result.Text != "bonjour le monde" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestFailedJobIsAnnouncedWithAPublicReason(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine := stubEngine{fetched: make(chan string, 1), err: errors.New("ffmpeg stderr with secrets")}
	pub := &stubPublisher{msgs: make(chan published, 1)}
	s := newService(engine, pub, nil, NewMemoryStore())
	go s.Run(ctx)

	j, err := s.Submit(ctx, Request{App: "app-1", URL: audioServer(t).URL, Reference: "ep-42"})
	if err != nil {
		t.Fatal(err)
	}
	msg := await(t, pub.msgs)
	var e Failed
	if err := json.Unmarshal(msg.body, &e); err != nil {
		t.Fatal(err)
	}
	if msg.subject != "events.vvaves.transcription.failed" || e.TranscriptionID != j.ID || e.Error != "transcription failed" || e.Reference != "ep-42" {
		t.Fatalf("published %s %+v", msg.subject, e)
	}
}

func TestUnreachableAudioIsReportedAsFetchFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pub := &stubPublisher{msgs: make(chan published, 1)}
	s := newService(stubEngine{fetched: make(chan string, 1)}, pub, nil, NewMemoryStore())
	go s.Run(ctx)

	if _, err := s.Submit(ctx, Request{URL: srv.URL + "?X-Amz-Signature=secret", Reference: "r"}); err != nil {
		t.Fatal(err)
	}
	var e Failed
	json.Unmarshal(await(t, pub.msgs).body, &e)
	if e.Error != "audio could not be fetched" {
		t.Fatalf("error = %q", e.Error)
	}
}

func TestRunResumesPendingJobs(t *testing.T) {
	store := NewMemoryStore()
	j := Job{ID: "00000000-0000-0000-0000-000000000001", Reference: "r", URL: audioServer(t).URL, Status: StatusPending, CreatedAt: time.Now()}
	store.Create(context.Background(), j)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pub := &stubPublisher{msgs: make(chan published, 1)}
	go newService(stubEngine{fetched: make(chan string, 1)}, pub, nil, store).Run(ctx)

	if msg := await(t, pub.msgs); msg.msgID != j.ID+".completed" {
		t.Fatalf("published %s", msg.msgID)
	}
}

func TestGetUnknownID(t *testing.T) {
	s := newService(stubEngine{}, nil, nil, NewMemoryStore())
	for _, id := range []string{"nope", "00000000-0000-0000-0000-000000000009"} {
		if _, err := s.Get(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v", id, err)
		}
	}
}
