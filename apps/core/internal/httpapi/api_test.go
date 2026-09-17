package httpapi_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lalternative/packages/go/audioreader"
	"github.com/lalternative/packages/go/tts"

	"github.com/lalternativefabrique/vvaves/core/internal/audio"
	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
)

type stubVoice struct {
	pieces [][]byte
	err    error

	mu    sync.Mutex
	spoke []string
}

func (v *stubVoice) Speak(ctx context.Context, text string) ([]byte, string, error) {
	var out []byte
	mime, err := v.SpeakStream(ctx, text, func(a []byte) error {
		out = append(out, a...)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return out, mime, nil
}

func (v *stubVoice) SpeakStream(_ context.Context, text string, emit func([]byte) error) (string, error) {
	v.mu.Lock()
	v.spoke = append(v.spoke, text)
	v.mu.Unlock()

	for _, p := range v.pieces {
		if err := emit(p); err != nil {
			return "", err
		}
	}
	if v.err != nil {
		return "", v.err
	}
	return tts.MIMEFor("mp3"), nil
}

func (v *stubVoice) calls() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.spoke)
}

// spokenText returns the text of the nth reading, counting from zero.
func (v *stubVoice) spokenText(n int) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if n >= len(v.spoke) {
		return ""
	}
	return v.spoke[n]
}

func baseDeps() httpapi.Deps {
	return httpapi.Deps{Unguarded: true}
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

type memStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newMemStore() *memStore { return &memStore{objects: map[string][]byte{}} }

func (m *memStore) Upload(_ context.Context, key string, body io.Reader, _ string) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = b
	return nil
}

