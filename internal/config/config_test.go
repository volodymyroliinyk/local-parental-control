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
	cfg := map[string]any{"users": map[string]any{current.Username: map[string]any{"daily_device_minutes": 120, "allowed_from": "08:00", "allowed_until": "20:00", "applications": []any{map[string]any{"id": "browser", "name": "Browser", "executables": []string{"/usr/bin/browser"}, "daily_minutes": 30}}}}}
	path := writeConfig(t, cfg)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	userConfig := got.Users[current.Username]
	if got.PollIntervalSeconds != 2 || got.TerminationGraceSeconds != 15 || got.Timezone != "Local" || userConfig.ContinuousUseMinutes != 60 || userConfig.BreakMinutes != 10 {
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
	c := Config{Timezone: "Local", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]UserConfig{current.Username: {DailyDeviceMinutes: 60, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "08:00", AllowedUntil: "20:00", Applications: []Application{
		{ID: "a", Name: "A", Executables: []string{"/usr/bin/x"}, DailyMinutes: 1},
		{ID: "b", Name: "B", Executables: []string{"/usr/bin/x"}, DailyMinutes: 1},
	}}}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected duplicate executable error")
	}
}

func TestValidateUniqueUserIDsRejectsAliases(t *testing.T) {
	lookup := func(username string) (*user.User, error) {
		return &user.User{Username: username, Uid: "1000"}, nil
	}
	err := validateUniqueUserIDs([]string{"alias-a", "alias-b"}, lookup)
	if err == nil || !strings.Contains(err.Error(), `users "alias-a" and "alias-b" resolve to the same numeric UID 1000`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRejectsMalformedAndTrailingJSON(t *testing.T) {
	current := currentUser(t)
	valid := `{"users":{"` + current.Username + `":{"daily_device_minutes":120,"allowed_from":"08:00","allowed_until":"20:00","applications":[{"id":"app","name":"App","executables":["/usr/bin/app"],"daily_minutes":1}]}}}`
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
		return Config{Timezone: "UTC", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]UserConfig{username: {DailyDeviceMinutes: 120, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "08:00", AllowedUntil: "20:00", Applications: []Application{validApp}}}}
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
		"device limit too small":     func(c *Config) { u := c.Users[username]; u.DailyDeviceMinutes = 0; c.Users[username] = u },
		"device limit too large":     func(c *Config) { u := c.Users[username]; u.DailyDeviceMinutes = 1441; c.Users[username] = u },
		"continuous limit too small": func(c *Config) { u := c.Users[username]; u.ContinuousUseMinutes = -1; c.Users[username] = u },
		"continuous limit too large": func(c *Config) { u := c.Users[username]; u.ContinuousUseMinutes = 1441; c.Users[username] = u },
		"break too small":            func(c *Config) { u := c.Users[username]; u.BreakMinutes = -1; c.Users[username] = u },
		"break too large":            func(c *Config) { u := c.Users[username]; u.BreakMinutes = 1441; c.Users[username] = u },
		"bad allowed from":           func(c *Config) { u := c.Users[username]; u.AllowedFrom = "8:00"; c.Users[username] = u },
		"bad allowed until":          func(c *Config) { u := c.Users[username]; u.AllowedUntil = "24:00"; c.Users[username] = u },
		"empty window":               func(c *Config) { u := c.Users[username]; u.AllowedUntil = u.AllowedFrom; c.Users[username] = u },
		"reversed window": func(c *Config) {
			u := c.Users[username]
			u.AllowedFrom, u.AllowedUntil = "20:00", "08:00"
			c.Users[username] = u
		},
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

func TestValidateExplainsEmptyApplicationExecutableRecovery(t *testing.T) {
	username := currentUser(t).Username
	cfg := Config{Timezone: "UTC", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]UserConfig{
		username: {
			DailyDeviceMinutes:   120,
			ContinuousUseMinutes: 60,
			BreakMinutes:         10,
			AllowedFrom:          "08:00",
			AllowedUntil:         "20:00",
			Applications: []Application{{
				ID: "firefox", Name: "Firefox", DailyMinutes: 30,
			}},
		},
	}}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "remove this application rule") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateAllowsDeviceOnlyConfiguration(t *testing.T) {
	username := currentUser(t).Username
	cfg := Config{Timezone: "UTC", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]UserConfig{
		username: {DailyDeviceMinutes: 120, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "08:00", AllowedUntil: "20:00"},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAcceptsBoundariesAndHelpers(t *testing.T) {
	username := currentUser(t).Username
	cfg := Config{Timezone: "UTC", PollIntervalSeconds: 1, TerminationGraceSeconds: 60, Users: map[string]UserConfig{username: {DailyDeviceMinutes: 1440, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "00:00", AllowedUntil: "23:59", Applications: []Application{
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
	userConfig := cfg.Users[username]
	if !userConfig.AllowedAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) || userConfig.AllowedAt(time.Date(2026, 1, 1, 23, 59, 0, 0, time.UTC)) {
		t.Fatal("allowed interval boundaries are not half-open")
	}
}

func TestValidateRejectsCleanedDuplicateExecutable(t *testing.T) {
	username := currentUser(t).Username
	cfg := Config{Timezone: "UTC", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]UserConfig{username: {DailyDeviceMinutes: 10, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "08:00", AllowedUntil: "20:00", Applications: []Application{
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

func TestValidateExecutablesNormalizesSnapRevisionAndMatchesRefresh(t *testing.T) {
	username := currentUser(t).Username
	snapRoot := t.TempDir()
	revisionA := filepath.Join(snapRoot, "firefox", "100")
	revisionB := filepath.Join(snapRoot, "firefox", "101")
	relative := filepath.Join("usr", "lib", "firefox", "firefox")
	for _, revision := range []string{revisionA, revisionB} {
		path := filepath.Join(revision, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append([]byte{0x7f, 'E', 'L', 'F'}, make([]byte, 12)...), 0755); err != nil {
			t.Fatal(err)
		}
	}
	current := filepath.Join(snapRoot, "firefox", "current")
	if err := os.Symlink("100", current); err != nil {
		t.Fatal(err)
	}
	configuredRevision := filepath.Join(revisionA, relative)
	cfg := Config{Users: map[string]UserConfig{username: {Applications: []Application{{ID: "firefox", Executables: []string{configuredRevision}}}}}}
	info, err := os.Stat(configuredRevision)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.validateExecutablesAt(info.Sys().(*syscall.Stat_t).Uid, snapRoot); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(current, relative)
	if got := cfg.Users[username].Applications[0].Executables[0]; got != stable {
		t.Fatalf("normalized executable = %q, want %q", got, stable)
	}
	if err := os.Remove(current); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("101", current); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(stable)
	if err != nil || resolved != filepath.Join(revisionB, relative) {
		t.Fatalf("stable executable after refresh = %q, %v", resolved, err)
	}
}

func TestExecutableMatchesSnapRevisions(t *testing.T) {
	configured := "/snap/firefox/current/usr/lib/firefox/firefox"
	for _, running := range []string{
		"/snap/firefox/100/usr/lib/firefox/firefox",
		"/snap/firefox/101/usr/lib/firefox/firefox",
		"/snap/firefox/x2/usr/lib/firefox/firefox",
	} {
		if !ExecutableMatches(configured, running) {
			t.Fatalf("configured %q did not match %q", configured, running)
		}
	}
	for _, running := range []string{
		"/snap/firefox/not-a-revision/usr/lib/firefox/firefox",
		"/snap/chromium/101/usr/lib/firefox/firefox",
		"/snap/firefox/101/usr/lib/firefox/helper",
	} {
		if ExecutableMatches(configured, running) {
			t.Fatalf("configured %q unexpectedly matched %q", configured, running)
		}
	}
}

func TestValidateExecutablesRejectsScriptAndSnapLauncher(t *testing.T) {
	username := currentUser(t).Username
	owner := uint32(os.Geteuid())
	script := filepath.Join(t.TempDir(), "browser")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec /usr/bin/true\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Users: map[string]UserConfig{username: {Applications: []Application{{ID: "script", Executables: []string{script}}}}}}
	if err := cfg.validateExecutables(owner); err == nil || !strings.Contains(err.Error(), "not a native ELF executable") {
		t.Fatalf("unexpected script validation error: %v", err)
	}

	info, err := os.Stat("/usr/bin/snap")
	if err != nil {
		t.Skipf("snap launcher is not installed: %v", err)
	}
	owner = info.Sys().(*syscall.Stat_t).Uid
	cfg.Users[username] = UserConfig{Applications: []Application{{ID: "snap", Executables: []string{"/usr/bin/snap"}}}}
	if err := cfg.validateExecutables(owner); err == nil || !strings.Contains(err.Error(), "Snap applications are not supported") {
		t.Fatalf("unexpected Snap validation error: %v", err)
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
