package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/volodymyroliinyk/local-parental-control/internal/api"
	"github.com/volodymyroliinyk/local-parental-control/internal/config"
	proc "github.com/volodymyroliinyk/local-parental-control/internal/process"
)

type fakeScanner struct {
	processes []proc.Info
	signals   []syscall.Signal
	pids      []int
	scanErr   error
	signalErr error
}

type fakeSessions struct {
	uids     []uint32
	err      error
	unlocked bool
	stateErr error
}

func (f *fakeSessions) Lock(uid uint32) error {
	if f.err != nil {
		return f.err
	}
	f.uids = append(f.uids, uid)
	return nil
}

func (f *fakeSessions) Unlocked(uint32) (bool, error) {
	return f.unlocked, f.stateErr
}

func (f *fakeScanner) Scan() ([]proc.Info, error) { return f.processes, f.scanErr }
func (f *fakeScanner) Signal(process proc.Info, signal syscall.Signal) error {
	if f.signalErr != nil {
		return f.signalErr
	}
	f.pids = append(f.pids, process.PID)
	f.signals = append(f.signals, signal)
	return nil
}

func TestTickCountsApplicationOnceAndEnforcesLimit(t *testing.T) {
	start := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	now := start
	cfg := &config.Config{Timezone: "UTC", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]config.UserConfig{"child": {DailyDeviceMinutes: 60, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "00:00", AllowedUntil: "23:59", Applications: []config.Application{{ID: "game", Name: "Game", Executables: []string{"/opt/game"}, DailyMinutes: 1}}}}}
	fake := &fakeScanner{processes: []proc.Info{{PID: 1, UID: 1000, Executable: "/opt/game"}, {PID: 2, UID: 1000, Executable: "/opt/game"}}}
	s := &Service{cfg: cfg, statePath: filepath.Join(t.TempDir(), "state", "state.json"), state: newState("2026-08-19"), scanner: fake, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), uidUsers: map[uint32]string{1000: "child"}, pendingKill: map[int]pendingTermination{}, sessions: &fakeSessions{unlocked: true}, lastTick: start, now: func() time.Time { return now }}
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

func TestTickResetsDateAndClampsElapsedTime(t *testing.T) {
	start := time.Date(2026, 8, 19, 23, 59, 59, 0, time.UTC)
	now := start.Add(10 * time.Second)
	fake := &fakeScanner{processes: []proc.Info{{PID: 10, UID: 1000, Executable: "/opt/app"}}}
	s := testService(t, start, &config.Config{Timezone: "UTC", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]config.UserConfig{"child": {DailyDeviceMinutes: 60, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "00:00", AllowedUntil: "23:59", Applications: []config.Application{{ID: "app", Name: "App", Executables: []string{"/opt/app"}, DailyMinutes: 10}}}}}, fake)
	s.state.Users["child"] = map[string]int64{"app": 100}
	s.now = func() time.Time { return now }
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if s.state.Date != "2026-08-20" || s.used("child", "app") != 4 {
		t.Fatalf("state after date reset: %+v", s.state)
	}

	now = now.Add(-time.Second)
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if s.used("child", "app") != 4 {
		t.Fatalf("negative elapsed time changed usage: %d", s.used("child", "app"))
	}
}

func TestTickCountsDeviceOnceAndLocksAtDailyLimit(t *testing.T) {
	start := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Second)
	fake := &fakeScanner{processes: []proc.Info{{PID: 1, UID: 1000}, {PID: 2, UID: 1000}}}
	s := testService(t, start, basicConfig(), fake)
	s.cfg.Users["child"] = config.UserConfig{DailyDeviceMinutes: 1, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "08:00", AllowedUntil: "20:00", Applications: s.cfg.Users["child"].Applications}
	s.state.DeviceSeconds["child"] = 58
	s.now = func() time.Time { return now }
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if got := s.state.DeviceSeconds["child"]; got != 60 {
		t.Fatalf("device usage = %d, want 60", got)
	}
	sessions := s.sessions.(*fakeSessions)
	if len(sessions.uids) != 1 || sessions.uids[0] != 1000 {
		t.Fatalf("locked UIDs = %v", sessions.uids)
	}
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if len(sessions.uids) != 2 {
		t.Fatalf("lock was not reinforced = %v", sessions.uids)
	}
}

