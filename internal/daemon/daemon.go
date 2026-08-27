package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/volodymyroliinyk/local-parental-control/internal/api"
	"github.com/volodymyroliinyk/local-parental-control/internal/config"
	proc "github.com/volodymyroliinyk/local-parental-control/internal/process"
)

const DefaultStatePath = "/var/lib/local-parental-control/usage.json"
const DefaultSocketPath = "/run/local-parental-control/control.sock"
const DefaultStatusSocketPath = "/run/local-parental-control/status.sock"

type Service struct {
	mu                                sync.RWMutex
	cfg                               *config.Config
	configPath, statePath, socketPath string
	statusSocketPath                  string
	state                             usageState
	scanner                           proc.Scanner
	logger                            *slog.Logger
	uidUsers                          map[uint32]string
	pendingKill                       map[int]pendingTermination
	lastTick                          time.Time
	now                               func() time.Time
	loadConfig                        func(string) (*config.Config, error)
	authorizePeer                     func(net.Conn) error
	peerUID                           func(net.Conn) (uint32, error)
	sessions                          sessionController
	lookupUser                        func(string) (*user.User, error)
	recovery                          *stateRecovery
}

type pendingTermination struct {
	deadline time.Time
	process  proc.Info
}

type stateRecovery struct {
	reason        error
	resetRequired bool
}

func New(cfg *config.Config, configPath, statePath, socketPath, statusSocketPath string, logger *slog.Logger) (*Service, error) {
	now := time.Now()
	date := now.In(cfg.Location()).Format("2006-01-02")
	state, recovery := loadServiceState(statePath, date)
	s := &Service{cfg: cfg, configPath: configPath, statePath: statePath, socketPath: socketPath, statusSocketPath: statusSocketPath, state: state, scanner: proc.NewScanner(), logger: logger, pendingKill: make(map[int]pendingTermination), sessions: loginctlController{}, now: time.Now, loadConfig: config.LoadSecure, authorizePeer: authorizeRootPeer, peerUID: unixPeerUID, lookupUser: user.Lookup, lastTick: now}
	s.recovery = recovery
	if err := s.resolveUsers(); err != nil {
		return nil, err
	}
	if recovery != nil {
		logger.Error("usage state recovery required; access is blocked", "error", recovery.reason)
	}
	return s, nil
}

func (s *Service) resolveUsers() error {
	resolved := make(map[uint32]string)
	names := make([]string, 0, len(s.cfg.Users))
	for name := range s.cfg.Users {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		u, err := s.lookupUser(name)
		if err != nil {
			return fmt.Errorf("resolve user %q: %w", name, err)
		}
		id, err := strconv.ParseUint(u.Uid, 10, 32)
		if err != nil {
			return fmt.Errorf("user %q has invalid numeric UID %q: %w", name, u.Uid, err)
		}
		uid := uint32(id)
		if other, ok := resolved[uid]; ok {
			return fmt.Errorf("users %q and %q resolve to the same numeric UID %d", other, name, uid)
		}
		resolved[uid] = name
	}
	s.uidUsers = resolved
	return nil
}

func (s *Service) Run(ctx context.Context) error {
	listener, err := s.listen()
	if err != nil {
		return err
	}
	defer func() { listener.Close(); os.Remove(s.socketPath) }()
	statusListener, err := s.listenStatus()
	if err != nil {
		return err
	}
	defer func() { statusListener.Close(); os.Remove(s.statusSocketPath) }()
	go s.serve(ctx, listener)
	go s.serveStatus(ctx, statusListener)
	s.logger.Info("local parental control started", "users", len(s.cfg.Users), "socket", s.socketPath, "status_socket", s.statusSocketPath)
	timer := time.NewTimer(time.Duration(s.cfg.PollIntervalSeconds) * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			var err error
			if s.recovery == nil {
				err = saveState(s.statePath, s.state)
				if err != nil {
					s.enterStateRecovery(err, false)
				}
			}
			s.mu.Unlock()
			return err
		case <-timer.C:
			if err := s.tick(); err != nil {
				s.logger.Error("monitoring iteration failed", "error", err)
			}
			s.mu.RLock()
			interval := s.cfg.PollIntervalSeconds
			s.mu.RUnlock()
			timer.Reset(time.Duration(interval) * time.Second)
		}
	}
}

