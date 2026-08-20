package config

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLoadAndDefaults(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"users": map[string]any{current.Username: map[string]any{"applications": []any{map[string]any{"id": "browser", "name": "Browser", "executables": []string{"/usr/bin/browser"}, "daily_minutes": 30}}}}}
	path := writeConfig(t, cfg)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.PollIntervalSeconds != 2 || got.TerminationGraceSeconds != 15 || got.Timezone != "Local" {
		t.Fatalf("defaults not applied: %+v", got)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	current, _ := user.Current()
	cfg := map[string]any{"unknown": true, "users": map[string]any{current.Username: map[string]any{"applications": []any{}}}}
	_, err := Load(writeConfig(t, cfg))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateRejectsDuplicateExecutable(t *testing.T) {
	current, _ := user.Current()
	c := Config{Timezone: "Local", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]UserConfig{current.Username: {Applications: []Application{
		{ID: "a", Name: "A", Executables: []string{"/usr/bin/x"}, DailyMinutes: 1},
		{ID: "b", Name: "B", Executables: []string{"/usr/bin/x"}, DailyMinutes: 1},
	}}}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected duplicate executable error")
	}
}

func TestLoadRejectsMalformedAndTrailingJSON(t *testing.T) {
	current := currentUser(t)
	valid := `{"users":{"` + current.Username + `":{"applications":[{"id":"app","name":"App","executables":["/usr/bin/app"],"daily_minutes":1}]}}}`
	tests := map[string]string{
		"malformed":       `{`,
		"multiple values": valid + `{}`,
		"trailing data":   valid + `not-json`,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected decode error")
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestValidateRejectsInvalidConfiguration(t *testing.T) {
	username := currentUser(t).Username
	validApp := Application{ID: "app", Name: "Application", Executables: []string{"/usr/bin/app"}, DailyMinutes: 10}
	valid := func() Config {
		return Config{Timezone: "UTC", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]UserConfig{username: {Applications: []Application{validApp}}}}
	}
	tests := map[string]func(*Config){
		"timezone":        func(c *Config) { c.Timezone = "Not/A_Real_Timezone" },
		"poll too small":  func(c *Config) { c.PollIntervalSeconds = 0 },
		"poll too large":  func(c *Config) { c.PollIntervalSeconds = 61 },
		"grace too small": func(c *Config) { c.TerminationGraceSeconds = 0 },
		"grace too large": func(c *Config) { c.TerminationGraceSeconds = 61 },
		"no users":        func(c *Config) { c.Users = nil },
		"empty username":  func(c *Config) { c.Users = map[string]UserConfig{" ": {Applications: []Application{validApp}}} },
		"unknown user": func(c *Config) {
			c.Users = map[string]UserConfig{"local-parental-control-user-that-does-not-exist": {Applications: []Application{validApp}}}
		},
		"no applications": func(c *Config) { c.Users[username] = UserConfig{} },
		"empty id": func(c *Config) {
			app := validApp
			app.ID = ""
			c.Users[username] = UserConfig{Applications: []Application{app}}
		},
		"invalid id": func(c *Config) {
			app := validApp
			app.ID = "bad id"
			c.Users[username] = UserConfig{Applications: []Application{app}}
		},
		"duplicate id": func(c *Config) { c.Users[username] = UserConfig{Applications: []Application{validApp, validApp}} },
		"empty name": func(c *Config) {
			app := validApp
			app.Name = " "
			c.Users[username] = UserConfig{Applications: []Application{app}}
		},
		"limit too small": func(c *Config) {
			app := validApp
			app.DailyMinutes = 0
			c.Users[username] = UserConfig{Applications: []Application{app}}
		},
		"limit too large": func(c *Config) {
			app := validApp
			app.DailyMinutes = 1441
			c.Users[username] = UserConfig{Applications: []Application{app}}
		},
		"no executables": func(c *Config) {
			app := validApp
			app.Executables = nil
			c.Users[username] = UserConfig{Applications: []Application{app}}
		},
		"relative executable": func(c *Config) {
			app := validApp
			app.Executables = []string{"bin/app"}
			c.Users[username] = UserConfig{Applications: []Application{app}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := valid()
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateAcceptsBoundariesAndHelpers(t *testing.T) {
	username := currentUser(t).Username
	cfg := Config{Timezone: "UTC", PollIntervalSeconds: 1, TerminationGraceSeconds: 60, Users: map[string]UserConfig{username: {Applications: []Application{
		{ID: "a", Name: "A", Executables: []string{"/usr/bin/a"}, DailyMinutes: 1},
		{ID: "b", Name: "B", Executables: []string{"/usr/bin/b"}, DailyMinutes: 1440},
	}}}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.ApplicationCount() != 2 {
		t.Fatalf("application count = %d", cfg.ApplicationCount())
	}
	if cfg.Location() != time.UTC {
		t.Fatalf("location = %s", cfg.Location())
	}
}

func TestValidateRejectsCleanedDuplicateExecutable(t *testing.T) {
	username := currentUser(t).Username
	cfg := Config{Timezone: "UTC", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]UserConfig{username: {Applications: []Application{
		{ID: "a", Name: "A", Executables: []string{"/usr/bin/app"}, DailyMinutes: 1},
		{ID: "b", Name: "B", Executables: []string{"/usr/bin/../bin/app"}, DailyMinutes: 1},
	}}}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "multiple rules") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSecureFileValidationRejectsUnsafeMetadata(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateSecureFile(path, uint32(os.Geteuid()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateSecureFile(path, uint32(os.Geteuid()), 0600); err == nil {
		t.Fatal("expected unsafe file mode to be rejected")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", path); err != nil {
		t.Fatal(err)
	}
	if err := validateSecureFile(path, uint32(os.Geteuid()), 0600); err == nil {
		t.Fatal("expected symlink to be rejected")
	}
}

func TestValidateExecutablesCanonicalizesSystemBinaryAndRejectsWritableFile(t *testing.T) {
	username := currentUser(t).Username
	cfg := Config{Users: map[string]UserConfig{username: {Applications: []Application{{ID: "true", Executables: []string{"/bin/true"}}}}}}
	info, err := os.Stat("/usr/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	owner := info.Sys().(*syscall.Stat_t).Uid
	if err := cfg.validateExecutables(owner); err != nil {
		t.Fatal(err)
	}
	if cfg.Users[username].Applications[0].Executables[0] != "/usr/bin/true" {
		t.Fatalf("executable was not canonicalized: %+v", cfg)
	}

	writable := filepath.Join(t.TempDir(), "program")
	if err := os.WriteFile(writable, []byte("program"), 0775); err != nil {
		t.Fatal(err)
	}
	cfg.Users[username] = UserConfig{Applications: []Application{{ID: "unsafe", Executables: []string{writable}}}}
	if err := cfg.validateExecutables(owner); err == nil {
		t.Fatal("expected non-root-owned executable to be rejected")
	}
}

func writeConfig(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func currentUser(t *testing.T) *user.User {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	return current
}
