// Package voicepick keeps the voice every reading uses unless a deployment says
// otherwise, chosen from the provider's catalogue in the admin.
package voicepick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Voice struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Languages []string `json:"languages"`
	Gender    string   `json:"gender,omitempty"`
}

type Catalogue interface {
	List(ctx context.Context) ([]Voice, error)
}

// Picker holds the chosen voice and the built readers for each voice it has
// served, so switching back and forth does not rebuild them.
type Picker[R any] struct {
	pool  *pgxpool.Pool
	build func(voiceID string) R

	mu      sync.RWMutex
	current string
	built   map[string]R
}

func NewPicker[R any](ctx context.Context, pool *pgxpool.Pool, build func(voiceID string) R) (*Picker[R], error) {
	p := &Picker[R]{pool: pool, build: build, built: map[string]R{}}
	err := pool.QueryRow(ctx, `SELECT voice_id FROM default_voice`).Scan(&p.current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load default voice: %w", err)
	}
	return p, nil
}

func (p *Picker[R]) Current() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

// Reader returns the reader of the chosen voice, false when none was chosen
// and the deployment's own voices apply.
func (p *Picker[R]) Reader() (R, string, bool) {
	p.mu.RLock()
	id := p.current
	r, ok := p.built[id]
	p.mu.RUnlock()
	if id == "" {
		var zero R
		return zero, "", false
	}
	if ok {
		return r, id, true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.built[id]; ok {
		return r, id, true
	}
	r = p.build(id)
	p.built[id] = r
	return r, id, true
}

func (p *Picker[R]) Choose(ctx context.Context, voiceID string) error {
	if voiceID == "" {
		return errors.New("voice_id is required")
	}
	if err := p.pool.QueryRow(ctx, `
		INSERT INTO default_voice (singleton, voice_id, updated_at) VALUES (true, $1, now())
		ON CONFLICT (singleton) DO UPDATE SET voice_id = EXCLUDED.voice_id, updated_at = now()
		RETURNING voice_id`, voiceID).Scan(new(string)); err != nil {
		return fmt.Errorf("save default voice: %w", err)
	}
	p.mu.Lock()
	p.current = voiceID
	p.mu.Unlock()
	return nil
}

type Mistral struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func (m Mistral) List(ctx context.Context) ([]Voice, error) {
	base := m.BaseURL
	if base == "" {
		base = "https://api.mistral.ai"
	}
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/audio/voices?limit=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mistral voices: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mistral voices: status %d", res.StatusCode)
	}
	var body struct {
		Items []Voice `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("mistral voices: %w", err)
	}
	return body.Items, nil
}

func Handler[R any](picker *Picker[R], catalogue Catalogue) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		list, err := catalogue.List(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"current": picker.Current(), "voices": list})
	})
	mux.HandleFunc("PUT /", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			VoiceID string `json:"voice_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.VoiceID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "voice_id is required"})
			return
		}
		if err := picker.Choose(r.Context(), body.VoiceID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"current": body.VoiceID})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
