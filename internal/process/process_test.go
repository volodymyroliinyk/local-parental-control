package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProcScannerScanReturnsValidProcesses(t *testing.T) {
	root := t.TempDir()
	writeProcessFixture(t, root, "123", "Uid:\t1000\t1000\t1000\t1000\n", "/usr/bin/example")
	writeProcessFixture(t, root, "456", "Name:\tmissing-uid\n", "/usr/bin/ignored")
	if err := os.Mkdir(filepath.Join(root, "789"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "not-a-pid"), 0700); err != nil {
		t.Fatal(err)
	}

	scanner := fixtureScanner(root)
	processes, err := scanner.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) != 1 {
		t.Fatalf("process count = %d, want 1: %+v", len(processes), processes)
	}
	defer processes[0].Close()
	got := processes[0]
	if got.PID != 123 || got.UID != 1000 || got.Executable != "/usr/bin/example" {
		t.Fatalf("unexpected process: %+v", got)
	}
}

func TestProcScannerTrimsDeletedExecutableSuffix(t *testing.T) {
	root := t.TempDir()
	writeProcessFixture(t, root, "42", "Uid:\t2000\t2000\t2000\t2000\n", "/opt/game (deleted)")
	processes, err := fixtureScanner(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(processes) != 1 || processes[0].Executable != "/opt/game" {
		t.Fatalf("unexpected processes: %+v", processes)
	}
	defer processes[0].Close()
}

func TestProcScannerScanMissingRoot(t *testing.T) {
	_, err := (&ProcScanner{Root: filepath.Join(t.TempDir(), "missing")}).Scan()
	if err == nil {
		t.Fatal("expected an error for a missing proc root")
	}
}

func TestReadUIDRejectsMalformedStatus(t *testing.T) {
	tests := []string{"Name:\ttest\n", "Uid:\n", "Uid:\tnot-a-number\n"}
	for _, contents := range tests {
		path := filepath.Join(t.TempDir(), "status")
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readUID(path); err == nil {
			t.Fatalf("expected error for %q", contents)
		}
	}
}

func TestSignalRejectsProcessWithoutPIDFD(t *testing.T) {
	if err := (&ProcScanner{}).Signal(Info{PID: 123}, syscall.SIGTERM); err == nil {
		t.Fatal("expected missing pidfd error")
	}
}

func TestSignalTerminatesProcess(t *testing.T) {
	command := exec.Command("sleep", "30")
	if err := command.Start(); err != nil {
		t.Skipf("sleep command unavailable: %v", err)
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	t.Cleanup(func() {
		if command.ProcessState == nil || !command.ProcessState.Exited() {
			_ = command.Process.Kill()
		}
	})

	pidfd, err := openPIDFD(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	process := Info{PID: command.Process.Pid, pidfd: pidfd + 1}
	defer process.Close()
	if err := (&ProcScanner{}).Signal(process, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("process exited successfully; expected termination by signal")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("process did not terminate after SIGTERM")
	}
}

func writeProcessFixture(t *testing.T, root, pid, status, executable string) {
	t.Helper()
	directory := filepath.Join(root, pid)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "status"), []byte(status), 0600); err != nil {
		t.Fatal(err)
	}
	stat := fmt.Sprintf("%s (fixture process) S%s 12345\n", pid, strings.Repeat(" 0", 18))
	if err := os.WriteFile(filepath.Join(directory, "stat"), []byte(stat), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(directory, "exe")); err != nil {
		t.Fatal(err)
	}
}

func fixtureScanner(root string) *ProcScanner {
	return &ProcScanner{Root: root, openPID: func(_ int) (int, error) {
		return syscall.Open("/dev/null", syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	}}
}
