package indicator

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"time"

	"github.com/volodymyroliinyk/local-parental-control/internal/api"
)

var ErrNotConfigured = errors.New("current user is not configured")

func Read(socketPath string) (*api.UserStatus, string, error) {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	var response api.Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return nil, "", err
	}
	if !response.OK {
		if response.Error == ErrNotConfigured.Error() {
			return nil, "", ErrNotConfigured
		}
		return nil, "", errors.New(response.Error)
	}
	if response.Status == nil || len(response.Status.Users) != 1 {
		return nil, "", errors.New("invalid status response")
	}
	return &response.Status.Users[0], response.Status.Date, nil
}

func Remaining(used, limit int64) int64 {
	remaining := limit - used
	if remaining <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(remaining) / 60))
}

func Label(status *api.UserStatus) string {
	return fmt.Sprintf("%dm", Remaining(status.DeviceUsedSeconds, status.DeviceLimitSeconds))
}

func Tooltip(status *api.UserStatus, date string) string {
	lines := []string{
		fmt.Sprintf("Device: %d min remaining", Remaining(status.DeviceUsedSeconds, status.DeviceLimitSeconds)),
	}
	if status.BreakUntil != "" {
		lines = append(lines, "Break in progress")
	} else {
		lines = append(lines, fmt.Sprintf("Until break: %d min", Remaining(status.ContinuousUsedSeconds, status.ContinuousLimitSeconds)))
	}
	for _, app := range status.Applications {
		lines = append(lines, fmt.Sprintf("%s: %d min remaining", app.Name, Remaining(app.UsedSeconds, app.LimitSeconds)))
	}
	if date != "" {
		lines = append(lines, date)
	}
	return strings.Join(lines, "\n")
}
