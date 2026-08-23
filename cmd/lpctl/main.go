package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/volodymyroliinyk/local-parental-control/internal/api"
	"github.com/volodymyroliinyk/local-parental-control/internal/config"
	"github.com/volodymyroliinyk/local-parental-control/internal/daemon"
)

var version = "development"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("lpctl", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultPath, "path to configuration file")
	socketPath := fs.String("socket", daemon.DefaultSocketPath, "path to administrative Unix socket")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Println(version)
		return 0
	}
	remaining := fs.Args()
	if len(remaining) == 0 {
		usage()
		return 2
	}

	command := remaining[0]
	if command == "validate" {
		cfg, err := config.LoadSecure(*configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid configuration:", err)
			return 1
		}
		fmt.Printf("configuration is valid (%d user(s), %d application rule(s))\n", len(cfg.Users), cfg.ApplicationCount())
		return 0
	}
	if command == "help" {
		usage()
		return 0
	}

	req := api.Request{Command: command}
	switch command {
	case "status", "reload":
		if len(remaining) != 1 {
			usage()
			return 2
		}
	case "reset":
		if len(remaining) < 2 || len(remaining) > 3 {
			usage()
			return 2
		}
		req.User = remaining[1]
		if len(remaining) == 3 {
			req.Application = remaining[2]
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", command)
		usage()
		return 2
	}

	response, err := call(*socketPath, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot contact daemon:", err)
		return 1
	}
	if !response.OK {
		fmt.Fprintln(os.Stderr, response.Error)
		return 1
	}
	if command == "status" {
		printStatus(response.Status)
	} else {
		fmt.Println(response.Message)
	}
	return 0
}

func call(socket string, req api.Request) (api.Response, error) {
	conn, err := net.DialTimeout("unix", socket, 3*time.Second)
	if err != nil {
		return api.Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return api.Response{}, err
	}
	var response api.Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return response, err
	}
	return response, nil
}

func printStatus(status *api.Status) {
	if status == nil {
		return
	}
	fmt.Printf("Date: %s\n\n", status.Date)
	for _, user := range status.Users {
		fmt.Printf("User: %s\n", user.Name)
		state := "ALLOWED"
		if user.DeviceBlocked {
			state = "BLOCKED"
		}
		fmt.Printf("  Device                   %6.1f / %-6.1f min  %s (%s-%s)\n", float64(user.DeviceUsedSeconds)/60, float64(user.DeviceLimitSeconds)/60, state, user.AllowedFrom, user.AllowedUntil)
		if user.BreakUntil != "" {
			fmt.Printf("  Break                    active until %s\n", user.BreakUntil)
		} else {
			fmt.Printf("  Continuous use           %6.1f / %-6.1f min\n", float64(user.ContinuousUsedSeconds)/60, float64(user.ContinuousLimitSeconds)/60)
		}
		for _, app := range user.Applications {
			state := "ALLOWED"
			if app.Blocked {
				state = "BLOCKED"
			}
			fmt.Printf("  %-24s %6.1f / %-6.1f min  %s\n", app.Name, float64(app.UsedSeconds)/60, float64(app.LimitSeconds)/60, state)
		}
		fmt.Println()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage: lpctl [options] COMMAND

Commands:
  validate                 validate the configuration file
  status                   show today's usage
  reload                   reload configuration without restarting
  reset USER [APP_ID]      reset usage for a user or one application
  help                     show this help`)
}
