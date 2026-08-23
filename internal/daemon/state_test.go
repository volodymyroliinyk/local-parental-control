package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateRoundTripAndDailyReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "usage.json")
	state := newState("2026-08-19")
	state.Users["child"] = map[string]int64{"vlc": 42}
	state.DeviceSeconds["child"] = 84
	if err := saveState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadState(path, "2026-08-19")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Users["child"]["vlc"] != 42 {
		t.Fatalf("unexpected state: %+v", loaded)
	}
	if loaded.DeviceSeconds["child"] != 84 {
		t.Fatalf("unexpected device state: %+v", loaded)
	}
	reset, err := loadState(path, "2026-08-20")
	if err != nil {
		t.Fatal(err)
	}
	if len(reset.Users) != 0 || reset.Date != "2026-08-20" {
		t.Fatalf("state was not reset: %+v", reset)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("state mode = %o", info.Mode().Perm())
	}
}

func TestLoadStateRejectsUntrustedOrInvalidData(t *testing.T) {
	tests := map[string]string{
		"unknown field":        `{"date":"2026-08-20","users":{},"extra":true}`,
		"trailing data":        `{"date":"2026-08-20","users":{}} {}`,
		"invalid date":         `{"date":"not-a-date","users":{}}`,
		"negative use":         `{"date":"2026-08-20","users":{"child":{"app":-1}}}`,
		"excessive use":        `{"date":"2026-08-20","users":{"child":{"app":86401}}}`,
		"negative device use":  `{"date":"2026-08-20","device_seconds":{"child":-1},"users":{}}`,
		"excessive device use": `{"date":"2026-08-20","device_seconds":{"child":86401},"users":{}}`,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "state")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "usage.json")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadState(path, "2026-08-20"); err == nil {
				t.Fatal("expected invalid state to be rejected")
			}
		})
	}
}

func TestStateRejectsUnsafePermissions(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "usage.json")
	if err := saveState(path, newState("2026-08-20")); err == nil || !strings.Contains(err.Error(), "mode 0700") {
		t.Fatalf("unexpected directory validation error: %v", err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"date":"2026-08-20","users":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadState(path, "2026-08-20"); err == nil || !strings.Contains(err.Error(), "mode 0600") {
		t.Fatalf("unexpected file validation error: %v", err)
	}
}
