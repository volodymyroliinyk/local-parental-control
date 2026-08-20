package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoundTripAndDailyReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "usage.json")
	state := newState("2026-08-19")
	state.Users["child"] = map[string]int64{"vlc": 42}
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