func (s *Service) tick() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.recovery != nil {
		if !s.recovery.resetRequired {
			if err := s.retryStatePersistence(); err == nil {
				s.logger.Info("usage state persistence recovered")
				return nil
			}
		}
		for uid, username := range s.uidUsers {
			s.lock(uid, username, "usage state recovery required")
		}
		return nil
	}
	date := now.In(s.cfg.Location()).Format("2006-01-02")
	if s.state.Date != date {
		s.state = newState(date)
		s.logger.Info("daily usage counters reset", "date", date)
	}
	delta := now.Sub(s.lastTick)
	s.lastTick = now
	maxDelta := 2 * time.Duration(s.cfg.PollIntervalSeconds) * time.Second
	if delta < 0 {
		delta = 0
	}
	if delta > maxDelta {
		delta = maxDelta
	}
	processes, err := s.scanner.Scan()
	if err != nil {
		return err
	}
	defer func() {
		for _, process := range processes {
			if err := process.Close(); err != nil {
				s.logger.Warn("close pidfd failed", "pid", process.PID, "error", err)
			}
		}
	}()
	active := make(map[string]map[string]bool)
	activeUsers := make(map[string]uint32)
	current := make(map[int]proc.Info, len(processes))
	for _, p := range processes {
		current[p.PID] = p
		username, monitored := s.uidUsers[p.UID]
		if !monitored {
			continue
		}
		activeUsers[username] = p.UID
		app, found := findApplication(s.cfg.Users[username], p.Executable)
		if !found {
			continue
		}
		used := s.used(username, app.ID)
		if used >= int64(app.DailyMinutes*60) {
			s.terminate(p, now)
			continue
		}
		if active[username] == nil {
			active[username] = make(map[string]bool)
		}
		active[username][app.ID] = true
	}
	seconds := int64(delta / time.Second)
	countingUsers := make(map[string]bool)
	for username, uid := range activeUsers {
		uc := s.cfg.Users[username]
		used := s.state.DeviceSeconds[username]
		if !uc.AllowedAt(now.In(s.cfg.Location())) || used >= int64(uc.DailyDeviceMinutes*60) {
			s.state.ContinuousSeconds[username] = 0
			delete(s.state.BreakUntil, username)
			s.lock(uid, username, "device access limit")
			continue
		}
		if until, onBreak := s.state.BreakUntil[username]; onBreak {
			if now.Before(until) {
				s.lock(uid, username, "mandatory break")
				continue
			}
			delete(s.state.BreakUntil, username)
			s.state.ContinuousSeconds[username] = 0
		}
		unlocked, err := s.sessions.Unlocked(uid)
		if err != nil {
			s.logger.Warn("session state unavailable; usage paused", "user", username, "uid", uid, "error", err)
			continue
		}
		if !unlocked {
			continue
		}
		countingUsers[username] = true
		s.state.DeviceSeconds[username] += seconds
		s.state.ContinuousSeconds[username] += seconds
		if s.state.DeviceSeconds[username] >= int64(uc.DailyDeviceMinutes*60) {
			s.lock(uid, username, "daily device limit")
			continue
		}
		if s.state.ContinuousSeconds[username] >= int64(uc.ContinuousUseMinutes*60) {
			s.state.ContinuousSeconds[username] = 0
			s.state.BreakUntil[username] = now.Add(time.Duration(uc.BreakMinutes) * time.Minute)
			s.lock(uid, username, "mandatory break")
		}
	}
	for username, apps := range active {
		if !countingUsers[username] {
			continue
		}
		for appID := range apps {
			s.add(username, appID, seconds)
		}
	}
	// Enforce immediately when this tick consumes the remaining allowance.
	for _, p := range processes {
		username, monitored := s.uidUsers[p.UID]
		if !monitored {
			continue
		}
		app, found := findApplication(s.cfg.Users[username], p.Executable)
		if found && s.used(username, app.ID) >= int64(app.DailyMinutes*60) {
			s.terminate(p, now)
		}
	}
	for pid, pending := range s.pendingKill {
		if !now.Before(pending.deadline) {
			// A PID can be reused after the original process exits. Kill only if the
			// full process identity still matches at the deadline.
			if process, exists := current[pid]; exists && process.SameIdentity(pending.process) {
				if err := s.scanner.Signal(process, syscall.SIGKILL); err != nil {
					s.logger.Warn("SIGKILL failed", "pid", pid, "error", err)
				}
			}
			delete(s.pendingKill, pid)
		}
	}
	if err := saveState(s.statePath, s.state); err != nil {
		s.enterStateRecovery(err, false)
		return fmt.Errorf("persist usage state; access blocked: %w", err)
	}
	return nil
}

func (s *Service) enterStateRecovery(reason error, resetRequired bool) {
	s.recovery = &stateRecovery{reason: reason, resetRequired: resetRequired}
	if err := writeRecoveryMarker(s.statePath, resetRequired); err != nil {
		s.logger.Error("cannot persist state recovery marker", "error", err)
	}
}

