package config

import (
	"annoying-web-blocker-desktop/internal/platformpaths"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var DefaultPath = filepath.Join(platformpaths.ApplicationDir, "config.json")

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type Config struct {
	Enabled bool     `json:"enabled"`
	Start   string   `json:"start"`
	End     string   `json:"end"`
	Domains []string `json:"domains"`
}

func Default() Config {
	return Config{Start: "09:00", End: "17:00", Domains: []string{}}
}

func (c Config) Validate() (Config, error) {
	start, err := parseClock(c.Start)
	if err != nil {
		return Config{}, fmt.Errorf("start time: %w", err)
	}
	end, err := parseClock(c.End)
	if err != nil {
		return Config{}, fmt.Errorf("end time: %w", err)
	}
	if start == end {
		return Config{}, errors.New("start and end times must be different")
	}

	seen := make(map[string]struct{}, len(c.Domains))
	domains := make([]string, 0, len(c.Domains))
	for _, raw := range c.Domains {
		domain, err := NormalizeDomain(raw)
		if err != nil {
			return Config{}, err
		}
		if _, ok := seen[domain]; !ok {
			seen[domain] = struct{}{}
			domains = append(domains, domain)
		}
	}
	sort.Strings(domains)
	c.Domains = domains
	return c, nil
}

func NormalizeDomain(raw string) (string, error) {
	domain := strings.ToLower(strings.TrimSpace(raw))
	if domain == "" {
		return "", errors.New("domain cannot be empty")
	}
	if strings.ContainsAny(domain, "/\\:#?@ \t\r\n") {
		return "", fmt.Errorf("%q is not an exact domain name", raw)
	}
	if net.ParseIP(domain) != nil {
		return "", fmt.Errorf("%q is an IP address, not a domain", raw)
	}
	if strings.HasPrefix(domain, "*.") {
		return "", errors.New("wildcards are not supported by the hosts file")
	}
	domain = strings.TrimSuffix(domain, ".")
	if len(domain) > 253 || !strings.Contains(domain, ".") {
		return "", fmt.Errorf("%q is not a valid domain name", raw)
	}
	for _, label := range strings.Split(domain, ".") {
		if !labelPattern.MatchString(label) {
			return "", fmt.Errorf("%q is not a valid domain name", raw)
		}
	}
	return domain, nil
}

func (c Config) ActiveAt(now time.Time) bool {
	if !c.Enabled || len(c.Domains) == 0 {
		return false
	}
	start, err1 := parseClock(c.Start)
	end, err2 := parseClock(c.End)
	if err1 != nil || err2 != nil || start == end {
		return false
	}
	minute := now.Hour()*60 + now.Minute()
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

func parseClock(value string) (int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return 0, fmt.Errorf("must use 24-hour HH:MM format")
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	return c.Validate()
}

func Save(path string, c Config) error {
	validated, err := c.Validate()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(validated, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func ParseDomains(text string) ([]string, error) {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ','
	})
	c := Config{Start: "00:00", End: "00:01", Domains: parts}
	validated, err := c.Validate()
	if err != nil {
		return nil, err
	}
	return validated.Domains, nil
}

func IsLoopbackURL(raw string, port int) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" && u.Host == fmt.Sprintf("127.0.0.1:%d", port)
}