func TestTickPausesUsageWhileSessionIsLocked(t *testing.T) {
	start := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Second)
	fake := &fakeScanner{processes: []proc.Info{{PID: 1, UID: 1000, Executable: "/opt/app"}}}
	s := testService(t, start, basicConfig(), fake)
	sessions := s.sessions.(*fakeSessions)
	sessions.unlocked = false
	s.state.DeviceSeconds["child"] = 30
	s.state.Users["child"] = map[string]int64{"app": 30}
	s.now = func() time.Time { return now }

	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if got := s.state.DeviceSeconds["child"]; got != 30 {
		t.Fatalf("device usage while locked = %d", got)
	}
	if got := s.used("child", "app"); got != 30 {
		t.Fatalf("application usage while locked = %d", got)
	}

	sessions.unlocked = true
	now = now.Add(2 * time.Second)
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if got := s.state.DeviceSeconds["child"]; got != 32 {
		t.Fatalf("device usage after unlock = %d, want 32", got)
	}
	if got := s.used("child", "app"); got != 32 {
		t.Fatalf("application usage after unlock = %d, want 32", got)
	}
}

func TestTickPausesUsageWhenSessionStateIsUnavailable(t *testing.T) {
	start := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Second)
	fake := &fakeScanner{processes: []proc.Info{{PID: 1, UID: 1000, Executable: "/opt/app"}}}
	s := testService(t, start, basicConfig(), fake)
	s.sessions.(*fakeSessions).stateErr = errors.New("logind unavailable")
	s.now = func() time.Time { return now }

	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if s.state.DeviceSeconds["child"] != 0 || s.used("child", "app") != 0 {
		t.Fatalf("usage changed without reliable session state: %+v", s.state)
	}
}

func TestTickLocksOutsideWindowWithoutCountingUsage(t *testing.T) {
	start := time.Date(2026, 8, 19, 7, 59, 58, 0, time.UTC)
	now := start.Add(2 * time.Second)
	fake := &fakeScanner{processes: []proc.Info{{PID: 1, UID: 1000}}}
	s := testService(t, start, basicConfig(), fake)
	s.cfg.Users["child"] = config.UserConfig{DailyDeviceMinutes: 60, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "08:01", AllowedUntil: "20:00", Applications: s.cfg.Users["child"].Applications}
	s.now = func() time.Time { return now }
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if got := s.state.DeviceSeconds["child"]; got != 0 {
		t.Fatalf("device usage outside window = %d", got)
	}
	if got := len(s.sessions.(*fakeSessions).uids); got != 1 {
		t.Fatalf("lock count = %d", got)
	}
}

func TestTickRetriesFailedLock(t *testing.T) {
	start := time.Date(2026, 8, 19, 7, 0, 0, 0, time.UTC)
	fake := &fakeScanner{processes: []proc.Info{{PID: 1, UID: 1000}}}
	s := testService(t, start, basicConfig(), fake)
	s.cfg.Users["child"] = config.UserConfig{DailyDeviceMinutes: 60, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "08:00", AllowedUntil: "20:00", Applications: s.cfg.Users["child"].Applications}
	sessions := s.sessions.(*fakeSessions)
	sessions.err = errors.New("logind unavailable")
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if len(sessions.uids) != 0 {
		t.Fatal("failed lock was recorded as successful")
	}
}

func TestTickStartsAndEnforcesMandatoryBreak(t *testing.T) {
	start := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Second)
	fake := &fakeScanner{processes: []proc.Info{{PID: 1, UID: 1000}}}
	s := testService(t, start, basicConfig(), fake)
	uc := s.cfg.Users["child"]
	uc.ContinuousUseMinutes = 1
	uc.BreakMinutes = 10
	s.cfg.Users["child"] = uc
	s.state.ContinuousSeconds["child"] = 58
	s.now = func() time.Time { return now }

	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	wantUntil := now.Add(10 * time.Minute)
	if got := s.state.BreakUntil["child"]; !got.Equal(wantUntil) {
		t.Fatalf("break until = %v, want %v", got, wantUntil)
	}
	if s.state.ContinuousSeconds["child"] != 0 || len(s.sessions.(*fakeSessions).uids) != 1 {
		t.Fatalf("break was not started: %+v", s.state)
	}

	now = now.Add(5 * time.Minute)
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if len(s.sessions.(*fakeSessions).uids) != 2 || s.state.DeviceSeconds["child"] != 2 {
		t.Fatal("active break was not reinforced or counted device time")
	}

	now = wantUntil
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if _, active := s.state.BreakUntil["child"]; active {
		t.Fatal("expired break remains active")
	}
	if s.state.ContinuousSeconds["child"] != 4 {
		t.Fatalf("continuous usage after break = %d", s.state.ContinuousSeconds["child"])
	}
}

