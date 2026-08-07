// Package app owns AquaOS dependency composition.
package app

import (
	"errors"
	"log/slog"

	"github.com/tylerkirby004-droid/aquaos/internal/adapters/simulator"
	"github.com/tylerkirby004-droid/aquaos/internal/api"
	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
	"github.com/tylerkirby004-droid/aquaos/internal/lifecycle"
	"github.com/tylerkirby004-droid/aquaos/internal/storage"
	"github.com/tylerkirby004-droid/aquaos/internal/subsystem"
)

// Container owns the foundation object graph.
type Container struct {
	Storage   storage.Storage
	API       api.API
	Health    *health.Manager
	Lifecycle *lifecycle.Manager
	Simulator health.Component
}

// Option reserves explicit composition options without global state.
type Option func(*options)

type options struct{}

// WithLogLevelSetter accepts the dynamic logger dependency used by later reload support.
func WithLogLevelSetter(any) Option { return func(*options) {} }

// New constructs the broker-free foundation graph without starting it.
func New(cfg config.Config, configPath string, logger *slog.Logger, supplied ...Option) (*Container, error) {
	if logger == nil {
		return nil, errors.New("construct application: logger is required")
	}
	if configPath == "" {
		return nil, errors.New("construct application: config path is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	values := options{}
	for _, option := range supplied {
		option(&values)
	}
	healthManager := health.NewManager()
	storageManager := subsystem.NewPassive("storage")
	apiServer := api.New(cfg.HTTP, healthManager, logger.With("component", "api"))
	components := []health.Component{healthManager, storageManager}
	healthManager.RegisterComponent(storageManager, true)
	var simulatorAdapter health.Component
	if cfg.Simulator.Enabled {
		simulatorAdapter = simulator.New()
		healthManager.RegisterComponent(simulatorAdapter, true)
		components = append(components, simulatorAdapter)
	}
	healthManager.RegisterComponent(apiServer, true)
	components = append(components, apiServer)
	return &Container{
		Storage: storageManager, API: apiServer, Health: healthManager,
		Lifecycle: lifecycle.NewConfigured(logger, lifecycle.Timeouts{
			Startup: cfg.Application.StartupTimeout, Shutdown: cfg.Application.ShutdownTimeout, Component: cfg.Application.ComponentTimeout,
		}, components...), Simulator: simulatorAdapter,
	}, nil
}
