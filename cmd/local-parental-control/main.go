package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/volodymyroliinyk/local-parental-control/internal/config"
	"github.com/volodymyroliinyk/local-parental-control/internal/daemon"
)

var version = "development"

const invalidConfigurationExitCode = 2

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	configPath := fs.String("config", config.DefaultPath, "path to configuration file")
	statePath := fs.String("state", daemon.DefaultStatePath, "path to persistent usage state")
	socketPath := fs.String("socket", daemon.DefaultSocketPath, "path to administrative Unix socket")
	statusSocketPath := fs.String("status-socket", daemon.DefaultStatusSocketPath, "path to read-only user status Unix socket")
	showVersion := fs.Bool("version", false, "print version and exit")
	_ = fs.Parse(os.Args[1:])
	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.LoadSecure(*configPath)
	if err != nil {
		logger.Error("cannot load configuration", "error", err)
		os.Exit(invalidConfigurationExitCode)
	}

	service, err := daemon.New(cfg, *configPath, *statePath, *socketPath, *statusSocketPath, logger)
	if err != nil {
		logger.Error("cannot initialize service", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