func TestTickReturnsScannerError(t *testing.T) {
	start := time.Now()
	want := errors.New("proc unavailable")
	s := testService(t, start, basicConfig(), &fakeScanner{scanErr: want})
	if err := s.tick(); !errors.Is(err, want) {
		t.Fatalf("tick error = %v, want %v", err, want)
	}
}

func TestDelayedKillRequiresSamePIDAndExecutable(t *testing.T) {
	start := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Second)
	fake := &fakeScanner{processes: []proc.Info{{PID: 10, UID: 1000, Executable: "/opt/app"}}}
	s := testService(t, start, basicConfig(), fake)
	s.state.Users["child"] = map[string]int64{"app": 60}
	s.now = func() time.Time { return now }
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if len(fake.signals) != 1 || fake.signals[0] != syscall.SIGTERM {
		t.Fatalf("signals after enforcement: %v", fake.signals)
	}

	// Simulate PID reuse by a different executable before the kill deadline.
	fake.processes = []proc.Info{{PID: 10, UID: 0, Executable: "/usr/sbin/system-service"}}
	now = now.Add(4 * time.Second)
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if len(fake.signals) != 1 {
		t.Fatalf("reused PID was signaled: %v", fake.signals)
	}
}

func TestDelayedKillSignalsOriginalExecutable(t *testing.T) {
	start := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Second)
	fake := &fakeScanner{processes: []proc.Info{{PID: 10, UID: 1000, Executable: "/opt/app"}}}
	s := testService(t, start, basicConfig(), fake)
	s.state.Users["child"] = map[string]int64{"app": 60}
	s.now = func() time.Time { return now }
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Second)
	if err := s.tick(); err != nil {
		t.Fatal(err)
	}
	if len(fake.signals) != 2 || fake.signals[0] != syscall.SIGTERM || fake.signals[1] != syscall.SIGKILL {
		t.Fatalf("signals = %v", fake.signals)
	}
}

func TestTerminateDoesNotScheduleKillAfterSignalError(t *testing.T) {
	start := time.Now()
	fake := &fakeScanner{signalErr: syscall.EPERM}
	s := testService(t, start, basicConfig(), fake)
	s.terminate(proc.Info{PID: 10, Executable: "/opt/app"}, start)
	if len(s.pendingKill) != 0 {
		t.Fatalf("pending kill scheduled after failed signal: %+v", s.pendingKill)
	}
}

func TestExecuteStatusAndReset(t *testing.T) {
	start := time.Now()
	cfg := &config.Config{Timezone: "UTC", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]config.UserConfig{
		"z-user": {DailyDeviceMinutes: 60, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "00:00", AllowedUntil: "23:59", Applications: []config.Application{{ID: "z", Name: "Z", Executables: []string{"/z"}, DailyMinutes: 2}, {ID: "a", Name: "A", Executables: []string{"/a"}, DailyMinutes: 1}}},
		"a-user": {DailyDeviceMinutes: 60, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "00:00", AllowedUntil: "23:59", Applications: []config.Application{{ID: "x", Name: "X", Executables: []string{"/x"}, DailyMinutes: 1}}},
	}}
	s := testService(t, start, cfg, &fakeScanner{})
	s.state.Users["z-user"] = map[string]int64{"a": 60}
	s.state.DeviceSeconds["z-user"] = 60
	status := s.execute(api.Request{Command: "status"})
	if !status.OK || status.Status == nil || status.Status.Users[0].Name != "a-user" {
		t.Fatalf("unexpected status: %+v", status)
	}
	apps := status.Status.Users[1].Applications
	if len(apps) != 2 || apps[0].ID != "a" || !apps[0].Blocked {
		t.Fatalf("applications not sorted or blocked: %+v", apps)
	}
	if status.Status.Users[1].DeviceUsedSeconds != 60 || status.Status.Users[1].DeviceLimitSeconds != 3600 {
		t.Fatalf("unexpected device status: %+v", status.Status.Users[1])
	}

	if response := s.execute(api.Request{Command: "reset", User: "missing"}); response.OK || response.Error == "" {
		t.Fatalf("unexpected unknown-user response: %+v", response)
	}
	if response := s.execute(api.Request{Command: "reset", User: "z-user", Application: "missing"}); response.OK || response.Error == "" {
		t.Fatalf("unexpected unknown-app response: %+v", response)
	}
	s.pendingKill[99] = pendingTermination{deadline: start, process: proc.Info{PID: 99, UID: 1000, Executable: "/a"}}
	if response := s.execute(api.Request{Command: "reset", User: "z-user", Application: "a"}); !response.OK {
		t.Fatalf("reset failed: %+v", response)
	}
	if s.used("z-user", "a") != 0 || len(s.pendingKill) != 0 {
		t.Fatalf("reset did not clear state: %+v", s.state)
	}
	if response := s.execute(api.Request{Command: "reset", User: "z-user"}); !response.OK || s.state.DeviceSeconds["z-user"] != 0 {
		t.Fatalf("user reset did not clear device usage: %+v", response)
	}
	if response := s.execute(api.Request{Command: "unknown"}); response.OK || response.Error == "" {
		t.Fatalf("unexpected command response: %+v", response)
	}
}

