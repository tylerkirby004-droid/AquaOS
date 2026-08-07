// Package app is the composition root. Concrete dependencies are constructed
// here and injected through constructors; domain packages never use globals or
// instantiate infrastructure themselves.
package app

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/adapters/simulator"
	"github.com/tylerkirby004-droid/aquaos/internal/alarms"
	"github.com/tylerkirby004-droid/aquaos/internal/api"
	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/devices"
	"github.com/tylerkirby004-droid/aquaos/internal/equipment"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
	"github.com/tylerkirby004-droid/aquaos/internal/lifecycle"
	"github.com/tylerkirby004-droid/aquaos/internal/mqtt"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
	"github.com/tylerkirby004-droid/aquaos/internal/safety"
	"github.com/tylerkirby004-droid/aquaos/internal/sensors"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
	"github.com/tylerkirby004-droid/aquaos/internal/storage"
	"github.com/tylerkirby004-droid/aquaos/internal/subsystem"
)

// Container owns the concrete object graph constructed during bootstrap.
type Container struct {
	Sensors       sensors.SensorRegistry
	Equipment     equipment.EquipmentRegistry
	Alarms        alarms.AlarmManager
	Devices       devices.DeviceRegistry
	State         state.StateManager
	Configuration config.ConfigurationManager
	MQTT          mqtt.MQTTClient
	Storage       storage.Storage
	API           api.API
	Health        *health.Manager
	Events        *events.Bus
	Safety        *safety.Engine
	Output        *output.Service
	Lifecycle     *lifecycle.Manager
	Simulator     health.Component
}

// Option customizes composition-root infrastructure without introducing globals.
type Option func(*options)

type options struct{ logLevels config.LogLevelSetter }

// WithLogLevelSetter enables atomic activation of harmless log-level reloads.
func WithLogLevelSetter(setter config.LogLevelSetter) Option {
	return func(values *options) { values.logLevels = setter }
}

// New constructs the complete AquaOS dependency graph without starting it.
func New(cfg config.Config, configPath string, logger *slog.Logger, supplied ...Option) (*Container, error) {
	if logger == nil {
		return nil, errors.New("construct application: logger is required")
	}
	if configPath == "" {
		return nil, errors.New("construct application: config path is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("construct application: validate config: %w", err)
	}
	values := options{}
	for _, option := range supplied {
		option(&values)
	}
	healthManager := health.NewManager()
	eventBus := events.NewBus(cfg.Application.EventConcurrency)
	managerOptions := make([]config.ManagerOption, 0, 1)
	if values.logLevels != nil {
		managerOptions = append(managerOptions, config.WithLogLevelSetter(values.logLevels))
	}
	configurationManager := config.NewManager(cfg, config.FileLoader{Path: configPath}, eventBus, logger.With("component", "configuration"), managerOptions...)
	deviceRegistry := devices.NewRegistry(eventBus)
	sensorManager := sensors.NewRegistry(eventBus, deviceRegistry)
	equipmentManager := equipment.NewRegistry(eventBus, deviceRegistry)
	stateManager := state.NewManager(eventBus)
	alarmManager := alarms.NewManager(eventBus, logger.With("component", "alarms"))
	safetyEngine, err := safety.NewEngine(stateManager, logger.With("component", "safety"), time.Now)
	if err != nil {
		return nil, fmt.Errorf("construct safety engine: %w", err)
	}
	outputService, err := output.NewService(safetyEngine, stateManager, output.RejectingExecutor{}, eventBus, logger.With("component", "output"))
	if err != nil {
		return nil, fmt.Errorf("construct output service: %w", err)
	}
	storageManager := subsystem.NewPassive("storage")
	apiServer := api.New(cfg.HTTP, healthManager, logger.With("component", "api"))

	monitored := []health.Component{eventBus, configurationManager, deviceRegistry, sensorManager, equipmentManager, stateManager, safetyEngine, outputService, alarmManager, storageManager}
	var mqttClient mqtt.MQTTClient
	if cfg.MQTT.Enabled {
		mqttClient, err = mqtt.New(cfg.MQTT, logger.With("component", "mqtt"))
		if err != nil {
			return nil, fmt.Errorf("construct MQTT client: %w", err)
		}
		monitored = append(monitored, mqttClient)
	}
	var simulatorAdapter health.Component
	if cfg.Simulator.Enabled {
		simulatorAdapter = simulator.New()
		monitored = append(monitored, simulatorAdapter)
	}
	monitored = append(monitored, apiServer)
	for _, component := range monitored {
		required := true
		if component == mqttClient && mqttClient != nil {
			required = cfg.MQTT.RequiredForReady
		}
		healthManager.RegisterComponent(component, required)
	}
	components := append([]health.Component{healthManager}, monitored...)

	return &Container{
		Sensors: sensorManager, Equipment: equipmentManager, Alarms: alarmManager,
		Devices: deviceRegistry, State: stateManager, Configuration: configurationManager, MQTT: mqttClient,
		Storage: storageManager, API: apiServer, Health: healthManager, Events: eventBus, Safety: safetyEngine, Output: outputService,
		Lifecycle: lifecycle.NewConfigured(logger, lifecycle.Timeouts{
			Startup: cfg.Application.StartupTimeout, Shutdown: cfg.Application.ShutdownTimeout, Component: cfg.Application.ComponentTimeout,
		}, components...), Simulator: simulatorAdapter,
	}, nil
}