func (m *memStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, audioreader.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStore) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.objects))
	for k := range m.objects {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// audioDeps wires a voice and a store the way main does, so a test exercises
// the same reader and primer the service runs.
// audioDeps is a vvaves nobody outside reaches, so it says Unguarded the
// way a laptop's stack does; the guard tests set keys or unset it.
func audioDeps(voice tts.Voice, store audioreader.Store) httpapi.Deps {
	d := baseDeps()
	d.Unguarded = true
	provider := audio.NewProvider(voice)
	d.Reader = audioreader.NewReader(provider, store, testOpeningChars, nil)
	if store != nil {
		d.Primer = audioreader.NewPrimer(provider, store, testOpeningChars, nil)
	}
	return d
}

// testOpeningChars is small so a short test text still splits into several
// pieces — the reader skips priming entirely for a text that is one piece.
const testOpeningChars = 8

// longText spans several pieces at testOpeningChars, which is what priming
// needs to have anything to do.
const longText = "un deux trois quatre cinq six sept huit neuf dix"

func TestSpeakSendsContentLength(t *testing.T) {
	d := audioDeps(&stubVoice{pieces: [][]byte{[]byte("aaa"), []byte("bb")}}, nil)

	rec := post(t, httpapi.New(d), "/speak", `{"text":"bonjour"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	// Without a declared length browsers infer the duration from the first
	// frame header and stop at the first seam.
	if got := rec.Header().Get("Content-Length"); got != "5" {
		t.Errorf("Content-Length = %q, want 5", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "audio/mpeg" {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestSpeakWithoutVoiceIs503(t *testing.T) {
	if rec := post(t, httpapi.New(baseDeps()), "/speak", `{"text":"x"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rec.Code)
	}
}

func TestSpeakRequiresText(t *testing.T) {
	d := audioDeps(&stubVoice{}, nil)
	if rec := post(t, httpapi.New(d), "/speak", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestSpeakStreamEmitsLengthPrefixedFrames(t *testing.T) {
	d := audioDeps(&stubVoice{pieces: [][]byte{[]byte("one"), []byte("two")}}, nil)

	rec := post(t, httpapi.New(d), "/speak", `{"text":"x","stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	// Concatenated mp3 pieces carry no boundary of their own, so each is sent
	// behind a big-endian uint32 count for the player to split them back out.
	if got := frames(t, rec.Body.Bytes()); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Errorf("frames = %q, want [one two]", got)
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Error("a streamed response must not declare a length")
	}
}

// TestSpeakStreamsOnTheQueryFlag covers the other way to ask: audioreader
// reads the flag off the query string, this API has always carried it in the
// body, and both are honoured so a caller can ask either way.
func TestSpeakStreamsOnTheQueryFlag(t *testing.T) {
	d := audioDeps(&stubVoice{pieces: [][]byte{[]byte("one")}}, nil)

	rec := post(t, httpapi.New(d), "/speak?stream=1", `{"text":"x"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if got := frames(t, rec.Body.Bytes()); !reflect.DeepEqual(got, []string{"one"}) {
		t.Errorf("frames = %q, want [one]", got)
	}
}

func TestSpeakStreamFailingBeforeFirstPieceIs502(t *testing.T) {
	d := audioDeps(&stubVoice{err: errors.New("piper down")}, nil)
	if rec := post(t, httpapi.New(d), "/speak", `{"text":"x","stream":true}`); rec.Code != http.StatusBadGateway {
		t.Fatalf("got %d, want 502", rec.Code)
	}
}

// TestSpeakSecondListenSkipsTheVoice is the reason the store exists: someone
// who plays a reading, reloads the page and plays it again must not set Piper
// going a second time for words it has already read.
func TestSpeakSecondListenSkipsTheVoice(t *testing.T) {
	voice := &stubVoice{pieces: [][]byte{[]byte("aaa"), []byte("bb")}}
	store := newMemStore()
	h := httpapi.New(audioDeps(voice, store))

	first := post(t, h, "/speak", `{"text":"bonjour","scope":"note","id":"n1"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first listen: got %d: %s", first.Code, first.Body)
	}
	if voice.calls() != 1 {
		t.Fatalf("first listen made %d voice calls, want 1", voice.calls())
	}
	// The reading is stored off the request's goroutine, so that it survives a
	// listener who leaves the moment the last byte lands.
	waitForKeys(t, store, 1)

	second := post(t, h, "/speak", `{"text":"bonjour","scope":"note","id":"n1"}`)
	if second.Code != http.StatusOK {
		t.Fatalf("second listen: got %d: %s", second.Code, second.Body)
	}
	if voice.calls() != 1 {
		t.Errorf("second listen made the voice read again (%d calls total)", voice.calls())
	}
	if second.Body.String() != first.Body.String() {
		t.Error("the cached reading differs from the one that was paid for")
	}
	if got := second.Header().Get("X-TTS-Cache"); got != "hit" {
		t.Errorf("X-TTS-Cache = %q, want hit", got)
	}
}

// TestSpeakCachesWithoutAnID covers the caller that names nothing: the
// reading is still keyed by its own text, so the same words are not read
// twice.
func TestSpeakCachesWithoutAnID(t *testing.T) {
	voice := &stubVoice{pieces: [][]byte{[]byte("aaa")}}
	store := newMemStore()
	h := httpapi.New(audioDeps(voice, store))

	post(t, h, "/speak", `{"text":"bonjour"}`)
	waitForKeys(t, store, 1)
	post(t, h, "/speak", `{"text":"bonjour"}`)
	if voice.calls() != 1 {
		t.Errorf("voice read %d times, want 1", voice.calls())
	}
}

// TestSpeakRereadsEditedText is the other half of a content-addressed key: a
// text that changed must not be served as the audio of the words it replaced.
func TestSpeakRereadsEditedText(t *testing.T) {
	voice := &stubVoice{pieces: [][]byte{[]byte("aaa")}}
	store := newMemStore()
	h := httpapi.New(audioDeps(voice, store))

	post(t, h, "/speak", `{"text":"bonjour","scope":"note","id":"n1"}`)
	waitForKeys(t, store, 1)
	post(t, h, "/speak", `{"text":"bonsoir","scope":"note","id":"n1"}`)
	if voice.calls() != 2 {
		t.Errorf("voice read %d times, want 2: an edit must miss the cache", voice.calls())
	}
}

func TestPrimeStoresTheOpeningAndAnswersAccepted(t *testing.T) {
	store := newMemStore()
	voice := &stubVoice{pieces: [][]byte{[]byte("opening")}}
	h := httpapi.New(audioDeps(voice, store))

	body := `{"text":"` + longText + `","scope":"chat","id":"m1"}`
	if got := post(t, h, "/speak/prime", body); got.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", got.Code)
	}

	keys := waitForKeys(t, store, 1)
	if !strings.HasSuffix(keys[0], ".opening") {
		t.Errorf("stored %q, want an opening", keys[0])
	}
	if !strings.HasPrefix(keys[0], "chat/m1-") {
		t.Errorf("stored %q, want it under chat/m1", keys[0])
	}
	if voice.calls() != 1 {
		t.Errorf("priming read %d times, want 1: only the opening is read ahead", voice.calls())
	}
}

// TestPrimedOpeningIsServedFirst is the point of priming: the first listen
// starts on audio that was already made, and the voice is only asked for what
// is left.
func TestPrimedOpeningIsServedFirst(t *testing.T) {
	store := newMemStore()
	voice := &stubVoice{pieces: [][]byte{[]byte("rest")}}
	h := httpapi.New(audioDeps(voice, store))

	body := `{"text":"` + longText + `","scope":"chat","id":"m1"}`
	post(t, h, "/speak/prime", body)
	waitForKeys(t, store, 1)

	rec := post(t, h, "/speak", `{"text":"`+longText+`","scope":"chat","id":"m1","stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	got := frames(t, rec.Body.Bytes())
	if len(got) == 0 || got[0] != "rest" {
		t.Fatalf("frames = %q, want the primed opening first", got)
	}
	if voice.spokenText(1) == longText {
		t.Error("the voice was handed the whole text: the primed opening was read twice")
	}
}

func TestPrimeWithoutAnIDIs400(t *testing.T) {
	h := httpapi.New(audioDeps(&stubVoice{}, newMemStore()))
	if rec := post(t, h, "/speak/prime", `{"text":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestPrimeWithoutAStoreIs503(t *testing.T) {
	h := httpapi.New(audioDeps(&stubVoice{}, nil))
	if rec := post(t, h, "/speak/prime", `{"text":"x","id":"a"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rec.Code)
	}
}

func TestPregenerateFillsTheCacheSoTheFirstListenPaysNothing(t *testing.T) {
	store := newMemStore()
	voice := &stubVoice{pieces: [][]byte{[]byte("whole")}}
	h := httpapi.New(audioDeps(voice, store))

	body := `{"text":"bonjour","scope":"page","id":"p1"}`
	if rec := post(t, h, "/speak/pregenerate", body); rec.Code != http.StatusAccepted {
		t.Fatalf("got %d, want 202", rec.Code)
	}
	waitForKeys(t, store, 1)

	rec := post(t, h, "/speak", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if voice.calls() != 1 {
		t.Errorf("voice read %d times, want 1: the listen must come from the cache", voice.calls())
	}
	if got := rec.Header().Get("X-TTS-Cache"); got != "hit" {
		t.Errorf("X-TTS-Cache = %q, want hit", got)
	}
}

func TestExistsReportsWhetherAReadingIsStored(t *testing.T) {
	store := newMemStore()
	h := httpapi.New(audioDeps(&stubVoice{pieces: [][]byte{[]byte("a")}}, store))
	body := `{"text":"bonjour","scope":"page","id":"p1"}`

	if ready(t, post(t, h, "/speak/exists", body)) {
		t.Error("nothing is stored yet")
	}
	post(t, h, "/speak/pregenerate", body)
	waitForKeys(t, store, 1)
	if !ready(t, post(t, h, "/speak/exists", body)) {
		t.Error("the reading is stored but exists says otherwise")
	}
}

func ready(t *testing.T, rec *httptest.ResponseRecorder) bool {
	t.Helper()
	var out struct {
		Ready bool `json:"ready"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode exists: %v", err)
	}
	return out.Ready
}

// frames splits a streamed body back into the pieces it was written from.
func frames(t *testing.T, body []byte) []string {
	t.Helper()
	var out []string
	for len(body) >= 4 {
		n := binary.BigEndian.Uint32(body[:4])
		if len(body) < int(4+n) {
			t.Fatalf("truncated frame: want %d bytes, have %d", n, len(body)-4)
		}
		out = append(out, string(body[4:4+n]))
		body = body[4+n:]
	}
	if len(body) != 0 {
		t.Fatalf("%d trailing bytes outside any frame", len(body))
	}
	return out
}

// waitForKeys waits for the store to hold n objects. Priming and
// pregenerating answer before the reading is done, on purpose — nobody is
// waiting on them — so a test has to.
func waitForKeys(t *testing.T, store *memStore, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		keys := store.keys()
		if len(keys) >= n {
			return keys
		}
		if time.Now().After(deadline) {
			t.Fatalf("stored %d objects, want %d", len(keys), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
