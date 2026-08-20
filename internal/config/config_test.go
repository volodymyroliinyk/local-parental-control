package config

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"testing"
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
	if got.PollIntervalSeconds != 2 || got.TerminationGraceSeconds != 3 || got.Timezone != "Local" {
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
