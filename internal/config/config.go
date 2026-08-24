package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const DefaultPath = "/etc/local-parental-control/config.json"
const maxConfigSize = 1 << 20

type Config struct {
	Timezone                string                `json:"timezone"`
	PollIntervalSeconds     int                   `json:"poll_interval_seconds"`
	TerminationGraceSeconds int                   `json:"termination_grace_seconds"`
	Users                   map[string]UserConfig `json:"users"`
}

type UserConfig struct {
	DailyDeviceMinutes   int           `json:"daily_device_minutes"`
	ContinuousUseMinutes int           `json:"continuous_use_minutes"`
	BreakMinutes         int           `json:"break_minutes"`
	AllowedFrom          string        `json:"allowed_from"`
	AllowedUntil         string        `json:"allowed_until"`
	Applications         []Application `json:"applications"`
}

type Application struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Executables  []string `json:"executables"`
	DailyMinutes int      `json:"daily_minutes"`
}

func Load(path string) (*Config, error) {
	return load(path, false)
}

// LoadSecure loads a root-owned production configuration and validates that
// every executable has a stable, system-owned identity.
func LoadSecure(path string) (*Config, error) {
	if err := validateSecureFile(path, 0, 0600); err != nil {
		return nil, err
	}
	return load(path, true)
}

func load(path string, validateExecutables bool) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxConfigSize {
		return nil, fmt.Errorf("configuration exceeds %d bytes", maxConfigSize)
	}
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
	if validateExecutables {
		if err := cfg.validateExecutables(0); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

func validateSecureFile(path string, expectedUID uint32, expectedMode os.FileMode) error {
	parent := filepath.Dir(path)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect configuration directory: %w", err)
	}
	parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || parentStat.Uid != expectedUID || !parentInfo.IsDir() || parentInfo.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("configuration directory %s must be owned by UID %d and not writable by group or others", parent, expectedUID)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != expectedUID || !info.Mode().IsRegular() || info.Mode().Perm() != expectedMode.Perm() {
		return fmt.Errorf("configuration %s must be a regular UID %d-owned file with mode %04o", path, expectedUID, expectedMode.Perm())
	}
	return nil
}

func (c *Config) validateExecutables(expectedUID uint32) error {
	for username, userConfig := range c.Users {
		seen := make(map[string]bool)
		for appIndex := range userConfig.Applications {
			app := &userConfig.Applications[appIndex]
			for pathIndex, executable := range app.Executables {
				resolved, err := filepath.EvalSymlinks(executable)
				if err != nil {
					return fmt.Errorf("application %q executable %q: %w", app.ID, executable, err)
				}
				info, err := os.Stat(resolved)
				if err != nil {
					return fmt.Errorf("application %q executable %q: %w", app.ID, resolved, err)
				}
				stat, ok := info.Sys().(*syscall.Stat_t)
				if !ok || stat.Uid != expectedUID || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
					return fmt.Errorf("application %q executable %q must be a UID %d-owned executable regular file not writable by group or others", app.ID, resolved, expectedUID)
				}
				if filepath.Clean(resolved) == "/usr/bin/snap" {
					return fmt.Errorf("application %q executable %q resolves to the shared Snap launcher /usr/bin/snap; Snap applications are not supported", app.ID, executable)
				}
				isELF, err := hasELFHeader(resolved)
				if err != nil {
					return fmt.Errorf("application %q executable %q: %w", app.ID, resolved, err)
				}
				if !isELF {
					return fmt.Errorf("application %q executable %q is not a native ELF executable; scripts and application launchers cannot be matched through /proc/PID/exe", app.ID, executable)
				}
				resolved = filepath.Clean(resolved)
				if seen[resolved] {
					return fmt.Errorf("resolved executable %q appears in multiple rules for user %q", resolved, username)
				}
				seen[resolved] = true
				app.Executables[pathIndex] = resolved
			}
		}
		c.Users[username] = userConfig
	}
	return nil
}

func hasELFHeader(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	header := make([]byte, 4)
	if _, err := io.ReadFull(f, header); err != nil {
		return false, err
	}
	return bytes.Equal(header, []byte{0x7f, 'E', 'L', 'F'}), nil
}

func (c *Config) defaults() {
	if c.Timezone == "" {
		c.Timezone = "Local"
	}
	if c.PollIntervalSeconds == 0 {
		c.PollIntervalSeconds = 2
	}
	if c.TerminationGraceSeconds == 0 {
		c.TerminationGraceSeconds = 15
	}
	for username, userConfig := range c.Users {
		if userConfig.ContinuousUseMinutes == 0 {
			userConfig.ContinuousUseMinutes = 60
		}
		if userConfig.BreakMinutes == 0 {
			userConfig.BreakMinutes = 10
		}
		c.Users[username] = userConfig
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
		if uc.DailyDeviceMinutes < 1 || uc.DailyDeviceMinutes > 1440 {
			return fmt.Errorf("users.%s.daily_device_minutes must be between 1 and 1440", username)
		}
		if uc.ContinuousUseMinutes < 1 || uc.ContinuousUseMinutes > 1440 {
			return fmt.Errorf("users.%s.continuous_use_minutes must be between 1 and 1440", username)
		}
		if uc.BreakMinutes < 1 || uc.BreakMinutes > 1440 {
			return fmt.Errorf("users.%s.break_minutes must be between 1 and 1440", username)
		}
		from, err := parseClock(uc.AllowedFrom)
		if err != nil {
			return fmt.Errorf("users.%s.allowed_from: %w", username, err)
		}
		until, err := parseClock(uc.AllowedUntil)
		if err != nil {
			return fmt.Errorf("users.%s.allowed_until: %w", username, err)
		}
		if from >= until {
			return fmt.Errorf("users.%s allowed_from must be earlier than allowed_until", username)
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

// AllowedAt reports whether local wall-clock time is within the configured
// half-open interval [allowed_from, allowed_until).
func (u UserConfig) AllowedAt(t time.Time) bool {
	from, _ := parseClock(u.AllowedFrom)
	until, _ := parseClock(u.AllowedUntil)
	minute := t.Hour()*60 + t.Minute()
	return minute >= from && minute < until
}

func parseClock(value string) (int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil || len(value) != 5 {
		return 0, errors.New("must use HH:MM in 24-hour time")
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func (c *Config) ApplicationCount() int {
	n := 0
	for _, u := range c.Users {
		n += len(u.Applications)
	}
	return n
}
func (c *Config) Location() *time.Location { loc, _ := time.LoadLocation(c.Timezone); return loc }
