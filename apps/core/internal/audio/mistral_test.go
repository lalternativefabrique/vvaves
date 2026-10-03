package audio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lalternative/packages/go/tts"
)

func TestMistralVoiceTranslatesTheWire(t *testing.T) {
	var mu sync.Mutex
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "bad request line", http.StatusBadRequest)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		seen = append(seen, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"audio_data": base64.StdEncoding.EncodeToString([]byte("<" + body["input"].(string) + ">")),
		})
	}))
	defer srv.Close()

	voice := NewMistralVoice(tts.Config{
		BaseURL:     srv.URL,
		APIKey:      "k",
		Model:       "voxtral-mini-tts-latest",
		VoiceID:     "en_paul_sad",
		Format:      "mp3",
		MaxChars:    20,
		Concurrency: 3,
	})

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
		if body["voice_id"] != "en_paul_sad" || body["voice"] != nil {
			t.Errorf("voice not renamed: %v", body)
		}
		if body["stream"] != false || body["response_format"] != "mp3" || body["model"] != "voxtral-mini-tts-latest" {
			t.Errorf("unexpected body: %v", body)
		}
	}
}

func TestMistralVoiceReportsRefusals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"moderation"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	voice := NewMistralVoice(tts.Config{BaseURL: srv.URL, APIKey: "k", VoiceID: "v", Format: "mp3"})
	_, _, err := voice.Speak(context.Background(), "Bonjour.")
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "moderation") {
		t.Fatalf("err = %v", err)
	}
}

func TestMistralVoiceRefusesAnEmptyReading(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"audio_data":""}`))
	}))
	defer srv.Close()

	voice := NewMistralVoice(tts.Config{BaseURL: srv.URL, APIKey: "k", VoiceID: "v", Format: "mp3"})
	if _, _, err := voice.Speak(context.Background(), "Bonjour."); err == nil {
		t.Fatal("an empty audio_data passed for a reading")
	}
}