func TestExecuteReloadIsTransactional(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, err := strconv.ParseUint(current.Uid, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	path := filepath.Join(t.TempDir(), "config.json")
	s := testService(t, start, basicConfig(), &fakeScanner{})
	s.configPath = path
	s.uidUsers = map[uint32]string{uint32(uid): "child"}
	if err := os.WriteFile(path, []byte(`{"invalid":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if response := s.execute(api.Request{Command: "reload"}); response.OK || s.cfg.Users["child"].Applications[0].ID != "app" {
		t.Fatalf("invalid reload replaced config: %+v", response)
	}

	valid := map[string]any{"timezone": "UTC", "users": map[string]any{current.Username: map[string]any{"daily_device_minutes": 60, "continuous_use_minutes": 60, "break_minutes": 10, "allowed_from": "00:00", "allowed_until": "23:59", "applications": []any{map[string]any{"id": "new", "name": "New", "executables": []string{"/opt/new"}, "daily_minutes": 5}}}}}
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s.pendingKill[1] = pendingTermination{deadline: start, process: proc.Info{PID: 1, UID: 1000, Executable: "/opt/app"}}
	if response := s.execute(api.Request{Command: "reload"}); !response.OK {
		t.Fatalf("valid reload failed: %+v", response)
	}
	if _, ok := s.cfg.Users[current.Username]; !ok || len(s.pendingKill) != 0 {
		t.Fatalf("reload did not replace config safely: %+v", s.cfg)
	}
}

func TestResolveUsersRejectsDuplicateNumericUID(t *testing.T) {
	s := testService(t, time.Now(), basicConfig(), &fakeScanner{})
	s.cfg.Users["alias"] = s.cfg.Users["child"]
	s.lookupUser = func(username string) (*user.User, error) {
		return &user.User{Username: username, Uid: "1000"}, nil
	}
	if err := s.resolveUsers(); err == nil || !strings.Contains(err.Error(), `users "alias" and "child" resolve to the same numeric UID 1000`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReloadRejectsDuplicateNumericUIDTransactionally(t *testing.T) {
	s := testService(t, time.Now(), basicConfig(), &fakeScanner{})
	s.lookupUser = func(username string) (*user.User, error) {
		return &user.User{Username: username, Uid: "1000"}, nil
	}
	duplicate := basicConfig()
	duplicate.Users["alias"] = duplicate.Users["child"]
	s.loadConfig = func(string) (*config.Config, error) { return duplicate, nil }

	response := s.execute(api.Request{Command: "reload"})
	if response.OK || !strings.Contains(response.Error, "same numeric UID 1000") {
		t.Fatalf("unexpected reload response: %+v", response)
	}
	if len(s.cfg.Users) != 1 || s.uidUsers[1000] != "child" {
		t.Fatalf("invalid reload changed active configuration: cfg=%+v uidUsers=%+v", s.cfg, s.uidUsers)
	}
}

func TestAdministrativeSocketPermissionsAndRequest(t *testing.T) {
	start := time.Now()
	s := testService(t, start, basicConfig(), &fakeScanner{})
	s.socketPath = filepath.Join(t.TempDir(), "run", "control.sock")
	listener, err := s.listen()
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skipf("Unix sockets are not permitted in this test sandbox: %v", err)
		}
		t.Fatal(err)
	}
	defer listener.Close()
	info, err := os.Stat(s.socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("socket mode = %o", info.Mode().Perm())
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			s.handle(conn)
		}
	}()
	conn, err := net.Dial("unix", s.socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(api.Request{Command: "status"}); err != nil {
		t.Fatal(err)
	}
	var response api.Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	<-done
	if !response.OK || response.Status == nil {
		t.Fatalf("unexpected socket response: %+v", response)
	}
}

func TestStatusSocketReturnsOnlyPeerUser(t *testing.T) {
	s := testService(t, time.Now(), basicConfig(), &fakeScanner{})
	s.cfg.Users["other"] = s.cfg.Users["child"]
	s.statusSocketPath = filepath.Join(t.TempDir(), "status", "status.sock")
	s.uidUsers = map[uint32]string{1000: "child"}
	s.peerUID = func(net.Conn) (uint32, error) { return 1000, nil }
	listener, err := s.listenStatus()
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skipf("Unix sockets are not permitted in this test sandbox: %v", err)
		}
		t.Fatal(err)
	}
	defer listener.Close()
	info, err := os.Stat(s.statusSocketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0666 {
		t.Fatalf("status socket mode = %o", info.Mode().Perm())
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			s.handleStatus(conn)
		}
	}()
	conn, err := net.Dial("unix", s.statusSocketPath)
	if err != nil {
		t.Fatal(err)
	}
	var response api.Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	<-done
	if !response.OK || response.Status == nil || len(response.Status.Users) != 1 || response.Status.Users[0].Name != "child" {
		t.Fatalf("unexpected status response: %+v", response)
	}
}

func TestUnixPeerUID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skipf("Unix sockets are not permitted in this test sandbox: %v", err)
		}
		t.Fatal(err)
	}
	defer listener.Close()
	result := make(chan uint32, 1)
	errorsChannel := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			errorsChannel <- acceptErr
			return
		}
		defer conn.Close()
		uid, peerErr := unixPeerUID(conn)
		if peerErr != nil {
			errorsChannel <- peerErr
			return
		}
		result <- uid
	}()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case err := <-errorsChannel:
		t.Fatal(err)
	case uid := <-result:
		if uid != uint32(os.Getuid()) {
			t.Fatalf("peer UID = %d, want %d", uid, os.Getuid())
		}
	}
}

func TestStatusSocketRejectsUnconfiguredPeer(t *testing.T) {
	server, client := net.Pipe()
	s := testService(t, time.Now(), basicConfig(), &fakeScanner{})
	s.peerUID = func(net.Conn) (uint32, error) { return 9999, nil }
	done := make(chan struct{})
	go func() { s.handleStatus(server); close(done) }()
	var response api.Response
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatal(err)
	}
	client.Close()
	<-done
	if response.OK || response.Error != "current user is not configured" || response.Status != nil {
		t.Fatalf("unexpected status response: %+v", response)
	}
}

func TestHandleRejectsUnknownRequestField(t *testing.T) {
	server, client := net.Pipe()
	s := testService(t, time.Now(), basicConfig(), &fakeScanner{})
	done := make(chan struct{})
	go func() { s.handle(server); close(done) }()
	if _, err := client.Write([]byte("{\"command\":\"status\",\"extra\":true}\n")); err != nil {
		t.Fatal(err)
	}
	var response api.Response
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatal(err)
	}
	client.Close()
	<-done
	if response.OK || response.Error == "" {
		t.Fatalf("unexpected response: %+v", response)
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

func TestFindApplicationMatchesSnapBeforeAndAfterRefresh(t *testing.T) {
	uc := config.UserConfig{Applications: []config.Application{{ID: "firefox", Executables: []string{"/snap/firefox/current/usr/lib/firefox/firefox"}}}}
	for _, executable := range []string{
		"/snap/firefox/100/usr/lib/firefox/firefox",
		"/snap/firefox/101/usr/lib/firefox/firefox",
	} {
		app, ok := findApplication(uc, executable)
		if !ok || app.ID != "firefox" {
			t.Fatalf("Snap process %q was not matched after revision change", executable)
		}
	}
}

func basicConfig() *config.Config {
	return &config.Config{Timezone: "UTC", PollIntervalSeconds: 2, TerminationGraceSeconds: 3, Users: map[string]config.UserConfig{"child": {DailyDeviceMinutes: 60, ContinuousUseMinutes: 60, BreakMinutes: 10, AllowedFrom: "00:00", AllowedUntil: "23:59", Applications: []config.Application{{ID: "app", Name: "App", Executables: []string{"/opt/app"}, DailyMinutes: 1}}}}}
}

func testService(t *testing.T, start time.Time, cfg *config.Config, scanner proc.Scanner) *Service {
	t.Helper()
	return &Service{
		cfg:           cfg,
		statePath:     filepath.Join(t.TempDir(), "state", "usage.json"),
		state:         newState(start.In(cfg.Location()).Format("2006-01-02")),
		scanner:       scanner,
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		uidUsers:      map[uint32]string{1000: "child"},
		pendingKill:   map[int]pendingTermination{},
		sessions:      &fakeSessions{unlocked: true},
		lastTick:      start,
		now:           func() time.Time { return start },
		loadConfig:    config.Load,
		authorizePeer: func(net.Conn) error { return nil },
		peerUID:       func(net.Conn) (uint32, error) { return 1000, nil },
		lookupUser:    user.Lookup,
	}
}
