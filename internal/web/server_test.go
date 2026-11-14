package web

import (
	"encoding/json"
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
	body := `{"sites":[{"domain":"Example.com","enabled":true,"schedules":[{"start":"09:00","end":"17:00"}]}]}`
	recorder := request(t, h, http.MethodPut, "/api/config", "secret", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(changed.Sites) != 1 || changed.Sites[0].Domain != "example.com" {
		t.Fatalf("unexpected callback config: %#v", changed)
	}
	loaded, err := config.Load(path)
	if err != nil || len(loaded.Sites) != 1 || loaded.Sites[0].Domain != "example.com" {
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

	body := `{"sites":[{"domain":"https://example.com","enabled":true,"schedules":[{"start":"09:00","end":"17:00"}]}]}`
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

func TestWeakeningRequiresAndConsumesChallenge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	strong := config.Config{Sites: []config.Site{{
		Domain: "example.com", Enabled: true,
		Schedules: []config.Schedule{{Start: "09:00", End: "17:00"}},
	}}}
	if err := config.Save(path, strong); err != nil {
		t.Fatal(err)
	}
	handler := Handler{
		Token: "secret", ConfigPath: path,
		ChallengeDuration: 10 * time.Millisecond, ClickInterval: time.Second,
	}.Routes()
	weaker := `{"sites":[{"domain":"example.com","enabled":true,"schedules":[{"start":"10:00","end":"17:00"}]}]}`
	if got := request(t, handler, http.MethodPut, "/api/config", "secret", weaker).Code; got != http.StatusLocked {
		t.Fatalf("weakening without challenge got %d", got)
	}

	start := request(t, handler, http.MethodPost, "/api/unlock/start", "secret", `{}`)
	if start.Code != http.StatusOK {
		t.Fatalf("start challenge: %d %s", start.Code, start.Body.String())
	}
	var challenge struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(start.Body.Bytes(), &challenge); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	click := request(t, handler, http.MethodPost, "/api/unlock/click", "secret", `{"id":"`+challenge.ID+`"}`)
	var unlocked struct {
		Token string `json:"unlockToken"`
	}
	if err := json.Unmarshal(click.Body.Bytes(), &unlocked); err != nil || unlocked.Token == "" {
		t.Fatalf("complete challenge: %d %s (%v)", click.Code, click.Body.String(), err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(weaker))
	req.Host = Address
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Origin", BaseURL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Change-Unlock", unlocked.Token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("save after challenge: %d %s", recorder.Code, recorder.Body.String())
	}
	if got := request(t, handler, http.MethodPut, "/api/config", "secret", `{"sites":[]}`).Code; got != http.StatusLocked {
		t.Fatalf("unlock token was reusable, got %d", got)
	}
}

func TestDefaultChallengeRequiresTwoMinutesAndFiveSecondClicks(t *testing.T) {
	duration, interval := (Handler{}).challengeSettings()
	if duration != 120*time.Second || interval != 5*time.Second {
		t.Fatalf("got duration %v and interval %v", duration, interval)
	}
}
