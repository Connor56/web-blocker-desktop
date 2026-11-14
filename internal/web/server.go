package web

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"annoying-web-blocker-desktop/internal/config"
)

const (
	Port    = 47823
	Address = "127.0.0.1:47823"
	BaseURL = "http://127.0.0.1:47823"
)

//go:embed static/*
var assets embed.FS

type Handler struct {
	Token                    string
	ConfigPath               string
	Now                      func() time.Time
	OnChange                 func(config.Config) error
	Save                     func(config.Config) error
	Status                   func() Status
	ChallengeDuration        time.Duration
	ClickInterval            time.Duration
	ProtectedStorageRequired bool
}

type Status struct {
	Active  bool      `json:"active"`
	Checked time.Time `json:"checked"`
	Error   string    `json:"error,omitempty"`
}

type response struct {
	Status *Status       `json:"status,omitempty"`
	Error  string        `json:"error,omitempty"`
	Config config.Config `json:"config"`
	Active bool          `json:"active"`
}

type challengeState struct {
	mu          sync.Mutex
	id          string
	started     time.Time
	lastClick   time.Time
	unlockToken string
	unlockUntil time.Time
}

func (h Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	challenge := &challengeState{}
	static, _ := fs.Sub(assets, "static")
	mux.Handle("/", http.FileServer(http.FS(static)))
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		h.handleConfig(w, r, challenge)
	})
	mux.HandleFunc("/api/unlock/start", func(w http.ResponseWriter, r *http.Request) {
		h.handleUnlockStart(w, r, challenge)
	})
	mux.HandleFunc("/api/unlock/click", func(w http.ResponseWriter, r *http.Request) {
		h.handleUnlockClick(w, r, challenge)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if !h.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if h.Status == nil {
			writeJSON(w, Status{}, http.StatusOK)
			return
		}
		writeJSON(w, h.Status(), http.StatusOK)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != Address {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
}

func (h Handler) handleConfig(w http.ResponseWriter, r *http.Request, challenge *challengeState) {
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.getConfig(w)
	case http.MethodPut:
		if r.Header.Get("Origin") != BaseURL {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}
		mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
		if mediaType != "application/json" {
			http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		h.putConfig(w, r, challenge)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h Handler) getConfig(w http.ResponseWriter) {
	c, err := config.Load(h.ConfigPath)
	if err != nil {
		c = config.Default()
	}
	var configError string
	if err != nil {
		configError = "Protected configuration could not be read. The service will keep its last valid enforcement state: " + err.Error()
	}
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	result := response{Config: c, Active: c.ActiveAt(now), Error: configError}
	if h.Status != nil {
		status := h.Status()
		result.Status = &status
	}
	writeJSON(w, result, http.StatusOK)
}

func (h Handler) putConfig(w http.ResponseWriter, r *http.Request, challenge *challengeState) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	var c config.Config
	if err := decoder.Decode(&c); err != nil {
		writeError(w, fmt.Errorf("invalid request: %w", err), http.StatusBadRequest)
		return
	}
	if err := ensureEOF(decoder); err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	validated, err := c.Validate()
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	current, loadErr := config.Load(h.ConfigPath)
	if loadErr != nil && (h.ProtectedStorageRequired || config.StoragePresent(h.ConfigPath)) {
		writeError(w, fmt.Errorf("refusing to replace unavailable protected configuration: %w", loadErr), http.StatusInternalServerError)
		return
	}
	if loadErr == nil && !validated.IsAtLeastAsStrictAs(current) {
		if !challenge.consumeUnlock(r.Header.Get("X-Change-Unlock"), time.Now()) {
			writeJSON(w, map[string]any{
				"error":             "This change weakens blocking and requires the moving-dot challenge.",
				"challengeRequired": true,
			}, http.StatusLocked)
			return
		}
	}
	save := h.Save
	if save == nil {
		save = func(c config.Config) error {
			if err := config.Save(h.ConfigPath, c); err != nil {
				return err
			}
			if h.OnChange != nil {
				return h.OnChange(c)
			}
			return nil
		}
	}
	if err := save(validated); err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	writeJSON(w, response{Config: validated, Active: validated.ActiveAt(now)}, http.StatusOK)
}

func (h Handler) challengeSettings() (time.Duration, time.Duration) {
	duration := h.ChallengeDuration
	if duration <= 0 {
		duration = 120 * time.Second
	}
	interval := h.ClickInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return duration, interval
}

func (h Handler) handleUnlockStart(w http.ResponseWriter, r *http.Request, state *challengeState) {
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Origin") != BaseURL {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	duration, interval := h.challengeSettings()
	now := time.Now()
	state.mu.Lock()
	state.id = randomToken()
	state.started = now
	state.lastClick = now
	state.unlockToken = ""
	id := state.id
	state.mu.Unlock()
	writeJSON(w, map[string]any{
		"id": id, "remainingMs": duration.Milliseconds(), "clickWithinMs": interval.Milliseconds(),
	}, http.StatusOK)
}

func (h Handler) handleUnlockClick(w http.ResponseWriter, r *http.Request, state *challengeState) {
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Origin") != BaseURL {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	var request struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&request); err != nil {
		writeError(w, errors.New("invalid challenge request"), http.StatusBadRequest)
		return
	}
	duration, interval := h.challengeSettings()
	now := time.Now()
	state.mu.Lock()
	defer state.mu.Unlock()
	if request.ID == "" || subtle.ConstantTimeCompare([]byte(request.ID), []byte(state.id)) != 1 {
		writeError(w, errors.New("challenge is no longer active"), http.StatusConflict)
		return
	}
	if now.Sub(state.lastClick) > interval {
		state.id = ""
		writeError(w, errors.New("you did not click in time; restart the challenge"), http.StatusConflict)
		return
	}
	state.lastClick = now
	remaining := duration - now.Sub(state.started)
	if remaining > 0 {
		writeJSON(w, map[string]any{"remainingMs": remaining.Milliseconds(), "clickWithinMs": interval.Milliseconds()}, http.StatusOK)
		return
	}
	state.unlockToken = randomToken()
	state.unlockUntil = now.Add(time.Minute)
	state.id = ""
	writeJSON(w, map[string]any{"remainingMs": 0, "unlockToken": state.unlockToken}, http.StatusOK)
}

func (s *challengeState) consumeUnlock(token string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" || now.After(s.unlockUntil) ||
		subtle.ConstantTimeCompare([]byte(token), []byte(s.unlockToken)) != 1 {
		return false
	}
	s.unlockToken = ""
	return true
}

func randomToken() string {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func (h Handler) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, prefix) || h.Token == "" {
		return false
	}
	supplied := strings.TrimPrefix(value, prefix)
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(h.Token)) == 1
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("request must contain exactly one JSON object")
	}
	return fmt.Errorf("invalid request: %w", err)
}

func writeJSON(w http.ResponseWriter, value any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, err error, status int) {
	writeJSON(w, map[string]string{"error": err.Error()}, status)
}
