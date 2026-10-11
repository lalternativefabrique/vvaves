package keyvoice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMistralListsTheCatalogue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/voices" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("unexpected request %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"items":[{"id":"v1","name":"Marie","languages":["fr"],"gender":"female"}],"total":1}`))
	}))
	defer srv.Close()

	list, err := Mistral{BaseURL: srv.URL, APIKey: "k"}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "v1" || list[0].Name != "Marie" || list[0].Languages[0] != "fr" {
		t.Fatalf("unexpected catalogue %+v", list)
	}
}

func TestReadersBuildEachVoiceOnce(t *testing.T) {
	builds := 0
	r := NewReaders(func(id string) string { builds++; return "reader:" + id })
	for range 3 {
		if got := r.For("v1"); got != "reader:v1" {
			t.Fatalf("got %q", got)
		}
	}
	r.For("v2")
	if builds != 2 {
		t.Fatalf("built %d times, want 2", builds)
	}
}

type fixedCatalogue []Voice

func (c fixedCatalogue) List(context.Context) ([]Voice, error) { return c, nil }

type memStore struct{ voices map[string]string }

func (m *memStore) Of(context.Context, string) (map[string]string, error) { return m.voices, nil }
func (m *memStore) Set(_ context.Context, _, key, voice string) error {
	m.voices[key] = voice
	return nil
}
func (m *memStore) Clear(_ context.Context, _, key string) error {
	delete(m.voices, key)
	return nil
}

func testHandler(s *memStore) http.Handler {
	return Handler(s, fixedCatalogue{{ID: "v1"}}, Owners{
		OwnerOf: func(*http.Request) (string, bool) { return "alice", true },
		Owns:    func(_ *http.Request, key string) (bool, error) { return key == "ak_mine", nil },
	})
}

func put(h http.Handler, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, path, strings.NewReader(body)))
	return rec
}

func TestChoosingAVoiceForOnesOwnKey(t *testing.T) {
	s := &memStore{voices: map[string]string{}}
	if rec := put(testHandler(s), "/keys/ak_mine", `{"voice_id":"v1"}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	if s.voices["ak_mine"] != "v1" {
		t.Fatalf("stored %v", s.voices)
	}
}

func TestChoosingAVoiceForSomeoneElsesKeyIsRefused(t *testing.T) {
	s := &memStore{voices: map[string]string{}}
	if rec := put(testHandler(s), "/keys/ak_theirs", `{"voice_id":"v1"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if len(s.voices) != 0 {
		t.Fatalf("stored %v", s.voices)
	}
}

func TestAVoiceOutsideTheCatalogueIsRefused(t *testing.T) {
	s := &memStore{voices: map[string]string{}}
	if rec := put(testHandler(s), "/keys/ak_mine", `{"voice_id":"made-up"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestClearingAKeysVoice(t *testing.T) {
	s := &memStore{voices: map[string]string{"ak_mine": "v1"}}
	rec := httptest.NewRecorder()
	testHandler(s).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/keys/ak_mine", nil))
	if rec.Code != http.StatusNoContent || len(s.voices) != 0 {
		t.Fatalf("status = %d, stored %v", rec.Code, s.voices)
	}
}
