// Command aquaos starts the AquaOS supervisory service.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tylerkirby004-droid/aquaos/internal/app"
	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/logging"
	"github.com/tylerkirby004-droid/aquaos/internal/telemetry"
)

func main() { os.Exit(run()) }

func run() int {
	configPath := flag.String("config", os.Getenv("AQUAOS_CONFIG"), "path to YAML configuration")
	flag.Parse()
	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "configuration path is required through -config or AQUAOS_CONFIG")
		return 1
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load configuration: %v\n", err)
		return 1
	}
	logger, levelController, err := logging.NewDynamic(os.Stdout, cfg.Application.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configure logging: %v\n", err)
		return 1
	}
	slog.SetDefault(logger)

	rootCtx, cancelSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelSignals()
	build := telemetry.CurrentBuild()
	logger.Info("AquaOS build", "version", build.Version, "revision", build.Revision, "built_at", build.BuiltAt, "modified", build.Modified)
	container, err := app.New(cfg, *configPath, logger, app.WithLogLevelSetter(levelController))
	if err != nil {
		logger.Error("compose application", "error", err)
		return 1
	}
	if err := container.Lifecycle.Start(rootCtx); err != nil {
		logger.Error("application startup failed", "error", err)
		return 1
	}
	logger.Info("AquaOS started")
	<-rootCtx.Done()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.Application.ShutdownTimeout)
	defer cancelShutdown()
	if err := container.Lifecycle.Stop(shutdownCtx); err != nil {
		logger.Error("application shutdown failed", "error", err)
		return 1
	}
	logger.Info("AquaOS stopped")
	return 0
}
