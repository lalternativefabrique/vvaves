package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAPISpecIsServed(t *testing.T) {
	mux := New(Deps{})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var doc struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if doc.OpenAPI == "" {
		t.Error("served document declares no openapi version")
	}
	for _, want := range []string{"/search", "/fetch", "/render", "/map", "/crawl", "/crawl/{id}", "/speak", "/speak/prime", "/speak/pregenerate", "/speak/exists", "/healthz"} {
		if _, ok := doc.Paths[want]; !ok {
			t.Errorf("contract is missing %s", want)
		}
	}
}
