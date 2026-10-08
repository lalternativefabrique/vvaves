package client

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVoiceTranscribe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, _, err := r.FormFile("audio")
		if err != nil || r.URL.Path != "/transcribe" || r.Header.Get(HeaderKey) != "k" || r.FormValue("language") != "fr" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		b, _ := io.ReadAll(f)
		if string(b) != "RIFF" {
			http.Error(w, "bad audio", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"text":"bonjour"}`))
	}))
	defer srv.Close()
	text, err := New(Config{BaseURL: srv.URL, Key: "k"}).Transcribe(context.Background(), "a.webm", bytes.NewReader([]byte("RIFF")), "fr")
	if err != nil || text != "bonjour" {
		t.Fatalf("%q %v", text, err)
	}
}

func TestVoiceTranscribeCustomerKeyIsBearer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+CustomerKeyPrefix+"x" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()
	if _, err := New(Config{BaseURL: srv.URL, Key: CustomerKeyPrefix + "x"}).Transcribe(context.Background(), "a.webm", bytes.NewReader([]byte("x")), ""); err != nil {
		t.Fatal(err)
	}
}
