package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

type sessionController interface {
	Terminate(uid uint32) error
}

type loginctlController struct{}

func (loginctlController) Terminate(uid uint32) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/bin/loginctl", "terminate-user", strconv.FormatUint(uint64(uid), 10)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("loginctl terminate-user: %w: %s", err, output)
	}
	return nil
}