func (s *Service) lock(uid uint32, username, reason string) {
	if err := s.sessions.Lock(uid); err != nil {
		s.logger.Warn("screen lock failed", "user", username, "uid", uid, "reason", reason, "error", err)
		return
	}
	s.logger.Debug("screen lock requested", "user", username, "uid", uid, "reason", reason)
}

func findApplication(uc config.UserConfig, executable string) (config.Application, bool) {
	for _, app := range uc.Applications {
		for _, candidate := range app.Executables {
			if config.ExecutableMatches(candidate, executable) {
				return app, true
			}
		}
	}
	return config.Application{}, false
}

func (s *Service) terminate(p proc.Info, now time.Time) {
	if _, pending := s.pendingKill[p.PID]; pending {
		return
	}
	if err := s.scanner.Signal(p, syscall.SIGTERM); err != nil {
		s.logger.Warn("SIGTERM failed", "pid", p.PID, "error", err)
		return
	}
	s.pendingKill[p.PID] = pendingTermination{
		deadline: now.Add(time.Duration(s.cfg.TerminationGraceSeconds) * time.Second),
		process:  p,
	}
	s.logger.Warn("application limit enforced", "pid", p.PID, "executable", p.Executable)
}

func (s *Service) used(user, app string) int64 {
	if s.state.Users[user] == nil {
		return 0
	}
	return s.state.Users[user][app]
}
func (s *Service) add(user, app string, seconds int64) {
	if s.state.Users[user] == nil {
		s.state.Users[user] = make(map[string]int64)
	}
	s.state.Users[user][app] += seconds
}

func (s *Service) listen() (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0755); err != nil {
		return nil, err
	}
	if err := validateSocketDirectory(filepath.Dir(s.socketPath)); err != nil {
		return nil, err
	}
	if err := os.Remove(s.socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(s.socketPath, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

func (s *Service) listenStatus() (net.Listener, error) {
	directory := filepath.Dir(s.statusSocketPath)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, err
	}
	if err := validateSocketDirectory(directory); err != nil {
		return nil, err
	}
	if err := os.Remove(s.statusSocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", s.statusSocketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(s.statusSocketPath, 0666); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

func validateSocketDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || !info.IsDir() || info.Mode().Perm() != 0755 {
		return fmt.Errorf("socket directory %s must be owned by UID %d with mode 0755", path, os.Geteuid())
	}
	return nil
}

func (s *Service) serve(ctx context.Context, listener net.Listener) {
	go func() { <-ctx.Done(); listener.Close() }()
	clients := make(chan struct{}, 16)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() == nil {
				s.logger.Error("administrative socket", "error", err)
			}
			return
		}
		select {
		case clients <- struct{}{}:
			go func() {
				defer func() { <-clients }()
				s.handle(conn)
			}()
		default:
			conn.Close()
			s.logger.Warn("administrative socket connection limit reached")
		}
	}
}

func (s *Service) serveStatus(ctx context.Context, listener net.Listener) {
	go func() { <-ctx.Done(); listener.Close() }()
	clients := make(chan struct{}, 16)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() == nil {
				s.logger.Error("status socket", "error", err)
			}
			return
		}
		select {
		case clients <- struct{}{}:
			go func() {
				defer func() { <-clients }()
				s.handleStatus(conn)
			}()
		default:
			conn.Close()
		}
	}
}

