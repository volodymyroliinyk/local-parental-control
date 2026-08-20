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

type Service struct {
	mu                                sync.RWMutex
	cfg                               *config.Config
	configPath, statePath, socketPath string
	state                             usageState
	scanner                           proc.Scanner
	logger                            *slog.Logger
	uidUsers                          map[uint32]string
	pendingKill                       map[int]pendingTermination
	lastTick                          time.Time
	now                               func() time.Time
	loadConfig                        func(string) (*config.Config, error)
	authorizePeer                     func(net.Conn) error
}

type pendingTermination struct {
	deadline time.Time
	process  proc.Info
}

func New(cfg *config.Config, configPath, statePath, socketPath string, logger *slog.Logger) (*Service, error) {
	now := time.Now()
	state, err := loadState(statePath, now.In(cfg.Location()).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	s := &Service{cfg: cfg, configPath: configPath, statePath: statePath, socketPath: socketPath, state: state, scanner: proc.NewScanner(), logger: logger, pendingKill: make(map[int]pendingTermination), now: time.Now, loadConfig: config.LoadSecure, authorizePeer: authorizeRootPeer, lastTick: now}
	if err := s.resolveUsers(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) resolveUsers() error {
	resolved := make(map[uint32]string)
	for name := range s.cfg.Users {
		u, err := user.Lookup(name)
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(u.Uid, 10, 32)
		if err != nil {
			return err
		}
		resolved[uint32(id)] = name
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
	go s.serve(ctx, listener)
	s.logger.Info("local parental control started", "users", len(s.cfg.Users), "socket", s.socketPath)
	timer := time.NewTimer(time.Duration(s.cfg.PollIntervalSeconds) * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			err := saveState(s.statePath, s.state)
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
	current := make(map[int]proc.Info, len(processes))
	for _, p := range processes {
		current[p.PID] = p
		username, monitored := s.uidUsers[p.UID]
		if !monitored {
			continue
		}
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
	for username, apps := range active {
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
	return saveState(s.statePath, s.state)
}

func findApplication(uc config.UserConfig, executable string) (config.Application, bool) {
	clean := filepath.Clean(executable)
	for _, app := range uc.Applications {
		for _, candidate := range app.Executables {
			if filepath.Clean(candidate) == clean {
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
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0700); err != nil {
		return nil, err
	}
	if err := validatePrivateDirectory(filepath.Dir(s.socketPath)); err != nil {
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
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return errors.New("administrative connection is not a Unix socket")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return err
	}
	var credentials *syscall.Ucred
	var socketErr error
	if err := raw.Control(func(fd uintptr) {
		credentials, socketErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if socketErr != nil {
		return socketErr
	}
	if credentials == nil || credentials.Uid != 0 {
		return errors.New("administrative client must run as root")
	}
	return nil
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
	case "reset":
		if _, ok := s.cfg.Users[req.User]; !ok {
			return api.Response{Error: fmt.Sprintf("unknown user %q", req.User)}
		}
		if req.Application == "" {
			delete(s.state.Users, req.User)
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
			return api.Response{Error: err.Error()}
		}
		return api.Response{OK: true, Message: "usage counters reset"}
	default:
		return api.Response{Error: fmt.Sprintf("unknown command %q", req.Command)}
	}
}

func (s *Service) status() *api.Status {
	result := &api.Status{Date: s.state.Date}
	names := make([]string, 0, len(s.cfg.Users))
	for name := range s.cfg.Users {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		us := api.UserStatus{Name: name}
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
