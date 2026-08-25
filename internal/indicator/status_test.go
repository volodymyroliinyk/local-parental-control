package indicator

import (
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/volodymyroliinyk/local-parental-control/internal/api"
)

func TestRemainingRoundsUpAndClamps(t *testing.T) {
	for _, test := range []struct {
		used, limit, want int64
	}{{0, 3600, 60}, {1, 60, 1}, {60, 60, 0}, {70, 60, 0}} {
		if got := Remaining(test.used, test.limit); got != test.want {
			t.Fatalf("Remaining(%d, %d) = %d, want %d", test.used, test.limit, got, test.want)
		}
	}
}

func TestReadReturnsSingleUserStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skipf("Unix sockets are not permitted in this test sandbox: %v", err)
		}
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_ = json.NewEncoder(conn).Encode(api.Response{OK: true, Status: &api.Status{Date: "2026-08-24", Users: []api.UserStatus{{Name: "child"}}}})
	}()
	status, date, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if status.Name != "child" || date != "2026-08-24" {
		t.Fatalf("unexpected status: %+v, %q", status, date)
	}
}

func TestReadRecognizesUnconfiguredUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skipf("Unix sockets are not permitted in this test sandbox: %v", err)
		}
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_ = json.NewEncoder(conn).Encode(api.Response{Error: ErrNotConfigured.Error()})
	}()
	if _, _, err := Read(path); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("error = %v, want ErrNotConfigured", err)
	}
}

func TestLabelAndTooltip(t *testing.T) {
	status := &api.UserStatus{DeviceUsedSeconds: 61, DeviceLimitSeconds: 3600, ContinuousUsedSeconds: 120, ContinuousLimitSeconds: 600, Applications: []api.ApplicationStatus{{Name: "Firefox", UsedSeconds: 60, LimitSeconds: 600}}}
	if got := Label(status); got != "59m" {
		t.Fatalf("label = %q", got)
	}
	tooltip := Tooltip(status, "2026-08-24")
	for _, text := range []string{"Device: 59 min remaining", "Until break: 8 min", "Firefox: 9 min remaining", "2026-08-24"} {
		if !strings.Contains(tooltip, text) {
			t.Fatalf("tooltip %q does not contain %q", tooltip, text)
		}
	}
	status.DeviceBlocked = true
	if got := Label(status); got != "59m" {
		t.Fatalf("blocked label = %q", got)
	}
}
