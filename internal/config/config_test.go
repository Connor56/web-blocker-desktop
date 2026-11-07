package config

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func at(hour, minute int) time.Time {
	return time.Date(2026, 11, 7, hour, minute, 0, 0, time.Local)
}

func TestActiveAtNormalWindow(t *testing.T) {
	c := Config{Enabled: true, Start: "09:00", End: "17:00", Domains: []string{"example.com"}}
	for _, test := range []struct {
		hour, minute int
		want         bool
	}{
		{8, 59, false}, {9, 0, true}, {16, 59, true}, {17, 0, false},
	} {
		if got := c.ActiveAt(at(test.hour, test.minute)); got != test.want {
			t.Fatalf("%02d:%02d: got %v, want %v", test.hour, test.minute, got, test.want)
		}
	}
}

func TestActiveAtOvernightWindow(t *testing.T) {
	c := Config{Enabled: true, Start: "22:00", End: "06:00", Domains: []string{"example.com"}}
	for _, test := range []struct {
		hour, minute int
		want         bool
	}{
		{21, 59, false}, {22, 0, true}, {2, 0, true}, {5, 59, true}, {6, 0, false},
	} {
		if got := c.ActiveAt(at(test.hour, test.minute)); got != test.want {
			t.Fatalf("%02d:%02d: got %v, want %v", test.hour, test.minute, got, test.want)
		}
	}
}

func TestValidateNormalizesDomains(t *testing.T) {
	c := Config{Start: "09:00", End: "17:00", Domains: []string{" Example.COM ", "a.example.com", "example.com"}}
	got, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.example.com", "example.com"}
	if !reflect.DeepEqual(got.Domains, want) {
		t.Fatalf("got %v, want %v", got.Domains, want)
	}
}

func TestRejectsInvalidDomains(t *testing.T) {
	for _, domain := range []string{"https://example.com", "*.example.com", "127.0.0.1", "localhost", "bad_domain.com", "example.com # injected"} {
		c := Config{Start: "09:00", End: "17:00", Domains: []string{domain}}
		if _, err := c.Validate(); err == nil {
			t.Fatalf("expected %q to be rejected", domain)
		}
	}
}

func TestRejectsEqualStartAndEnd(t *testing.T) {
	c := Config{Enabled: true, Start: "09:00", End: "09:00", Domains: []string{"example.com"}}
	if _, err := c.Validate(); err == nil {
		t.Fatal("expected equal start and end times to fail")
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := Config{Enabled: true, Start: "10:00", End: "11:00", Domains: []string{"example.com"}}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
