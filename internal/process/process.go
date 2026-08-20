package process

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type Info struct {
	PID        int
	UID        uint32
	Executable string
	StartTime  uint64
	pidfd      int
}

func (p Info) SameIdentity(other Info) bool {
	return p.PID == other.PID && p.UID == other.UID && p.StartTime == other.StartTime && p.Executable == other.Executable
}

func (p Info) Close() error {
	if p.pidfd <= 0 {
		return nil
	}
	return syscall.Close(p.pidfd - 1)
}

type Scanner interface {
	Scan() ([]Info, error)
	Signal(process Info, signal syscall.Signal) error
}

type ProcScanner struct {
	Root    string
	openPID func(int) (int, error)
}

func NewScanner() *ProcScanner { return &ProcScanner{Root: "/proc"} }

func (s *ProcScanner) Scan() ([]Info, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	result := make([]Info, 0)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		base := filepath.Join(s.Root, entry.Name())
		uid, err := readUID(filepath.Join(base, "status"))
		if err != nil {
			continue
		}
		startTime, err := readStartTime(filepath.Join(base, "stat"))
		if err != nil {
			continue
		}
		pidfd, err := s.pidfdOpen(pid)
		if errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("open pidfd for %d: %w", pid, err)
		}
		startTimeAfterOpen, err := readStartTime(filepath.Join(base, "stat"))
		if err != nil || startTimeAfterOpen != startTime {
			_ = syscall.Close(pidfd)
			continue
		}
		exe, err := os.Readlink(filepath.Join(base, "exe"))
		if err != nil {
			_ = syscall.Close(pidfd)
			continue
		}
		exe = strings.TrimSuffix(exe, " (deleted)")
		result = append(result, Info{PID: pid, UID: uid, Executable: filepath.Clean(exe), StartTime: startTime, pidfd: pidfd + 1})
	}
	return result, nil
}

func readStartTime(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	contents := string(data)
	closingParen := strings.LastIndexByte(contents, ')')
	if closingParen < 0 || closingParen+2 >= len(contents) {
		return 0, fmt.Errorf("malformed process stat in %s", path)
	}
	fields := strings.Fields(contents[closingParen+1:])
	// fields[0] is field 3 (state); starttime is field 22.
	if len(fields) <= 19 {
		return 0, fmt.Errorf("starttime field not found in %s", path)
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse starttime in %s: %w", path, err)
	}
	return startTime, nil
}

func readUID(path string) (uint32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		n, err := strconv.ParseUint(fields[1], 10, 32)
		return uint32(n), err
	}
	return 0, fmt.Errorf("Uid field not found in %s", path)
}

func (s *ProcScanner) pidfdOpen(pid int) (int, error) {
	if s.openPID != nil {
		return s.openPID(pid)
	}
	return openPIDFD(pid)
}

func (s *ProcScanner) Signal(process Info, signal syscall.Signal) error {
	if process.pidfd <= 0 {
		return errors.New("process has no pidfd")
	}
	err := signalPIDFD(process.pidfd-1, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
