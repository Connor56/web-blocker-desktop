package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func at(hour, minute int) time.Time {
	return time.Date(2026, 11, 7, hour, minute, 0, 0, time.Local)
}

func TestActiveAtNormalWindow(t *testing.T) {
	c := Config{Sites: []Site{{Domain: "example.com", Enabled: true, Schedules: []Schedule{{Start: "09:00", End: "17:00"}}}}}
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
	c := Config{Sites: []Site{{Domain: "example.com", Enabled: true, Schedules: []Schedule{{Start: "22:00", End: "06:00"}}}}}
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
	c := Config{Sites: []Site{
		{Domain: " Example.COM ", Enabled: true, Schedules: []Schedule{{Start: "09:00", End: "17:00"}}},
		{Domain: "a.example.com", Enabled: true, Schedules: []Schedule{{Start: "18:00", End: "19:00"}}},
	}}
	got, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.example.com", "example.com"}
	if gotDomains := []string{got.Sites[0].Domain, got.Sites[1].Domain}; !reflect.DeepEqual(gotDomains, want) {
		t.Fatalf("got %v, want %v", gotDomains, want)
	}
}

func TestRejectsInvalidDomains(t *testing.T) {
	for _, domain := range []string{"https://example.com", "*.example.com", "127.0.0.1", "localhost", "bad_domain.com", "example.com # injected"} {
		c := Config{Sites: []Site{{Domain: domain, Enabled: true, Schedules: []Schedule{{Start: "09:00", End: "17:00"}}}}}
		if _, err := c.Validate(); err == nil {
			t.Fatalf("expected %q to be rejected", domain)
		}
	}
}

func TestRejectsEqualStartAndEnd(t *testing.T) {
	c := Config{Sites: []Site{{Domain: "example.com", Enabled: true, Schedules: []Schedule{{Start: "09:00", End: "09:00"}}}}}
	if _, err := c.Validate(); err == nil {
		t.Fatal("expected equal start and end times to fail")
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.bin")
	want := Config{Sites: []Site{{Domain: "example.com", Enabled: true, Schedules: []Schedule{{Start: "10:00", End: "11:00"}}}}}
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
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("example.com")) || bytes.HasPrefix(data, []byte("{")) {
		t.Fatal("configuration was stored as readable JSON")
	}
}

func TestMultipleSchedulesAndDomains(t *testing.T) {
	c := Config{Sites: []Site{
		{Domain: "morning.example", Enabled: true, Schedules: []Schedule{{Start: "08:00", End: "09:00"}, {Start: "20:00", End: "22:00"}}},
		{Domain: "day.example", Enabled: true, Schedules: []Schedule{{Start: "09:00", End: "17:00"}}},
	}}
	if got := c.ActiveDomainsAt(at(8, 30)); !reflect.DeepEqual(got, []string{"morning.example"}) {
		t.Fatalf("08:30 got %v", got)
	}
	if got := c.ActiveDomainsAt(at(12, 0)); !reflect.DeepEqual(got, []string{"day.example"}) {
		t.Fatalf("12:00 got %v", got)
	}
}

func TestEnabledSiteWithoutSchedulesIsPermanent(t *testing.T) {
	c := Config{Sites: []Site{{Domain: "always.example", Enabled: true}}}
	validated, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	for _, when := range []time.Time{at(0, 0), at(12, 0), at(23, 59)} {
		if got := validated.ActiveDomainsAt(when); !reflect.DeepEqual(got, []string{"always.example"}) {
			t.Fatalf("%s got %v", when.Format("15:04"), got)
		}
	}
}

func TestStrictnessUsesBlockedMinuteSets(t *testing.T) {
	old := Config{Sites: []Site{{Domain: "example.com", Enabled: true, Schedules: []Schedule{{Start: "09:00", End: "17:00"}}}}}
	tighter := Config{Sites: []Site{{Domain: "example.com", Enabled: true, Schedules: []Schedule{{Start: "08:00", End: "18:00"}}}}}
	weaker := Config{Sites: []Site{{Domain: "example.com", Enabled: true, Schedules: []Schedule{{Start: "10:00", End: "17:00"}}}}}
	if !tighter.IsAtLeastAsStrictAs(old) {
		t.Fatal("wider block should be stricter")
	}
	if weaker.IsAtLeastAsStrictAs(old) {
		t.Fatal("shorter block should require challenge")
	}
	permanent := Config{Sites: []Site{{Domain: "example.com", Enabled: true}}}
	if !permanent.IsAtLeastAsStrictAs(old) {
		t.Fatal("removing the final schedule should tighten to permanent blocking")
	}
	if old.IsAtLeastAsStrictAs(permanent) {
		t.Fatal("limiting a permanent block should require challenge")
	}
}

func TestLoadsLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.bin")
	legacy := filepath.Join(dir, "config.json")
	if err := os.WriteFile(legacy, []byte(`{"enabled":true,"start":"09:00","end":"17:00","domains":["example.com"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || len(got.Sites) != 1 || got.Sites[0].Domain != "example.com" {
		t.Fatalf("legacy load got %#v, %v", got, err)
	}
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy file still exists: %v", err)
	}
}

func TestTamperedPrimaryFallsBackToAuthenticatedBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.bin")
	first := Config{Sites: []Site{{Domain: "first.example", Enabled: true, Schedules: []Schedule{{Start: "09:00", End: "17:00"}}}}}
	second := Config{Sites: []Site{{Domain: "second.example", Enabled: true, Schedules: []Schedule{{Start: "09:00", End: "17:00"}}}}}
	if err := Save(path, first); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, second); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sites) != 1 || got.Sites[0].Domain != "first.example" {
		t.Fatalf("did not recover backup: %#v", got)
	}
}

func TestMissingProtectedStorageIsNotAnEmptyConfig(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "config.bin")); err == nil {
		t.Fatal("missing protected storage must fail closed")
	}
}
