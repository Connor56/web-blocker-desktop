package daemon

import (
	"annoying-web-blocker-desktop/internal/config"
	"annoying-web-blocker-desktop/internal/web"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentSavesRemainOrdered(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	options := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	flushCount := 0
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, options, time.Hour, address, func() error {
			flushCount++
			if flushCount == 2 {
				close(entered)
				<-release
			}
			return nil
		})
	}()
	t.Cleanup(func() {
		unblock()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(6 * time.Second):
			t.Error("daemon did not stop")
		}
	})
	client := &http.Client{Timeout: 3 * time.Second}
	request := func(method, body string) error {
		req, err := http.NewRequest(method, "http://"+address+"/api/config", strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Host = web.Address
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Origin", web.BaseURL)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		bodyBytes, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("%d: %s", res.StatusCode, bodyBytes)
		}
		return nil
	}
	eventually(t, func() bool { return request("GET", "") == nil })
	now := time.Now()
	body := func(domain string) string {
		if domain == "second.example" {
			return fmt.Sprintf(`{"sites":[{"domain":"first.example","enabled":true,"schedules":[{"start":%q,"end":%q}]},{"domain":"second.example","enabled":true,"schedules":[{"start":%q,"end":%q}]}]}`,
				now.Add(-time.Hour).Format("15:04"), now.Add(time.Hour).Format("15:04"),
				now.Add(-time.Hour).Format("15:04"), now.Add(time.Hour).Format("15:04"))
		}
		return fmt.Sprintf(`{"sites":[{"domain":%q,"enabled":true,"schedules":[{"start":%q,"end":%q}]}]}`, domain, now.Add(-time.Hour).Format("15:04"), now.Add(time.Hour).Format("15:04"))
	}
	first := make(chan error, 1)
	go func() { first <- request("PUT", body("first.example")) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first save did not reach enforcement")
	}
	second := make(chan error, 1)
	go func() { second <- request("PUT", body("second.example")) }()
	// The first save holds the coordinator while applying its settings. A second
	// request must not persist its settings until that application has finished.
	select {
	case err := <-second:
		t.Fatalf("second save finished before first: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	c, err := config.Load(options.ConfigPath)
	if err != nil || len(c.Sites) != 1 || c.Sites[0].Domain != "first.example" {
		t.Fatalf("settings changed during first save: %+v, %v", c, err)
	}
	unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	c, err = config.Load(options.ConfigPath)
	if err != nil || len(c.Sites) != 2 || c.Sites[1].Domain != "second.example" {
		t.Fatalf("unexpected final settings: %+v, %v", c, err)
	}
	data, err := os.ReadFile(options.HostsPath)
	if err != nil || !strings.Contains(string(data), "second.example") || !strings.Contains(string(data), "first.example") {
		t.Fatalf("hosts do not match latest settings: %s, %v", data, err)
	}
}

func fixture(t *testing.T) Options {
	t.Helper()
	dir := t.TempDir()
	options := Options{ConfigPath: filepath.Join(dir, "config.json"), TokenPath: filepath.Join(dir, "token"), HostsPath: filepath.Join(dir, "hosts")}
	for path, data := range map[string]string{options.TokenPath: "secret", options.HostsPath: "127.0.0.1 localhost\n"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.Initialize(options.ConfigPath); err != nil {
		t.Fatal(err)
	}
	return options
}
func launch(t *testing.T, options Options, address string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, options, 20*time.Millisecond, address, func() error { return nil }) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(6 * time.Second):
			t.Error("daemon did not stop")
		}
	})
}
func eventually(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not reached")
}
func TestBusyUIPortDoesNotStopEnforcement(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	options := fixture(t)
	launch(t, options, listener.Addr().String())
	// Saving after startup exercises the periodic loop, rather than just the initial write.
	time.Sleep(50 * time.Millisecond)
	now := time.Now()
	c := config.Config{Sites: []config.Site{{Domain: "example.com", Enabled: true, Schedules: []config.Schedule{{Start: now.Add(-time.Hour).Format("15:04"), End: now.Add(time.Hour).Format("15:04")}}}}}
	if err := config.Save(options.ConfigPath, c); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		data, _ := os.ReadFile(options.HostsPath)
		return strings.Contains(string(data), "127.0.0.1 example.com")
	})
}
func TestMalformedProtectedConfigCannotBeReplacedThroughUI(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	options := fixture(t)
	if err := os.WriteFile(options.ConfigPath, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	launch(t, options, address)
	client := &http.Client{Timeout: time.Second}
	request := func(method, path, body string) (int, []byte) {
		req, _ := http.NewRequest(method, "http://"+address+path, strings.NewReader(body))
		req.Host = web.Address
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Origin", web.BaseURL)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			return 0, nil
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		return res.StatusCode, data
	}
	eventually(t, func() bool {
		code, data := request("GET", "/api/config", "")
		return code == 200 && strings.Contains(string(data), "Protected configuration could not be read")
	})
	now := time.Now()
	body := fmt.Sprintf(`{"sites":[{"domain":"example.com","enabled":true,"schedules":[{"start":%q,"end":%q}]}]}`, now.Add(-time.Hour).Format("15:04"), now.Add(time.Hour).Format("15:04"))
	if code, data := request("PUT", "/api/config", body); code != http.StatusInternalServerError {
		t.Fatalf("protected configuration was replaced: %d %s", code, data)
	}
}
