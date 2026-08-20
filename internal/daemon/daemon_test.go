package daemon

import (
	"io"
	"log/slog"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/volodymyroliinyk/local-parental-control/internal/config"
	proc "github.com/volodymyroliinyk/local-parental-control/internal/process"
)

type fakeScanner struct {
	processes []proc.Info
	signals   []syscall.Signal
}

func (f *fakeScanner) Scan() ([]proc.Info, error) { return f.processes, nil }
func (f *fakeScanner) Signal(_ int, signal syscall.Signal) error {
	f.signals = append(f.signals, signal)
	return nil
}

func TestTickCountsApplicationOnceAndEnforcesLimit(t *testing.T) {
	start := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	now := start
	cfg := &config.Config{Timezone: "UTC", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]config.UserConfig{"child": {Applications: []config.Application{{ID: "game", Name: "Game", Executables: []string{"/opt/game"}, DailyMinutes: 1}}}}}
	fake := &fakeScanner{processes: []proc.Info{{PID: 1, UID: 1000, Executable: "/opt/game"}, {PID: 2, UID: 1000, Executable: "/opt/game"}}}
	s := &Service{cfg: cfg, statePath: filepath.Join(t.TempDir(), "state.json"), state: newState("2026-08-19"), scanner: fake, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), uidUsers: map[uint32]string{1000: "child"}, pendingKill: map[int]pendingTermination{}, lastTick: start, now: func() time.Time { return now }}
	now = now.Add(2 * time.Second)
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if got := s.used("child", "game"); got != 2 {
		t.Fatalf("usage = %d, want 2", got)
	}
	s.state.Users["child"]["game"] = 58
	now = now.Add(2 * time.Second)
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if len(fake.signals) != 2 || fake.signals[0] != syscall.SIGTERM {
		t.Fatalf("signals = %v", fake.signals)
	}
}

func TestFindApplicationUsesExactCleanPath(t *testing.T) {
	uc := config.UserConfig{Applications: []config.Application{{ID: "x", Executables: []string{"/usr/bin/x"}}}}
	if _, ok := findApplication(uc, "/usr/bin/../bin/x"); !ok {
		t.Fatal("clean path should match")
	}
	if _, ok := findApplication(uc, "/tmp/x"); ok {
		t.Fatal("unexpected match")
	}
}
