package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lalternative/packages/go/audioreader"
	"github.com/lalternative/packages/vvaves/sdk-go/signed"

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

func postForEndUser(t *testing.T, d httpapi.Deps, path, token, user string) *httptest.ResponseRecorder {
	t.Helper()
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(speakBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(httpapi.EndUserHeader, user)
	httpapi.New(d).ServeHTTP(res, req)
	return res
}

func TestSpeakDeclaresTheEndUserTheCallerNamed(t *testing.T) {
	rec := &recordedCosts{}
	d := meteredDeps(t, rec)
	d.CustomerKeys = &stubCustomerKeys{clientID: "ak_synthiz"}

	if res := postForEndUser(t, d, "/speak", customerKey, "u42"); res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", res.Code, res.Body.String())
	}

	lines := rec.all()
	if len(lines) != 1 || lines[0].ExternalUserID != "ak_synthiz" || lines[0].EndUserID != "u42" {
		t.Fatalf("lines = %+v, want one on ak_synthiz for u42", lines)
	}
}

func TestSpeakRefusesAnEndUserNoLineCanCarry(t *testing.T) {
	d := meteredDeps(t, &recordedCosts{})
	d.CustomerKeys = &stubCustomerKeys{clientID: "ak_synthiz"}

	if res := postForEndUser(t, d, "/speak", customerKey, strings.Repeat("u", 300)); res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.Code)
	}
}

func TestSignedSpeakDeclaresTheSignedEndUser(t *testing.T) {
	rec := &recordedCosts{}
	d := meteredDeps(t, rec)

	q, err := signed.Sign(testIssuer, testKey, signed.Params{
		Scope: "chat", ID: "m1", TextHash: signed.HashText(longText), Expires: time.Now().Add(time.Minute), User: "u42",
	})
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	httpapi.New(d).ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/speak?"+q.Encode(), strings.NewReader(speakBody)))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", res.Code, res.Body.String())
	}

	lines := rec.all()
	if len(lines) != 1 || lines[0].ExternalUserID != testIssuer || lines[0].EndUserID != "u42" {
		t.Fatalf("lines = %+v, want one on %q for u42", lines, testIssuer)
	}
}

func TestAURLVvavesSignedDeclaresTheKeyThatAskedAndItsEndUser(t *testing.T) {
	rec := &recordedCosts{}
	d := meteredDeps(t, rec)
	d.CustomerKeys = &stubCustomerKeys{clientID: "ak_lalter"}
	d.SigningSecret = signingSecret
	d.Verifier = signed.NewLookupVerifier(httpapi.WithSigningIssuer(nil, signingSecret))

	res := postForEndUser(t, d, "/speak/sign", customerKey, "u7")
	if res.Code != http.StatusOK {
		t.Fatalf("sign: status = %d (body %q)", res.Code, res.Body.String())
	}
	var got struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(got.URL)
	play := httptest.NewRecorder()
	httpapi.New(d).ServeHTTP(play, httptest.NewRequest(http.MethodPost, "/speak?"+u.RawQuery, strings.NewReader(speakBody)))
	if play.Code != http.StatusOK {
		t.Fatalf("play: status = %d (body %q)", play.Code, play.Body.String())
	}

	lines := rec.all()
	if len(lines) != 1 || lines[0].ExternalUserID != "ak_lalter" || lines[0].EndUserID != "u7" {
		t.Fatalf("lines = %+v, want one on ak_lalter for u7", lines)
	}
}
