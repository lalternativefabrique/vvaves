package client

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lalternative/packages/go/audioreader"
)

func frameBytes(pieces ...[]byte) []byte {
	var out []byte
	for _, p := range pieces {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(p)))
		out = append(out, length[:]...)
		out = append(out, p...)
	}
	return out
}

func TestVoiceNilWithoutBaseURL(t *testing.T) {
	if v := New(Config{}); v != nil {
		t.Fatal("expected nil voice without a BaseURL")
	}
}

func TestVoiceSpeak(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speak" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("mp3-bytes"))
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL + "/", Scope: "chat"})
	audio, mime, err := v.Speak(context.Background(), "bonjour")
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "mp3-bytes" || mime != "audio/mpeg" {
		t.Fatalf("audio=%q mime=%q", audio, mime)
	}
	if got["text"] != "bonjour" || got["scope"] != "chat" || got["stream"] != false {
		t.Fatalf("request body = %v", got)
	}
	if _, named := got["id"]; named {
		t.Fatalf("request body = %v, want no id: an unnamed reading is keyed on its text alone", got)
	}
}

func TestVoiceSpeakStreamFrames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["stream"] != true {
			t.Errorf("stream = %v", req["stream"])
		}
		w.Header().Set("Content-Type", audioreader.FramesContentType)
		w.Write(frameBytes([]byte("one"), []byte("two")))
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL})
	var pieces []string
	mime, err := v.SpeakStream(context.Background(), "text", func(p []byte) error {
		pieces = append(pieces, string(p))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if mime != "audio/mpeg" {
		t.Fatalf("mime = %q", mime)
	}
	if len(pieces) != 2 || pieces[0] != "one" || pieces[1] != "two" {
		t.Fatalf("pieces = %v", pieces)
	}
}

func TestVoiceSpeakStreamCacheHitServedWhole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("whole-cached"))
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL})
	var pieces []string
	mime, err := v.SpeakStream(context.Background(), "text", func(p []byte) error {
		pieces = append(pieces, string(p))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if mime != "audio/mpeg" || len(pieces) != 1 || pieces[0] != "whole-cached" {
		t.Fatalf("mime=%q pieces=%v", mime, pieces)
	}
}

func TestVoiceErrorCarriesDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"speech is not configured"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL})
	_, _, err := v.Speak(context.Background(), "text")
	if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestVoicePregenerate(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speak/pregenerate" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL, Scope: "writing-live"})
	if err := v.Pregenerate(context.Background(), "abc123", "le texte"); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "abc123" || got["scope"] != "writing-live" || got["text"] != "le texte" {
		t.Fatalf("request body = %v", got)
	}
}

func TestVoicePrimeOpening(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speak/prime" {
			t.Errorf("path = %q, want /speak/prime — pregenerate would read the whole text", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL, Scope: "chat-message"})
	if err := v.PrimeOpening(context.Background(), "msg-1", "le texte"); err != nil {
		t.Fatal(err)
	}
	// Priming means naming ahead of time what will be listened to: without
	// the id the opening could never be found again.
	if got["id"] != "msg-1" || got["scope"] != "chat-message" || got["text"] != "le texte" {
		t.Fatalf("request body = %v", got)
	}
}

// A reading asked for by name is what finds one primed under that name.
func TestVoiceSpeakNamedSendsTheID(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("audio"))
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL, Scope: "chat-message"})
	audio, mime, err := v.SpeakNamed(context.Background(), "msg-1", "le texte")
	if err != nil {
		t.Fatal(err)
	}
	if got["id"] != "msg-1" || got["scope"] != "chat-message" {
		t.Fatalf("request body = %v, want the reading named", got)
	}
	if string(audio) != "audio" || mime != "audio/mpeg" {
		t.Errorf("got (%q, %q)", audio, mime)
	}
}

func TestVoicePrimeOpeningReportsAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"priming needs both speech and a store"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL, Scope: "chat-message"})
	if err := v.PrimeOpening(context.Background(), "msg-1", "le texte"); err == nil {
		t.Fatal("a 503 must be reported so the caller can log it")
	}
}

// Only a streamed listen is served a primed opening, so the named stream is
// what makes priming pay: it must carry both the name and the stream flag.
func TestVoiceSpeakStreamNamedSendsTheID(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", audioreader.FramesContentType)
		w.Write(frameBytes([]byte("opening"), []byte("rest")))
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL, Scope: "chat-message"})
	var pieces []string
	mime, err := v.SpeakStreamNamed(context.Background(), "msg-1", "le texte", func(p []byte) error {
		pieces = append(pieces, string(p))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["id"] != "msg-1" || got["scope"] != "chat-message" || got["stream"] != true {
		t.Fatalf("request body = %v, want the reading named and streamed", got)
	}
	if mime != "audio/mpeg" || len(pieces) != 2 || pieces[0] != "opening" || pieces[1] != "rest" {
		t.Fatalf("mime=%q pieces=%v", mime, pieces)
	}
}

func TestAppKeyIsSentOnEveryCall(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get(HeaderKey))
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("audio"))
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL, Key: "an-app-key"})
	if _, _, err := v.Speak(context.Background(), "bonjour"); err != nil {
		t.Fatal(err)
	}
	if err := v.PrimeOpening(context.Background(), "msg-1", "bonjour"); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != "an-app-key" || keys[1] != "an-app-key" {
		t.Fatalf("app keys sent = %q, want it on both calls", keys)
	}
}

func TestNoAppKeyHeaderWithoutOne(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header[HeaderKey]
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("audio"))
	}))
	defer srv.Close()

	if _, _, err := New(Config{BaseURL: srv.URL}).Speak(context.Background(), "bonjour"); err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("an empty app key was sent as a header")
	}
}

func TestExistsAsksByNameAndReadsReady(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speak/exists" {
			t.Errorf("path = %q, want /speak/exists", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ready":true}`))
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL, Scope: "pensee-note"})
	ready, err := v.Exists(context.Background(), "p-1", "le texte")
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("ready = false, want what the server said")
	}
	if got["id"] != "p-1" || got["scope"] != "pensee-note" || got["text"] != "le texte" {
		t.Fatalf("request body = %v", got)
	}
}

func TestAuthorizeReplacesTheAppKey(t *testing.T) {
	var auth, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		key = r.Header.Get(HeaderKey)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("audio"))
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL, Key: "an-app-key", Authorize: func(r *http.Request) error {
		r.Header.Set("Authorization", "Bearer a-token")
		return nil
	}})
	if _, _, err := v.Speak(context.Background(), "bonjour"); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer a-token" || key != "" {
		t.Fatalf("Authorization=%q %s=%q, want the token and no key", auth, HeaderKey, key)
	}
}

func TestCustomerKeyGoesAsBearer(t *testing.T) {
	var auth, key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		key = r.Header.Get(HeaderKey)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("audio"))
	}))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL, Key: CustomerKeyPrefix + "jwt"})
	if _, _, err := v.Speak(context.Background(), "bonjour"); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer "+CustomerKeyPrefix+"jwt" || key != "" {
		t.Fatalf("Authorization=%q %s=%q, want the customer key as bearer only", auth, HeaderKey, key)
	}
}

func TestAuthorizeFailureStopsTheCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()

	v := New(Config{BaseURL: srv.URL, Authorize: func(*http.Request) error { return errors.New("issuer down") }})
	if _, _, err := v.Speak(context.Background(), "bonjour"); err == nil || !strings.Contains(err.Error(), "issuer down") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("vvaves was called without a credential")
	}
}
