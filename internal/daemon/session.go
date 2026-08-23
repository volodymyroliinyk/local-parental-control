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
}

type loginctlController struct{}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (loginctlController) Lock(uid uint32) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	uidText := strconv.FormatUint(uint64(uid), 10)
	output, err := exec.CommandContext(ctx, "/usr/bin/loginctl", "show-user", uidText, "--property=Sessions", "--value", "--no-ask-password").CombinedOutput()
	if err != nil {
		return fmt.Errorf("loginctl show-user: %w: %s", err, output)
	}
	sessions := strings.Fields(string(output))
	if len(sessions) == 0 {
		return nil
	}
	args := []string{"lock-session", "--no-ask-password"}
	for _, session := range sessions {
		if !sessionIDPattern.MatchString(session) {
			return fmt.Errorf("loginctl returned invalid session ID %q", session)
		}
		args = append(args, session)
	}
	output, err = exec.CommandContext(ctx, "/usr/bin/loginctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("loginctl lock-session: %w: %s", err, output)
	}
	return nil
}
