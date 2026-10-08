package stt

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewWithoutKeyIsNil(t *testing.T) {
	if New(Config{}) != nil {
		t.Fatal("a transcriber with no key must be absent")
	}
}

func TestTranscribeSendsAudioModelAndKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "no file", http.StatusBadRequest)
			return
		}
		audio, _ := io.ReadAll(f)
		if r.Header.Get("Authorization") != "Bearer k" || r.FormValue("model") != "m" || r.FormValue("language") != "fr" || string(audio) != "RIFF" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"text":" bonjour "}`))
	}))
	defer srv.Close()
	text, err := New(Config{URL: srv.URL, APIKey: "k", Model: "m"}).Transcribe(context.Background(), "a.webm", bytes.NewReader([]byte("RIFF")), "fr")
	if err != nil || text != "bonjour" {
		t.Fatalf("%q %v", text, err)
	}
}

func TestTranscribeReportsUpstreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "quota", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	if _, err := New(Config{URL: srv.URL, APIKey: "k"}).Transcribe(context.Background(), "a.webm", bytes.NewReader([]byte("x")), ""); err == nil {
		t.Fatal("expected an error")
	}
}
