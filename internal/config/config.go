package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

const DefaultPath = "/etc/local-parental-control/config.json"

type Config struct {
	Timezone                string                `json:"timezone"`
	PollIntervalSeconds     int                   `json:"poll_interval_seconds"`
	TerminationGraceSeconds int                   `json:"termination_grace_seconds"`
	Users                   map[string]UserConfig `json:"users"`
}

type UserConfig struct {
	Applications []Application `json:"applications"`
}

type Application struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Executables  []string `json:"executables"`
	DailyMinutes int      `json:"daily_minutes"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("configuration contains multiple JSON values")
		}
		return nil, fmt.Errorf("trailing data in %s: %w", path, err)
	}
	cfg.defaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) defaults() {
	if c.Timezone == "" {
		c.Timezone = "Local"
	}
	if c.PollIntervalSeconds == 0 {
		c.PollIntervalSeconds = 2
	}
	if c.TerminationGraceSeconds == 0 {
		c.TerminationGraceSeconds = 3
	}
}

func (c *Config) Validate() error {
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("timezone: %w", err)
	}
	if c.PollIntervalSeconds < 1 || c.PollIntervalSeconds > 60 {
		return errors.New("poll_interval_seconds must be between 1 and 60")
	}
	if c.TerminationGraceSeconds < 1 || c.TerminationGraceSeconds > 60 {
		return errors.New("termination_grace_seconds must be between 1 and 60")
	}
	if len(c.Users) == 0 {
		return errors.New("at least one user is required")
	}
	for username, uc := range c.Users {
		if strings.TrimSpace(username) == "" {
			return errors.New("empty username")
		}
		if _, err := user.Lookup(username); err != nil {
			return fmt.Errorf("user %q does not exist: %w", username, err)
		}
		if len(uc.Applications) == 0 {
			return fmt.Errorf("user %q has no applications", username)
		}
		ids, paths := map[string]bool{}, map[string]bool{}
		for i, app := range uc.Applications {
			prefix := fmt.Sprintf("users.%s.applications[%d]", username, i)
			if app.ID == "" || strings.ContainsAny(app.ID, " /\\") {
				return fmt.Errorf("%s.id must be non-empty and contain no whitespace or slashes", prefix)
			}
			if ids[app.ID] {
				return fmt.Errorf("duplicate application id %q for user %q", app.ID, username)
			}
			ids[app.ID] = true
			if strings.TrimSpace(app.Name) == "" {
				return fmt.Errorf("%s.name is required", prefix)
			}
			if app.DailyMinutes < 1 || app.DailyMinutes > 1440 {
				return fmt.Errorf("%s.daily_minutes must be between 1 and 1440", prefix)
			}
			if len(app.Executables) == 0 {
				return fmt.Errorf("%s.executables is required", prefix)
			}
			for _, executable := range app.Executables {
				if !filepath.IsAbs(executable) {
					return fmt.Errorf("%s executable %q is not absolute", prefix, executable)
				}
				clean := filepath.Clean(executable)
				if paths[clean] {
					return fmt.Errorf("executable %q appears in multiple rules for user %q", clean, username)
				}
				paths[clean] = true
			}
		}
	}
	return nil
}

func (c *Config) ApplicationCount() int {
	n := 0
	for _, u := range c.Users {
		n += len(u.Applications)
	}
	return n
}
func (c *Config) Location() *time.Location { loc, _ := time.LoadLocation(c.Timezone); return loc }
