package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type sessionController interface {
	Lock(uid uint32) error
	Unlocked(uid uint32) (bool, error)
}

type loginctlController struct{}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (loginctlController) Unlocked(uid uint32) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessions, err := userSessions(ctx, uid)
	if err != nil {
		return false, err
	}
	for _, session := range sessions {
		output, err := exec.CommandContext(ctx, "/usr/bin/loginctl", "show-session", session,
			"--property=Active", "--property=LockedHint", "--property=Type", "--no-ask-password").CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("loginctl show-session %q: %w: %s", session, err, output)
		}
		active, unlocked, graphical, err := parseSessionState(string(output))
		if err != nil {
			return false, fmt.Errorf("loginctl show-session %q: %w", session, err)
		}
		if active && unlocked && graphical {
			return true, nil
		}
	}
	return false, nil
}

func userSessions(ctx context.Context, uid uint32) ([]string, error) {
	uidText := strconv.FormatUint(uint64(uid), 10)
	output, err := exec.CommandContext(ctx, "/usr/bin/loginctl", "show-user", uidText, "--property=Sessions", "--value", "--no-ask-password").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("loginctl show-user: %w: %s", err, output)
	}
	sessions := strings.Fields(string(output))
	for _, session := range sessions {
		if !sessionIDPattern.MatchString(session) {
			return nil, fmt.Errorf("loginctl returned invalid session ID %q", session)
		}
	}
	return sessions, nil
}

func parseSessionState(output string) (active, unlocked, graphical bool, err error) {
	properties := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			properties[key] = value
		}
	}
	activeValue, activeFound := properties["Active"]
	lockedValue, lockedFound := properties["LockedHint"]
	typeValue, typeFound := properties["Type"]
	if !activeFound || !lockedFound || !typeFound {
		return false, false, false, fmt.Errorf("incomplete session state")
	}
	if activeValue != "yes" && activeValue != "no" {
		return false, false, false, fmt.Errorf("invalid Active value %q", activeValue)
	}
	if lockedValue != "yes" && lockedValue != "no" {
		return false, false, false, fmt.Errorf("invalid LockedHint value %q", lockedValue)
	}
	return activeValue == "yes", lockedValue == "no", typeValue == "x11" || typeValue == "wayland", nil
}

func (loginctlController) Lock(uid uint32) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessions, err := userSessions(ctx, uid)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return nil
	}
	args := []string{"lock-session", "--no-ask-password"}
	for _, session := range sessions {
		args = append(args, session)
	}
	output, err := exec.CommandContext(ctx, "/usr/bin/loginctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("loginctl lock-session: %w: %s", err, output)
	}
	return nil
}
