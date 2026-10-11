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
