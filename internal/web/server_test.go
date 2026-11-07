package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"annoying-web-blocker-desktop/internal/config"
)

func request(t *testing.T, handler http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = Address
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", BaseURL)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestAPIRequiresAuthorization(t *testing.T) {
	h := Handler{Token: "secret", ConfigPath: filepath.Join(t.TempDir(), "config.json")}.Routes()
	if got := request(t, h, http.MethodGet, "/api/config", "", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("got status %d", got)
	}
}

func TestAPISavesValidatedConfigAndCallsOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var changed config.Config
	h := Handler{
		Token:      "secret",
		ConfigPath: path,
		Now:        func() time.Time { return time.Date(2026, 1, 1, 10, 0, 0, 0, time.Local) },
		OnChange: func(c config.Config) error {
			changed = c
			return nil
		},
	}.Routes()
	body := `{"enabled":true,"start":"09:00","end":"17:00","domains":["Example.com"]}`
	recorder := request(t, h, http.MethodPut, "/api/config", "secret", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(changed.Domains) != 1 || changed.Domains[0] != "example.com" {
		t.Fatalf("unexpected callback config: %#v", changed)
	}
	loaded, err := config.Load(path)
	if err != nil || len(loaded.Domains) != 1 || loaded.Domains[0] != "example.com" {
		t.Fatalf("unexpected saved config: %#v, %v", loaded, err)
	}
}

func TestAPIRejectsInvalidOriginAndDomain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	handler := Handler{Token: "secret", ConfigPath: path}.Routes()

	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{}`))
	req.Host = Address
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("invalid origin got status %d", recorder.Code)
	}

	body := `{"enabled":true,"start":"09:00","end":"17:00","domains":["https://example.com"]}`
	if got := request(t, handler, http.MethodPut, "/api/config", "secret", body).Code; got != http.StatusBadRequest {
		t.Fatalf("invalid domain got status %d", got)
	}
}

func TestRejectsDNSRebindingHost(t *testing.T) {
	handler := Handler{Token: "secret"}.Routes()
	req := httptest.NewRequest(http.MethodGet, "http://evil.example/api/config", nil)
	req.Host = "evil.example"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("got status %d", recorder.Code)
	}
}
