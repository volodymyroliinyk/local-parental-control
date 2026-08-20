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
}

type Scanner interface {
	Scan() ([]Info, error)
	Signal(pid int, signal syscall.Signal) error
}

type ProcScanner struct{ Root string }

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
		exe, err := os.Readlink(filepath.Join(base, "exe"))
		if err != nil {
			continue
		}
		exe = strings.TrimSuffix(exe, " (deleted)")
		result = append(result, Info{PID: pid, UID: uid, Executable: filepath.Clean(exe)})
	}
	return result, nil
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

func (s *ProcScanner) Signal(pid int, signal syscall.Signal) error {
	err := syscall.Kill(pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
