package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type Config struct {
	Sites []Site `json:"sites"`
}

type Site struct {
	Domain    string     `json:"domain"`
	Enabled   bool       `json:"enabled"`
	Schedules []Schedule `json:"schedules"`
}

type Schedule struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

func Default() Config {
	return Config{Sites: []Site{}}
}

func (c Config) Validate() (Config, error) {
	seen := make(map[string]struct{}, len(c.Sites))
	sites := make([]Site, 0, len(c.Sites))
	for _, site := range c.Sites {
		domain, err := NormalizeDomain(site.Domain)
		if err != nil {
			return Config{}, err
		}
		if _, ok := seen[domain]; ok {
			return Config{}, fmt.Errorf("duplicate domain %q", domain)
		}
		seen[domain] = struct{}{}
		schedules := make([]Schedule, 0, len(site.Schedules))
		scheduleSeen := make(map[Schedule]struct{}, len(site.Schedules))
		for _, schedule := range site.Schedules {
			start, err := parseClock(schedule.Start)
			if err != nil {
				return Config{}, fmt.Errorf("%s start time: %w", domain, err)
			}
			end, err := parseClock(schedule.End)
			if err != nil {
				return Config{}, fmt.Errorf("%s end time: %w", domain, err)
			}
			if start == end {
				return Config{}, fmt.Errorf("%s start and end times must be different", domain)
			}
			if _, ok := scheduleSeen[schedule]; !ok {
				scheduleSeen[schedule] = struct{}{}
				schedules = append(schedules, schedule)
			}
		}
		sort.Slice(schedules, func(i, j int) bool {
			if schedules[i].Start == schedules[j].Start {
				return schedules[i].End < schedules[j].End
			}
			return schedules[i].Start < schedules[j].Start
		})
		sites = append(sites, Site{Domain: domain, Enabled: site.Enabled, Schedules: schedules})
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].Domain < sites[j].Domain })
	c.Sites = sites
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

func (c Config) ActiveDomainsAt(now time.Time) []string {
	minute := now.Hour()*60 + now.Minute()
	var domains []string
	for _, site := range c.Sites {
		if !site.Enabled {
			continue
		}
		if len(site.Schedules) == 0 {
			domains = append(domains, site.Domain)
			continue
		}
		for _, schedule := range site.Schedules {
			start, err1 := parseClock(schedule.Start)
			end, err2 := parseClock(schedule.End)
			if err1 == nil && err2 == nil && start != end && minuteInRange(minute, start, end) {
				domains = append(domains, site.Domain)
				break
			}
		}
	}
	return domains
}

func (c Config) ActiveAt(now time.Time) bool {
	return len(c.ActiveDomainsAt(now)) > 0
}

func minuteInRange(minute, start, end int) bool {
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

// IsAtLeastAsStrictAs reports whether c blocks every domain at every minute
// that previous blocks. New domains and additional blocked time are stricter.
func (c Config) IsAtLeastAsStrictAs(previous Config) bool {
	current := blockedMinutes(c)
	before := blockedMinutes(previous)
	for domain, minutes := range before {
		for minute, blocked := range minutes {
			if blocked && !current[domain][minute] {
				return false
			}
		}
	}
	return true
}

func blockedMinutes(c Config) map[string][1440]bool {
	result := make(map[string][1440]bool, len(c.Sites))
	for _, site := range c.Sites {
		var minutes [1440]bool
		if site.Enabled {
			if len(site.Schedules) == 0 {
				for minute := range minutes {
					minutes[minute] = true
				}
			}
			for _, schedule := range site.Schedules {
				start, err1 := parseClock(schedule.Start)
				end, err2 := parseClock(schedule.End)
				if err1 != nil || err2 != nil || start == end {
					continue
				}
				for minute := 0; minute < len(minutes); minute++ {
					minutes[minute] = minutes[minute] || minuteInRange(minute, start, end)
				}
			}
		}
		result[site.Domain] = minutes
	}
	return result
}

func parseClock(value string) (int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return 0, fmt.Errorf("must use 24-hour HH:MM format")
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func ParseDomains(text string) ([]string, error) {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ','
	})
	domains := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		domain, err := NormalizeDomain(part)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[domain]; !ok {
			seen[domain] = struct{}{}
			domains = append(domains, domain)
		}
	}
	sort.Strings(domains)
	return domains, nil
}

func IsLoopbackURL(raw string, port int) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "http" && u.Host == fmt.Sprintf("127.0.0.1:%d", port)
}
