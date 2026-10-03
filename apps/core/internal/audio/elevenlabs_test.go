package audio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lalternative/packages/go/tts"
)

func TestElevenLabsVoiceTranslatesTheWire(t *testing.T) {
	var mu sync.Mutex
	var seen []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/text-to-speech/JBFqnCBsd6RMkjVDRZzb" ||
			r.URL.Query().Get("output_format") != "mp3_44100_128" ||
			r.Header.Get("xi-api-key") != "k" || r.Header.Get("Authorization") != "" {
			http.Error(w, "bad request line: "+r.URL.String(), http.StatusBadRequest)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		seen = append(seen, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("<" + body["text"] + ">"))
	}))
	defer srv.Close()

	voice, err := NewElevenLabsVoice(tts.Config{
		BaseURL:     srv.URL,
		APIKey:      "k",
		VoiceID:     "JBFqnCBsd6RMkjVDRZzb",
		MaxChars:    20,
		Concurrency: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	text := "Première phrase ici. Deuxième phrase là. Troisième et dernière."
	audio, mime, err := voice.Speak(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	if mime != "audio/mpeg" {
		t.Errorf("mime = %q", mime)
	}
	if len(seen) < 2 {
		t.Fatalf("text was not cut: %d requests", len(seen))
	}

	var want strings.Builder
	for _, piece := range tts.Split(text, 20) {
		want.WriteString("<" + piece + ">")
	}
	if string(audio) != want.String() {
		t.Errorf("audio = %q, want %q", audio, want.String())
	}
	for _, body := range seen {
		if body["model_id"] != "eleven_multilingual_v2" || len(body) != 2 {
			t.Errorf("unexpected body: %v", body)
		}
	}
}

func TestElevenLabsVoiceReportsRefusals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":{"status":"quota_exceeded"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	voice, err := NewElevenLabsVoice(tts.Config{BaseURL: srv.URL, APIKey: "k", VoiceID: "v"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = voice.Speak(context.Background(), "Bonjour.")
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "quota_exceeded") {
		t.Fatalf("err = %v", err)
	}
}

func TestElevenLabsVoiceRefusesAFormatItCannotJoin(t *testing.T) {
	if _, err := NewElevenLabsVoice(tts.Config{APIKey: "k", VoiceID: "v", Format: "wav"}); err == nil {
		t.Fatal("wav accepted")
	}
}
