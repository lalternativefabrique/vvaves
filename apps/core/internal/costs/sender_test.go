package costs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLungorPostsTheLinesWithTheAppKeyAndNoAppID(t *testing.T) {
	var got map[string][]map[string]any
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := Lungor{URL: srv.URL + "/", AppKey: "k"}.Post(context.Background(), []Line{{
		Code: "mistral/m.chars", Quantity: 12, OccurredAt: time.Unix(0, 0).UTC(), IdempotencyKey: "x.chars", ExternalUserID: "lalter",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer k" || path != "/api/v1/metering/costs" {
		t.Fatalf("auth %q path %q", auth, path)
	}
	line := got["lines"][0]
	if _, ok := line["app_id"]; ok {
		t.Fatal("a line must never name an app_id")
	}
	if line["code"] != "mistral/m.chars" || line["quantity"] != float64(12) || line["external_user_id"] != "lalter" || line["idempotency_key"] != "x.chars" {
		t.Fatalf("line = %v", line)
	}
}

func TestClassifyParksRefusalsAndRetriesTheRest(t *testing.T) {
	for _, s := range []int{400, 403, 404, 409, 422} {
		if err := classify(s, ""); !errors.Is(err, errRejected) {
			t.Errorf("%d: want rejected, got %v", s, err)
		}
	}
	for _, s := range []int{401, 429, 500, 503} {
		if err := classify(s, ""); err == nil || errors.Is(err, errRejected) {
			t.Errorf("%d: want a retry, got %v", s, err)
		}
	}
	if err := classify(201, ""); err != nil {
		t.Errorf("201: %v", err)
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	if backoff(0) != 5*time.Second || backoff(1) != 10*time.Second || backoff(30) != maxBackoff {
		t.Fatalf("backoff = %v %v %v", backoff(0), backoff(1), backoff(30))
	}
}

func TestPutClientRegistersTheKeyUnderItsID(t *testing.T) {
	var method, path, auth string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	if err := (Lungor{URL: srv.URL, AppKey: "k"}).PutClient(context.Background(), "ak_1", "synthiz-prod", &at); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPut || path != "/api/v1/metering/clients/ak_1" || auth != "Bearer k" {
		t.Fatalf("%s %s %s", method, path, auth)
	}
	if got["name"] != "synthiz-prod" || got["revoked_at"] != "2026-10-11T00:00:00Z" {
		t.Fatalf("body = %v", got)
	}
}

func TestPutClientSendsANullRevocationForAnActiveKey(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	_ = Lungor{URL: srv.URL, AppKey: "k"}.PutClient(context.Background(), "ak_1", "n", nil)
	if v, ok := got["revoked_at"]; !ok || v != nil {
		t.Fatalf("revoked_at = %v (present %v), want null", v, ok)
	}
}

func TestPutClientRetriesALungorWithoutTheRoute(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	err := Lungor{URL: srv.URL, AppKey: "k"}.PutClient(context.Background(), "ak_1", "n", nil)
	if err == nil || errors.Is(err, errRejected) {
		t.Fatalf("err = %v, want a retry", err)
	}
}
