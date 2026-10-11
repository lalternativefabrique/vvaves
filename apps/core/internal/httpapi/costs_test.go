package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lalternative/packages/go/audioreader"

	"github.com/lalternativefabrique/vvaves/core/internal/audio"
	"github.com/lalternativefabrique/vvaves/core/internal/costs"
	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
)

type recordedCosts struct {
	mu    sync.Mutex
	lines []costs.Line
}

func (r *recordedCosts) Record(_ context.Context, l costs.Line) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, l)
	return nil
}

func (r *recordedCosts) all() []costs.Line {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]costs.Line(nil), r.lines...)
}

const costCode = "mistral/voxtral-mini-tts-latest.chars"

func meteredDeps(t *testing.T, rec *recordedCosts) httpapi.Deps {
	t.Helper()
	d := guardedDeps(t)
	provider := audio.Metered(audio.NewProvider(&stubVoice{pieces: [][]byte{[]byte("aaa"), []byte("bb")}}), costCode, rec)
	store := newMemStore()
	d.Reader = audioreader.NewReader(provider, store, testOpeningChars, nil)
	d.Primer = audioreader.NewPrimer(provider, store, testOpeningChars, nil)
	return d
}

func charsDeclared(lines []costs.Line) int64 {
	var n int64
	for _, l := range lines {
		n += l.Quantity
	}
	return n
}

func TestSpeakDeclaresTheCharactersOnTheCustomerKeysClient(t *testing.T) {
	rec := &recordedCosts{}
	d := meteredDeps(t, rec)
	d.CustomerKeys = &stubCustomerKeys{clientID: "lalter"}

	if res := postBearer(t, d, "/speak", customerKey, speakBody); res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", res.Code, res.Body.String())
	}

	lines := rec.all()
	if len(lines) != 1 {
		t.Fatalf("lines = %+v, want one", lines)
	}
	l := lines[0]
	if l.Code != costCode || l.ExternalUserID != "lalter" || l.Quantity != int64(utf8.RuneCountInString(longText)) {
		t.Fatalf("line = %+v", l)
	}
	if l.IdempotencyKey == "" || l.OccurredAt.IsZero() {
		t.Fatalf("line lacks a key or a time: %+v", l)
	}
}

func TestSpeakDeclaresNothingOnACacheHit(t *testing.T) {
	rec := &recordedCosts{}
	d := meteredDeps(t, rec)

	for range 2 {
		if res := postBearer(t, d, "/speak", "a-service-token", speakBody); res.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.Code)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if lines := rec.all(); len(lines) != 1 {
		t.Fatalf("lines = %+v, want only the first listen declared", lines)
	}
}

func TestSignedSpeakDeclaresOnTheIssuer(t *testing.T) {
	rec := &recordedCosts{}
	d := meteredDeps(t, rec)

	path := signedQuery("/speak", "chat", "m1", longText, time.Now().Add(time.Minute))
	res := httptest.NewRecorder()
	httpapi.New(d).ServeHTTP(res, httptest.NewRequest(http.MethodPost, path, strings.NewReader(speakBody)))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", res.Code, res.Body.String())
	}

	lines := rec.all()
	if len(lines) != 1 || lines[0].ExternalUserID != testIssuer {
		t.Fatalf("lines = %+v, want one on %q", lines, testIssuer)
	}
}

func TestPrimeDeclaresTheOpeningOnTheCaller(t *testing.T) {
	rec := &recordedCosts{}
	d := meteredDeps(t, rec)
	d.Tokens = stubTokens{"primes": {ClientID: "synthiz", Scopes: []string{httpapi.ScopeSpeak}}}

	if res := postBearer(t, d, "/speak/prime", "primes", speakBody); res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", res.Code)
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(rec.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	lines := rec.all()
	if len(lines) != 1 || lines[0].ExternalUserID != "synthiz" {
		t.Fatalf("lines = %+v, want the opening on synthiz", lines)
	}
	if got := charsDeclared(lines); got <= 0 || got >= int64(utf8.RuneCountInString(longText)) {
		t.Fatalf("declared %d chars, want the opening alone", got)
	}
}
