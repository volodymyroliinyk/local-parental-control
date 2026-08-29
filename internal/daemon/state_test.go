package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStateRoundTripAndDailyReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "usage.json")
	state := newState("2026-08-19")
	state.Users["child"] = map[string]int64{"vlc": 42}
	state.DeviceSeconds["child"] = 84
	state.ContinuousSeconds["child"] = 42
	state.BreakUntil["child"] = time.Date(2026, 8, 19, 12, 10, 0, 0, time.UTC)
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
	if loaded.ContinuousSeconds["child"] != 42 || !loaded.BreakUntil["child"].Equal(state.BreakUntil["child"]) {
		t.Fatalf("unexpected break state: %+v", loaded)
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

func TestLoadStateRejectsLocalDateRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "usage.json")
	state := newState("2026-08-20")
	state.DeviceSeconds["child"] = 120
	if err := saveState(path, state); err != nil {
		t.Fatal(err)
	}
	rollbackState, err := loadState(path, "2026-08-19")
	if err == nil || !strings.Contains(err.Error(), "clock rollback detected") {
		t.Fatalf("unexpected rollback error: %v", err)
	}
	if rollbackState.Date != "2026-08-20" || rollbackState.DeviceSeconds["child"] != 120 {
		t.Fatalf("rollback did not return preserved state: %+v", rollbackState)
	}
	loaded, err := loadState(path, "2026-08-20")
	if err != nil || loaded.DeviceSeconds["child"] != 120 {
		t.Fatalf("recorded date state was not preserved: state=%+v err=%v", loaded, err)
	}
}

func TestLoadServiceStatePreservesUsageDuringDateRollbackRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "usage.json")
	state := newState("2026-08-20")
	state.DeviceSeconds["child"] = 120
	if err := saveState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, recovery := loadServiceState(path, "2026-08-19")
	if recovery == nil || !recovery.resetRequired || !strings.Contains(recovery.reason.Error(), "clock rollback detected") {
		t.Fatalf("rollback did not enter recovery: %+v", recovery)
	}
	if loaded.Date != "2026-08-20" || loaded.DeviceSeconds["child"] != 120 {
		t.Fatalf("rollback recovery discarded usage: %+v", loaded)
	}
}

func TestLoadStateRejectsUntrustedOrInvalidData(t *testing.T) {
	tests := map[string]string{
		"unknown field":           `{"date":"2026-08-20","users":{},"extra":true}`,
		"trailing data":           `{"date":"2026-08-20","users":{}} {}`,
		"invalid date":            `{"date":"not-a-date","users":{}}`,
		"negative use":            `{"date":"2026-08-20","users":{"child":{"app":-1}}}`,
		"excessive use":           `{"date":"2026-08-20","users":{"child":{"app":86401}}}`,
		"negative device use":     `{"date":"2026-08-20","device_seconds":{"child":-1},"users":{}}`,
		"excessive device use":    `{"date":"2026-08-20","device_seconds":{"child":86401},"users":{}}`,
		"negative continuous use": `{"date":"2026-08-20","continuous_seconds":{"child":-1},"users":{}}`,
		"zero break deadline":     `{"date":"2026-08-20","break_until":{"child":"0001-01-01T00:00:00Z"},"users":{}}`,
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

func TestRecoveryMarkerKeepsMissingStateBlocked(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "usage.json")
	if err := writeRecoveryMarker(path, true); err != nil {
		t.Fatal(err)
	}
	state, recovery := loadServiceState(path, "2026-08-27")
	if recovery == nil || !recovery.resetRequired || state.Date != "2026-08-27" {
		t.Fatalf("missing state escaped recovery: state=%+v recovery=%+v", state, recovery)
	}
	info, err := os.Stat(recoveryMarkerPath(path))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("invalid recovery marker: info=%v err=%v", info, err)
	}
}

func TestLoadServiceStateEntersRecoveryForInvalidState(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "usage.json")
	if err := os.WriteFile(path, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	state, recovery := loadServiceState(path, "2026-08-27")
	if recovery == nil || !recovery.resetRequired || state.Date != "2026-08-27" {
		t.Fatalf("invalid state did not enter recovery: state=%+v recovery=%+v", state, recovery)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "not-json" {
		t.Fatalf("invalid state was modified: %q, %v", data, err)
	}
}