func (s *Service) handleStatus(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	uid, err := s.peerUID(conn)
	if err != nil {
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	username, ok := s.uidUsers[uid]
	if !ok {
		_ = json.NewEncoder(conn).Encode(api.Response{Error: "current user is not configured"})
		return
	}
	status := s.statusForUsers([]string{username})
	status.RecoveryReason = ""
	_ = json.NewEncoder(conn).Encode(api.Response{OK: true, Status: status})
}

func (s *Service) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := s.authorizePeer(conn); err != nil {
		s.logger.Warn("rejected administrative socket client", "error", err)
		return
	}
	var req api.Request
	dec := json.NewDecoder(io.LimitReader(conn, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		json.NewEncoder(conn).Encode(api.Response{Error: "invalid request: " + err.Error()})
		return
	}
	response := s.execute(req)
	_ = json.NewEncoder(conn).Encode(response)
}

func authorizeRootPeer(conn net.Conn) error {
	uid, err := unixPeerUID(conn)
	if err != nil {
		return err
	}
	if uid != 0 {
		return errors.New("administrative client must run as root")
	}
	return nil
}

func unixPeerUID(conn net.Conn) (uint32, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("connection is not a Unix socket")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var credentials *syscall.Ucred
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, socketErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if socketErr != nil {
		return 0, socketErr
	}
	if credentials == nil {
		return 0, errors.New("missing Unix peer credentials")
	}
	return credentials.Uid, nil
}

func (s *Service) execute(req api.Request) api.Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch req.Command {
	case "status":
		return api.Response{OK: true, Status: s.status()}
	case "reload":
		cfg, err := s.loadConfig(s.configPath)
		if err != nil {
			return api.Response{Error: err.Error()}
		}
		old := s.cfg
		s.cfg = cfg
		if err := s.resolveUsers(); err != nil {
			s.cfg = old
			_ = s.resolveUsers()
			return api.Response{Error: err.Error()}
		}
		// A raised limit or removed rule must cancel previously scheduled kills.
		s.pendingKill = make(map[int]pendingTermination)
		return api.Response{OK: true, Message: "configuration reloaded"}
	case "recover-state":
		if s.recovery == nil {
			return api.Response{Error: "usage state does not require recovery"}
		}
		message, err := s.recoverState()
		if err != nil {
			return api.Response{Error: err.Error()}
		}
		return api.Response{OK: true, Message: message}
	case "reset":
		if s.recovery != nil {
			return api.Response{Error: "usage state recovery is required; run lpctl recover-state"}
		}
		if _, ok := s.cfg.Users[req.User]; !ok {
			return api.Response{Error: fmt.Sprintf("unknown user %q", req.User)}
		}
		if req.Application == "" {
			delete(s.state.Users, req.User)
			delete(s.state.DeviceSeconds, req.User)
			delete(s.state.ContinuousSeconds, req.User)
			delete(s.state.BreakUntil, req.User)
		} else {
			found := false
			for _, app := range s.cfg.Users[req.User].Applications {
				if app.ID == req.Application {
					found = true
				}
			}
			if !found {
				return api.Response{Error: fmt.Sprintf("unknown application %q for user %q", req.Application, req.User)}
			}
			if s.state.Users[req.User] != nil {
				delete(s.state.Users[req.User], req.Application)
			}
		}
		// Reset is an explicit administrative unblock operation.
		s.pendingKill = make(map[int]pendingTermination)
		if err := saveState(s.statePath, s.state); err != nil {
			s.enterStateRecovery(err, false)
			return api.Response{Error: err.Error()}
		}
		return api.Response{OK: true, Message: "usage counters reset"}
	default:
		return api.Response{Error: fmt.Sprintf("unknown command %q", req.Command)}
	}
}

func (s *Service) status() *api.Status {
	names := make([]string, 0, len(s.cfg.Users))
	for name := range s.cfg.Users {
		names = append(names, name)
	}
	sort.Strings(names)
	return s.statusForUsers(names)
}

func (s *Service) statusForUsers(names []string) *api.Status {
	result := &api.Status{Date: s.state.Date}
	if s.recovery != nil {
		result.RecoveryRequired = true
		result.RecoveryReason = s.recovery.reason.Error()
	}
	for _, name := range names {
		uc := s.cfg.Users[name]
		deviceLimit := int64(uc.DailyDeviceMinutes * 60)
		deviceUsed := s.state.DeviceSeconds[name]
		us := api.UserStatus{Name: name, DeviceUsedSeconds: deviceUsed, DeviceLimitSeconds: deviceLimit, AllowedFrom: uc.AllowedFrom, AllowedUntil: uc.AllowedUntil, DeviceBlocked: s.recovery != nil || deviceUsed >= deviceLimit || !uc.AllowedAt(s.now().In(s.cfg.Location())), ContinuousUsedSeconds: s.state.ContinuousSeconds[name], ContinuousLimitSeconds: int64(uc.ContinuousUseMinutes * 60), RecoveryRequired: s.recovery != nil}
		if until, ok := s.state.BreakUntil[name]; ok && s.now().Before(until) {
			us.BreakUntil = until.In(s.cfg.Location()).Format(time.RFC3339)
			us.DeviceBlocked = true
		}
		apps := append([]config.Application(nil), s.cfg.Users[name].Applications...)
		sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
		for _, app := range apps {
			used := s.used(name, app.ID)
			limit := int64(app.DailyMinutes * 60)
			us.Applications = append(us.Applications, api.ApplicationStatus{ID: app.ID, Name: app.Name, UsedSeconds: used, LimitSeconds: limit, Blocked: used >= limit})
		}
		result.Users = append(result.Users, us)
	}
	return result
}
