// Package keyvoice keeps the voice each application key is read in, chosen by
// the key's owner from the provider's catalogue.
package keyvoice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
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

var ErrNotYours = errors.New("keyvoice: this key belongs to someone else")

const lookupTTL = 30 * time.Second

type cached struct {
	voiceID string
	at      time.Time
}

type Store struct {
	pool *pgxpool.Pool

	mu    sync.Mutex
	cache map[string]cached
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, cache: map[string]cached{}}
}

// VoiceOf is on every reading's path, so it answers from memory for a while;
// another replica's change lands within lookupTTL.
func (s *Store) VoiceOf(ctx context.Context, keyID string) (string, error) {
	s.mu.Lock()
	c, ok := s.cache[keyID]
	s.mu.Unlock()
	if ok && time.Since(c.at) < lookupTTL {
		return c.voiceID, nil
	}
	var voiceID string
	err := s.pool.QueryRow(ctx, `SELECT voice_id FROM key_voice WHERE key_id = $1`, keyID).Scan(&voiceID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("load key voice: %w", err)
	}
	s.remember(keyID, voiceID)
	return voiceID, nil
}

func (s *Store) remember(keyID, voiceID string) {
	s.mu.Lock()
	s.cache[keyID] = cached{voiceID: voiceID, at: time.Now()}
	s.mu.Unlock()
}

func (s *Store) Of(ctx context.Context, owner string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT key_id, voice_id FROM key_voice WHERE owner_id = $1`, owner)
	if err != nil {
		return nil, fmt.Errorf("list key voices: %w", err)
	}
	out := map[string]string{}
	for rows.Next() {
		var key, voice string
		if err := rows.Scan(&key, &voice); err != nil {
			return nil, err
		}
		out[key] = voice
	}
	return out, rows.Err()
}

func (s *Store) Set(ctx context.Context, owner, keyID, voiceID string) error {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO key_voice (key_id, owner_id, voice_id, updated_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (key_id) DO UPDATE SET voice_id = EXCLUDED.voice_id, updated_at = now()
		WHERE key_voice.owner_id = EXCLUDED.owner_id`, keyID, owner, voiceID)
	if err != nil {
		return fmt.Errorf("save key voice: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotYours
	}
	s.remember(keyID, voiceID)
	return nil
}

func (s *Store) Clear(ctx context.Context, owner, keyID string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM key_voice WHERE key_id = $1 AND owner_id = $2`, keyID, owner); err != nil {
		return fmt.Errorf("clear key voice: %w", err)
	}
	s.remember(keyID, "")
	return nil
}

// Readers builds one reader per voice and keeps it, so keys sharing a voice
// share its reader and its cache.
type Readers[R any] struct {
	build func(voiceID string) R

	mu    sync.Mutex
	built map[string]R
}

func NewReaders[R any](build func(voiceID string) R) *Readers[R] {
	return &Readers[R]{build: build, built: map[string]R{}}
}

func (b *Readers[R]) For(voiceID string) R {
	b.mu.Lock()
	defer b.mu.Unlock()
	if r, ok := b.built[voiceID]; ok {
		return r
	}
	r := b.build(voiceID)
	b.built[voiceID] = r
	return r
}

const catalogueTTL = 5 * time.Minute

// CachedCatalogue spares the provider a call per page view.
type CachedCatalogue struct {
	Source Catalogue

	mu     sync.Mutex
	voices []Voice
	at     time.Time
}

func (c *CachedCatalogue) List(ctx context.Context) ([]Voice, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.voices != nil && time.Since(c.at) < catalogueTTL {
		return c.voices, nil
	}
	voices, err := c.Source.List(ctx)
	if err != nil {
		return nil, err
	}
	c.voices, c.at = voices, time.Now()
	return voices, nil
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

type Owners struct {
	OwnerOf func(*http.Request) (string, bool)
	Owns    func(r *http.Request, keyID string) (bool, error)
}

type store interface {
	Of(ctx context.Context, owner string) (map[string]string, error)
	Set(ctx context.Context, owner, keyID, voiceID string) error
	Clear(ctx context.Context, owner, keyID string) error
}

// Handler serves the voices page, mounted with its prefix stripped.
func Handler(s store, catalogue Catalogue, owners Owners) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /catalogue", func(w http.ResponseWriter, r *http.Request) {
		list, err := catalogue.List(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"voices": list})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := owners.OwnerOf(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign_in_required"})
			return
		}
		voices, err := s.Of(r.Context(), owner)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"voices": voices})
	})
	mux.HandleFunc("PUT /keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		owner, keyID, ok := ownedKey(w, r, owners)
		if !ok {
			return
		}
		var body struct {
			VoiceID string `json:"voice_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.VoiceID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "voice_id is required"})
			return
		}
		list, err := catalogue.List(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if !slices.ContainsFunc(list, func(v Voice) bool { return v.ID == body.VoiceID }) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown_voice"})
			return
		}
		switch err := s.Set(r.Context(), owner, keyID, body.VoiceID); {
		case errors.Is(err, ErrNotYours):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown_key"})
		case err != nil:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusOK, map[string]string{"key_id": keyID, "voice_id": body.VoiceID})
		}
	})
	mux.HandleFunc("DELETE /keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		owner, keyID, ok := ownedKey(w, r, owners)
		if !ok {
			return
		}
		if err := s.Clear(r.Context(), owner, keyID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func ownedKey(w http.ResponseWriter, r *http.Request, owners Owners) (string, string, bool) {
	owner, ok := owners.OwnerOf(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign_in_required"})
		return "", "", false
	}
	keyID := r.PathValue("id")
	owns, err := owners.Owns(r, keyID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return "", "", false
	}
	if !owns {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown_key"})
		return "", "", false
	}
	return owner, keyID, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
