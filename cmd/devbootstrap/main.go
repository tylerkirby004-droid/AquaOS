// Command devbootstrap performs safe, broker-free foundation verification.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"

	"github.com/tylerkirby004-droid/aquaos/internal/app"
	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/logging"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("devbootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to broker-free development YAML")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *configPath == "" {
		report(stderr, "preflight failed: -config is required\n")
		return 1
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		report(stderr, "preflight failed: load config: %v\n", err)
		return 1
	}
	if cfg.MQTT.Enabled {
		report(stderr, "preflight failed: development bootstrap requires MQTT to be disabled\n")
		return 1
	}
	if !cfg.Simulator.Enabled {
		report(stderr, "preflight failed: development bootstrap requires the safe simulator adapter\n")
		return 1
	}

	logger, levelController, err := logging.NewDynamic(stdout, cfg.Application.LogLevel)
	if err != nil {
		report(stderr, "preflight failed: configure logger: %v\n", err)
		return 1
	}
	logger.Info("preflight passed", "go_version", runtime.Version(), "mqtt_enabled", false)
	container, err := app.New(cfg, *configPath, logger, app.WithLogLevelSetter(levelController))
	if err != nil {
		report(stderr, "setup failed: %v\n", err)
		return 1
	}

	rootCtx, cancelRoot := context.WithCancel(context.Background())
	if err := container.Lifecycle.Start(rootCtx); err != nil {
		cancelRoot()
		report(stderr, "verification failed: start application: %v\n", err)
		return 1
	}
	if container.Simulator == nil || !container.Health.Healthy() {
		cancelRoot()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.Application.ShutdownTimeout)
		defer cancelShutdown()
		_ = container.Lifecycle.Stop(shutdownCtx)
		report(stderr, "verification failed: foundation components are not ready\n")
		return 1
	}
	logger.Info("safe simulator verification passed", "broker_required", false, "hardware_access", false)

	cancelRoot()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.Application.ShutdownTimeout)
	defer cancelShutdown()
	if err := container.Lifecycle.Stop(shutdownCtx); err != nil {
		logger.Error("verification shutdown failed", "error", err)
		return 1
	}
	logger.Info("developer bootstrap complete", slog.String("result", "passed"))
	return 0
}

func report(w io.Writer, format string, args ...any) {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return
	}
}
