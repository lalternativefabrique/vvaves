package keys

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type directory struct {
	known   map[string]string
	revoked []string
}

func (d *directory) Known(_ context.Context, id, name string) error {
	d.known[id] = name
	return nil
}

func (d *directory) Revoked(_ context.Context, id string) error {
	d.revoked = append(d.revoked, id)
	return nil
}

func serve(status int, body string) (*http.ServeMux, *directory) {
	relay := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	d := &directory{known: map[string]string{}}
	mux := http.NewServeMux()
	New(relay, d).RegisterRoutes(mux, "/api/keys", func(h http.Handler) http.Handler { return h })
	return mux, d
}

func call(mux *http.ServeMux, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(`{"label":"x"}`)))
	return rec
}

func TestListingRemembersEveryKeyAndPassesTheAnswerThrough(t *testing.T) {
	body := `{"keys":[{"id":"ak_1","label":"synthiz-prod"},{"id":"ak_2","label":"lalter-prod"}]}`
	mux, d := serve(http.StatusOK, body)
	rec := call(mux, http.MethodGet, "/api/keys")
	if rec.Body.String() != body {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if d.known["ak_1"] != "synthiz-prod" || d.known["ak_2"] != "lalter-prod" {
		t.Fatalf("known = %v", d.known)
	}
}

func TestCreatingRemembersTheNewKey(t *testing.T) {
	mux, d := serve(http.StatusOK, `{"id":"ak_9","label":"messag-prod","secret":"s"}`)
	call(mux, http.MethodPost, "/api/keys")
	if d.known["ak_9"] != "messag-prod" {
		t.Fatalf("known = %v", d.known)
	}
}

func TestRevokingMarksTheKey(t *testing.T) {
	mux, d := serve(http.StatusNoContent, "")
	if rec := call(mux, http.MethodDelete, "/api/keys/ak_9"); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(d.revoked) != 1 || d.revoked[0] != "ak_9" {
		t.Fatalf("revoked = %v", d.revoked)
	}
}

func TestAFailedRelayTellsTheDirectoryNothing(t *testing.T) {
	mux, d := serve(http.StatusServiceUnavailable, `{"error":"unavailable"}`)
	call(mux, http.MethodPost, "/api/keys")
	call(mux, http.MethodDelete, "/api/keys/ak_9")
	if len(d.known) != 0 || len(d.revoked) != 0 {
		t.Fatalf("known %v revoked %v", d.known, d.revoked)
	}
}
