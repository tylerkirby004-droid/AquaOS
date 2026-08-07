// Package app is the composition root. Concrete dependencies are constructed
// here and injected through constructors; domain packages never use globals or
// instantiate infrastructure themselves.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/adapters/esp32"
	"github.com/tylerkirby004-droid/aquaos/internal/adapters/shelly"
	"github.com/tylerkirby004-droid/aquaos/internal/adapters/simulator"
	"github.com/tylerkirby004-droid/aquaos/internal/alarms"
	"github.com/tylerkirby004-droid/aquaos/internal/api"
	"github.com/tylerkirby004-droid/aquaos/internal/bench"
	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/devices"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
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
	OutputRouter  *output.ExecutorRouter
	Lifecycle     *lifecycle.Manager
	Simulator     health.Component
	Shelly        *shelly.Adapter
	ESP32         *esp32.Adapter
	Bench         *bench.Coordinator
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
	profiles := make([]equipment.Profile, 0, len(cfg.Adapters.Shelly.Endpoints))
	if cfg.Adapters.Shelly.Enabled {
		for _, endpoint := range cfg.Adapters.Shelly.Endpoints {
			kind := equipment.KindOutlet
			hazardous := false
			if endpoint.EquipmentKind == "heater" {
				kind, hazardous = equipment.KindHeater, true
			}
			requiredInputs := make([]equipment.InputRequirement, 0, len(endpoint.RequiredProbeIDs))
			for _, probeID := range endpoint.RequiredProbeIDs {
				requiredInputs = append(requiredInputs, equipment.InputRequirement{Key: state.Key{EntityKind: state.EntitySensor, EntityID: domain.EntityID(probeID), Plane: state.PlaneObservation, Attribute: "measurement"}})
			}
			profiles = append(profiles, equipment.Profile{EquipmentID: domain.EquipmentID(endpoint.EquipmentID), Kind: kind, Hazardous: hazardous, FailSafeOn: endpoint.SafeOn, Capabilities: []domain.Capability{domain.CapabilitySwitch, domain.CapabilityCommandAcknowledgement, domain.CapabilityReportedState, domain.CapabilityPowerTelemetry}, Limits: equipment.Limits{MaximumOn: endpoint.MaximumOn}, RequiredInputs: requiredInputs})
		}
	}
	safetyEngine, err := safety.NewEngine(stateManager, logger.With("component", "safety"), time.Now, profiles...)
	if err != nil {
		return nil, fmt.Errorf("construct safety engine: %w", err)
	}
	outputRouter := output.NewExecutorRouter()
	outputService, err := output.NewService(safetyEngine, stateManager, outputRouter, eventBus, logger.With("component", "output"))
	if err != nil {
		return nil, fmt.Errorf("construct output service: %w", err)
	}
	storageManager := subsystem.NewPassive("storage")
	apiServer := api.New(cfg.HTTP, healthManager, logger.With("component", "api"))

	var shellyAdapter *shelly.Adapter
	var esp32Adapter *esp32.Adapter
	var benchCoordinator *bench.Coordinator
	if cfg.Adapters.Shelly.Enabled || cfg.Adapters.ESP32.Enabled {
		shellyEndpoints := make([]shelly.Endpoint, 0, len(cfg.Adapters.Shelly.Endpoints))
		shellyPolicies := make([]bench.ShellyPolicy, 0, len(cfg.Adapters.Shelly.Endpoints))
		for _, endpoint := range cfg.Adapters.Shelly.Endpoints {
			value := shelly.Endpoint{ID: domain.EndpointID(endpoint.ID), EquipmentID: domain.EquipmentID(endpoint.EquipmentID), BaseURL: endpoint.BaseURL, Channel: endpoint.Channel, PollInterval: endpoint.PollInterval, RequestTimeout: endpoint.RequestTimeout, Retries: endpoint.Retries, SafeOn: endpoint.SafeOn, PowerReturnPolicy: shelly.PowerReturnPolicy(endpoint.PowerReturnPolicy)}
			shellyEndpoints = append(shellyEndpoints, value)
			shellyPolicies = append(shellyPolicies, bench.ShellyPolicy{Endpoint: value, RuleID: domain.RuleID(endpoint.AlarmRuleID)})
		}
		espEndpoints := make([]esp32.Endpoint, 0, len(cfg.Adapters.ESP32.Endpoints))
		espPolicies := make([]bench.ESP32Policy, 0, len(cfg.Adapters.ESP32.Endpoints))
		for _, endpoint := range cfg.Adapters.ESP32.Endpoints {
			token, readErr := readSecretFile(endpoint.BearerTokenFile)
			if readErr != nil {
				return nil, fmt.Errorf("construct ESP32 adapter credentials: %w", readErr)
			}
			value := esp32.Endpoint{ID: domain.EndpointID(endpoint.ID), DeviceID: domain.DeviceID(endpoint.DeviceID), BaseURL: endpoint.BaseURL, BearerToken: token, ProbeIDs: [2]domain.SensorID{domain.SensorID(endpoint.ProbeIDs[0]), domain.SensorID(endpoint.ProbeIDs[1])}, PollInterval: endpoint.PollInterval, RequestTimeout: endpoint.RequestTimeout, FreshFor: endpoint.FreshFor, MaximumClockSkew: endpoint.MaximumClockSkew, MaximumDifference: endpoint.MaximumDifference}
			espEndpoints = append(espEndpoints, value)
			espPolicies = append(espPolicies, bench.ESP32Policy{Endpoint: value, RuleID: domain.RuleID(endpoint.AlarmRuleID)})
		}
		benchCoordinator, err = bench.NewCoordinator(stateManager, alarmManager, outputService, logger.With("component", "bench-coordinator"), shellyPolicies, espPolicies)
		if err != nil {
			return nil, fmt.Errorf("construct bench coordinator: %w", err)
		}
		if err := benchCoordinator.RegisterRules(context.Background()); err != nil {
			return nil, fmt.Errorf("register bench alarm rules: %w", err)
		}
		if cfg.Adapters.Shelly.Enabled {
			client, clientErr := shelly.NewHTTPClient(&http.Client{})
			if clientErr != nil {
				return nil, clientErr
			}
			shellyAdapter, err = shelly.NewAdapter(client, benchCoordinator, outputService, benchCoordinator, logger.With("component", "adapter-shelly"), time.Now, shellyEndpoints...)
			if err != nil {
				return nil, fmt.Errorf("construct Shelly adapter: %w", err)
			}
			for _, endpoint := range shellyEndpoints {
				if err := outputRouter.Register(endpoint.EquipmentID, shellyAdapter); err != nil {
					return nil, fmt.Errorf("register Shelly output route: %w", err)
				}
			}
		}
		if cfg.Adapters.ESP32.Enabled {
			client, clientErr := esp32.NewHTTPClient(&http.Client{})
			if clientErr != nil {
				return nil, clientErr
			}
			esp32Adapter, err = esp32.NewAdapter(client, benchCoordinator, benchCoordinator, logger.With("component", "adapter-esp32"), time.Now, espEndpoints...)
			if err != nil {
				return nil, fmt.Errorf("construct ESP32 adapter: %w", err)
			}
		}
	}

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
	if shellyAdapter != nil {
		monitored = append(monitored, shellyAdapter)
	}
	if esp32Adapter != nil {
		monitored = append(monitored, esp32Adapter)
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
		OutputRouter: outputRouter,
		Lifecycle: lifecycle.NewConfigured(logger, lifecycle.Timeouts{
			Startup: cfg.Application.StartupTimeout, Shutdown: cfg.Application.ShutdownTimeout, Component: cfg.Application.ComponentTimeout,
		}, components...), Simulator: simulatorAdapter,
		Shelly: shellyAdapter, ESP32: esp32Adapter, Bench: benchCoordinator,
	}, nil
}

func readSecretFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open token file: %w", err)
	}
	defer func() { _ = file.Close() }()
	payload, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	if len(payload) > 4096 {
		return "", errors.New("token file exceeds 4096 bytes")
	}
	token := strings.TrimSpace(string(payload))
	if token == "" {
		return "", errors.New("token file is empty")
	}
	return token, nil
}
