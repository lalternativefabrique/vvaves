package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lalternative/packages/go/tts"
)

type elevenLabsStub struct {
	mu       sync.Mutex
	bodies   []map[string]string
	inFlight atomic.Int32
	peak     atomic.Int32
}

func (s *elevenLabsStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := s.inFlight.Add(1)
	defer s.inFlight.Add(-1)
	for {
		p := s.peak.Load()
		if n <= p || s.peak.CompareAndSwap(p, n) {
			break
		}
	}

	if !strings.HasPrefix(r.URL.Path, "/v1/text-to-speech/") || !strings.HasSuffix(r.URL.Path, "/stream") ||
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
	s.mu.Lock()
	s.bodies = append(s.bodies, body)
	s.mu.Unlock()

	time.Sleep(20 * time.Millisecond)
	w.Header().Set("Content-Type", "audio/mpeg")
	frames := 1 + len(body["text"])
	for i := range frames {
		w.Write(mp3Frame(i%2 == 0))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func TestElevenLabsVoiceStreamsFlash(t *testing.T) {
	stub := &elevenLabsStub{}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	voice, err := NewElevenLabs(srv.URL, "k", 2).Voice(tts.Config{
		VoiceID:     "EXAVITQu4vr4xnSDxMaL",
		MaxChars:    20,
		Concurrency: 4,
	}, "fr")
	if err != nil {
		t.Fatal(err)
	}

	text := "Première phrase ici. Deuxième phrase là. Troisième et dernière. Une quatrième."
	var pieces [][]byte
	mime, err := voice.SpeakStream(context.Background(), text, func(audio []byte) error {
		pieces = append(pieces, bytes.Clone(audio))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if mime != "audio/mpeg" {
		t.Errorf("mime = %q", mime)
	}

	var want []byte
	for _, piece := range tts.Split(text, 20) {
		for i := range 1 + len(piece) {
			want = append(want, mp3Frame(i%2 == 0)...)
		}
	}
	if !bytes.Equal(bytes.Join(pieces, nil), want) {
		t.Errorf("audio is not the pieces' frames in reading order")
	}
	for _, body := range stub.bodies {
		if body["model_id"] != "eleven_flash_v2_5" || body["language_code"] != "fr" {
			t.Errorf("unexpected body: %v", body)
		}
	}
	if peak := stub.peak.Load(); peak > 2 {
		t.Errorf("%d requests in flight, the account allows 2", peak)
	}
}

func TestElevenLabsAccountCapsEveryVoiceTogether(t *testing.T) {
	stub := &elevenLabsStub{}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	account := NewElevenLabs(srv.URL, "k", 1)
	var wg sync.WaitGroup
	for _, lang := range []string{"fr", "en", ""} {
		voice, err := account.Voice(tts.Config{VoiceID: "v-" + lang, MaxChars: 20, Concurrency: 3}, lang)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := voice.Speak(context.Background(), "Une phrase. Une autre phrase. Encore une."); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if peak := stub.peak.Load(); peak != 1 {
		t.Errorf("%d requests in flight, the account allows 1", peak)
	}
	for _, body := range stub.bodies {
		if _, set := body["language_code"]; set && body["language_code"] == "" {
			t.Errorf("empty language_code sent: %v", body)
		}
	}
}

func TestElevenLabsVoiceReportsRefusalsAndFreesTheSlot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":{"status":"quota_exceeded"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	voice, err := NewElevenLabs(srv.URL, "k", 1).Voice(tts.Config{VoiceID: "v"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, _, err = voice.Speak(ctx, "Bonjour.")
		cancel()
		if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "quota_exceeded") {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestElevenLabsVoiceOnlyStreamsMP3(t *testing.T) {
	if _, err := NewElevenLabs("", "k", 1).Voice(tts.Config{VoiceID: "v", Format: "opus"}, ""); err == nil {
		t.Fatal("opus accepted")
	}
}
