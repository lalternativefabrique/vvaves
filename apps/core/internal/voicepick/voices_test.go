package voicepick

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestMistralReportsARefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := (Mistral{BaseURL: srv.URL, APIKey: "k"}).List(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestReaderBuildsTheChosenVoiceOnce(t *testing.T) {
	builds := 0
	p := &Picker[string]{build: func(id string) string { builds++; return "reader:" + id }, built: map[string]string{}}
	if _, _, ok := p.Reader(); ok {
		t.Fatal("no voice chosen yet, expected the deployment's voices")
	}
	p.current = "v1"
	for range 3 {
		r, id, ok := p.Reader()
		if !ok || id != "v1" || r != "reader:v1" {
			t.Fatalf("got %q %q %v", r, id, ok)
		}
	}
	if builds != 1 {
		t.Fatalf("built %d times", builds)
	}
}
