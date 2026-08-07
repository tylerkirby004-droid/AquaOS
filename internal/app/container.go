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
	"github.com/tylerkirby004-droid/aquaos/internal/integrations/homeassistant"
	"github.com/tylerkirby004-droid/aquaos/internal/lifecycle"
	"github.com/tylerkirby004-droid/aquaos/internal/mqtt"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
	"github.com/tylerkirby004-droid/aquaos/internal/safety"
	"github.com/tylerkirby004-droid/aquaos/internal/sensors"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
	"github.com/tylerkirby004-droid/aquaos/internal/storage"
)

// Container owns the concrete object graph constructed during bootstrap.
type Container struct {
	Sensors        sensors.SensorRegistry
	Equipment      equipment.EquipmentRegistry
	Alarms         alarms.AlarmManager
	AlarmEvaluator *alarms.Evaluator
	Devices        devices.DeviceRegistry
	State          state.StateManager
	Configuration  config.ConfigurationManager
	MQTT           mqtt.MQTTClient
	Storage        storage.Storage
	API            api.API
	Health         *health.Manager
	Events         *events.Bus
	Safety         *safety.Engine
	Output         *output.Service
	OutputRouter   *output.ExecutorRouter
	Lifecycle      *lifecycle.Manager
	Simulator      health.Component
	Shelly         *shelly.Adapter
	ESP32          *esp32.Adapter
	Bench          *bench.Coordinator
	HomeAssistant  *homeassistant.Manager
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
	if err := registerAdapterInventory(context.Background(), cfg, deviceRegistry, sensorManager, equipmentManager); err != nil {
		return nil, fmt.Errorf("register configured adapter inventory: %w", err)
	}
	stateManager := state.NewManager(eventBus)
	alarmManager := alarms.NewManager(eventBus, logger.With("component", "alarms"))
	thresholdRules, err := configuredThresholdRules(cfg)
	if err != nil {
		return nil, fmt.Errorf("construct configured alarm rules: %w", err)
	}
	alarmEvaluator, err := alarms.NewEvaluator(eventBus, alarmManager, thresholdRules)
	if err != nil {
		return nil, fmt.Errorf("construct alarm evaluator: %w", err)
	}
	profiles := make([]equipment.Profile, 0, len(cfg.Adapters.Shelly.Endpoints))
	equipmentDefinitions := make(map[string]config.Equipment, len(cfg.Inventory.Equipment))
	sensorDefinitions := make(map[string]config.Sensor, len(cfg.Inventory.Sensors))
	for _, definition := range cfg.Inventory.Equipment {
		equipmentDefinitions[definition.EntityID] = definition
	}
	for _, definition := range cfg.Inventory.Sensors {
		sensorDefinitions[definition.ID] = definition
	}
	if cfg.Adapters.Shelly.Enabled {
		for _, endpoint := range cfg.Adapters.Shelly.Endpoints {
			kind := equipment.KindOutlet
			hazardous := false
			limits := equipment.Limits{MaximumOn: endpoint.MaximumOn}
			definition, configuredDefinition := equipmentDefinitions[endpoint.EquipmentID]
			if configuredDefinition && definition.Kind != "" {
				kind = equipment.Kind(definition.Kind)
				hazardous = definition.Hazardous
				limits = equipment.Limits{MaximumOn: definition.MaximumOn, MaximumDailyOn: definition.MaximumDaily, MinimumOff: definition.MinimumOff}
			} else if endpoint.EquipmentKind == "heater" {
				kind, hazardous = equipment.KindHeater, true
			}
			requiredProbeIDs := append([]string(nil), endpoint.RequiredProbeIDs...)
			if configuredDefinition && len(definition.RequiredSensorIDs) > 0 {
				requiredProbeIDs = requiredProbeIDs[:0]
				for _, sensorID := range definition.RequiredSensorIDs {
					requiredProbeIDs = append(requiredProbeIDs, sensorDefinitions[sensorID].EntityID)
				}
			}
			requiredInputs := make([]equipment.InputRequirement, 0, len(requiredProbeIDs))
			for _, probeID := range requiredProbeIDs {
				requiredInputs = append(requiredInputs, equipment.InputRequirement{Key: state.Key{EntityKind: state.EntitySensor, EntityID: domain.EntityID(probeID), Plane: state.PlaneObservation, Attribute: "measurement"}})
			}
			profiles = append(profiles, equipment.Profile{EquipmentID: domain.EquipmentID(endpoint.EquipmentID), Kind: kind, Hazardous: hazardous, FailSafeOn: endpoint.SafeOn, Capabilities: []domain.Capability{domain.CapabilitySwitch, domain.CapabilityCommandAcknowledgement, domain.CapabilityReportedState, domain.CapabilityPowerTelemetry}, Limits: limits, RequiredInputs: requiredInputs})
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
	var storageManager storage.Storage = storage.NewDisabled()
	if cfg.Storage.InfluxDB.Enabled {
		token, tokenErr := readSecretFile(cfg.Storage.InfluxDB.TokenFile)
		if tokenErr != nil {
			return nil, fmt.Errorf("construct InfluxDB credentials: %w", tokenErr)
		}
		client, clientErr := storage.NewInfluxClient(storage.InfluxConfig{URL: cfg.Storage.InfluxDB.URL, Organization: cfg.Storage.InfluxDB.Organization, Bucket: cfg.Storage.InfluxDB.Bucket, Token: token}, &http.Client{Timeout: cfg.Storage.InfluxDB.WriteTimeout})
		if clientErr != nil {
			return nil, fmt.Errorf("construct InfluxDB client: %w", clientErr)
		}
		writer, writerErr := storage.New(storage.Config{QueueCapacity: cfg.Storage.InfluxDB.QueueCapacity, BatchSize: cfg.Storage.InfluxDB.BatchSize, FlushInterval: cfg.Storage.InfluxDB.FlushInterval, RetryMinimum: cfg.Storage.InfluxDB.RetryMinimum, RetryMaximum: cfg.Storage.InfluxDB.RetryMaximum, WriteTimeout: cfg.Storage.InfluxDB.WriteTimeout}, client, logger.With("component", "storage"))
		if writerErr != nil {
			return nil, fmt.Errorf("construct storage writer: %w", writerErr)
		}
		sink, sinkErr := storage.NewEventSink(writer, logger.With("component", "storage-events"))
		if sinkErr != nil {
			return nil, sinkErr
		}
		if _, sinkErr = sink.Attach(eventBus); sinkErr != nil {
			return nil, fmt.Errorf("attach storage event sink: %w", sinkErr)
		}
		storageManager = writer
	}
	apiOptions := []api.Option{api.WithDependencies(api.Dependencies{Devices: deviceRegistry, Sensors: sensorManager, Equipment: equipmentManager, State: stateManager, Commands: outputService, Alarms: alarmManager, Configuration: configurationManager})}
	if cfg.HTTP.BearerTokenFile != "" {
		token, tokenErr := readSecretFile(cfg.HTTP.BearerTokenFile)
		if tokenErr != nil {
			return nil, fmt.Errorf("construct API credentials: %w", tokenErr)
		}
		authenticator, authErr := api.NewBearerAuthenticator(token, api.Principal{ID: "local-administrator", Roles: []api.Role{api.RoleAdministrator}})
		if authErr != nil {
			return nil, fmt.Errorf("construct API authenticator: %w", authErr)
		}
		apiOptions = append(apiOptions, api.WithSecurity(authenticator, nil))
	}
	apiServer := api.New(cfg.HTTP, healthManager, logger.With("component", "api"), apiOptions...)

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

	monitored := []health.Component{eventBus, configurationManager, deviceRegistry, sensorManager, equipmentManager, stateManager, safetyEngine, outputService, alarmManager, alarmEvaluator, storageManager}
	var mqttClient mqtt.MQTTClient
	var mqttConcrete *mqtt.Client
	var homeAssistant *homeassistant.Manager
	if cfg.MQTT.Enabled {
		mqttConcrete, err = mqtt.New(cfg.MQTT, logger.With("component", "mqtt"))
		if err != nil {
			return nil, fmt.Errorf("construct MQTT client: %w", err)
		}
		mqttClient = mqttConcrete
		monitored = append(monitored, mqttClient)
		exporter, exporterErr := mqtt.NewEventExporter(cfg.MQTT.SiteID, cfg.MQTT.MaximumPayload, mqttClient, logger.With("component", "mqtt-exporter"))
		if exporterErr != nil {
			return nil, fmt.Errorf("construct MQTT event exporter: %w", exporterErr)
		}
		if _, exporterErr = exporter.Attach(eventBus); exporterErr != nil {
			return nil, fmt.Errorf("attach MQTT event exporter: %w", exporterErr)
		}
		if cfg.MQTT.HomeAssistant.Enabled {
			notificationRuleIDs := make([]domain.RuleID, 0, len(cfg.Alarms.Rules))
			for _, rule := range cfg.Alarms.Rules {
				for _, target := range rule.Notifications {
					if target == "home-assistant" {
						notificationRuleIDs = append(notificationRuleIDs, domain.RuleID(rule.ID))
						break
					}
				}
			}
			tombstones := make([]homeassistant.Tombstone, 0, len(cfg.MQTT.HomeAssistant.Tombstones))
			for _, item := range cfg.MQTT.HomeAssistant.Tombstones {
				tombstones = append(tombstones, homeassistant.Tombstone{Component: item.Component, ObjectID: item.ObjectID})
			}
			homeAssistant, err = homeassistant.New(homeassistant.Config{SiteID: cfg.MQTT.SiteID, CommandTTL: cfg.MQTT.HomeAssistant.CommandTTL, MaximumPayload: cfg.MQTT.MaximumPayload, Tombstones: tombstones, NotificationRuleIDs: notificationRuleIDs}, mqttClient, homeassistant.RegistryInventory{DeviceRegistry: deviceRegistry, SensorRegistry: sensorManager, EquipmentRegistry: equipmentManager}, alarmManager, outputService, logger.With("component", "home-assistant"))
			if err != nil {
				return nil, fmt.Errorf("construct Home Assistant integration: %w", err)
			}
			mqttConcrete.SetReconciler(homeAssistant.Refresh)
			for _, eventType := range []events.Type{events.AlarmRaised, events.AlarmAcknowledged, events.AlarmCleared, events.AlarmEscalated} {
				if _, err = eventBus.Subscribe(eventType, homeAssistant.HandleAlarmEvent); err != nil {
					return nil, fmt.Errorf("attach Home Assistant alarm status: %w", err)
				}
			}
			monitored = append(monitored, homeAssistant)
		}
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
		if component == homeAssistant && homeAssistant != nil {
			required = false
		}
		if component == storageManager && cfg.Storage.InfluxDB.Enabled {
			required = false
		}
		healthManager.RegisterComponent(component, required)
	}
	components := []health.Component{healthManager}
	for _, component := range monitored {
		if component == mqttClient || component == homeAssistant || (component == storageManager && cfg.Storage.InfluxDB.Enabled) {
			components = append(components, lifecycle.NewOptional(component, logger.With("component", "lifecycle")))
		} else {
			components = append(components, component)
		}
	}

	return &Container{
		Sensors: sensorManager, Equipment: equipmentManager, Alarms: alarmManager, AlarmEvaluator: alarmEvaluator,
		Devices: deviceRegistry, State: stateManager, Configuration: configurationManager, MQTT: mqttClient,
		Storage: storageManager, API: apiServer, Health: healthManager, Events: eventBus, Safety: safetyEngine, Output: outputService,
		OutputRouter: outputRouter,
		Lifecycle: lifecycle.NewConfigured(logger, lifecycle.Timeouts{
			Startup: cfg.Application.StartupTimeout, Shutdown: cfg.Application.ShutdownTimeout, Component: cfg.Application.ComponentTimeout,
		}, components...), Simulator: simulatorAdapter,
		Shelly: shellyAdapter, ESP32: esp32Adapter, Bench: benchCoordinator, HomeAssistant: homeAssistant,
	}, nil
}

func configuredThresholdRules(cfg config.Config) ([]alarms.ThresholdRule, error) {
	sensorsByID := make(map[string]config.Sensor, len(cfg.Inventory.Sensors))
	for _, sensor := range cfg.Inventory.Sensors {
		sensorsByID[sensor.ID] = sensor
	}
	result := make([]alarms.ThresholdRule, 0, len(cfg.Alarms.Rules))
	for _, configured := range cfg.Alarms.Rules {
		sensor := sensorsByID[configured.SensorID]
		if sensor.EntityID == "" {
			return nil, fmt.Errorf("alarm %s sensor %s has no runtime entity_id", configured.ID, configured.SensorID)
		}
		scale, offset := 1.0, 0.0
		if sensor.Calibration.Enabled {
			scale, offset = sensor.Calibration.Scale, sensor.Calibration.Offset
		}
		result = append(result, alarms.ThresholdRule{ID: configured.ID, Name: configured.Name, SensorID: domain.SensorID(sensor.EntityID), Condition: configured.Condition, Threshold: configured.Threshold, ThresholdHigh: configured.ThresholdHigh, Severity: events.Severity(configured.Severity), Delay: configured.Delay, ClearDelay: configured.ClearDelay, Latching: configured.Latching, Scale: scale, Offset: offset})
	}
	return result, nil
}

// registerAdapterInventory creates REST and discovery identities from validated
// adapter ownership without opening connections or starting hardware work.
func registerAdapterInventory(ctx context.Context, cfg config.Config, deviceRegistry *devices.Registry, sensorRegistry *sensors.Registry, equipmentRegistry *equipment.Registry) error {
	deviceDefinitions := make(map[string]config.Device, len(cfg.Inventory.Devices))
	for _, definition := range cfg.Inventory.Devices {
		deviceDefinitions[definition.EntityID] = definition
	}
	sensorDefinitions := make(map[string]config.Sensor, len(cfg.Inventory.Sensors))
	for _, definition := range cfg.Inventory.Sensors {
		sensorDefinitions[definition.EntityID] = definition
	}
	equipmentDefinitions := make(map[string]config.Equipment, len(cfg.Inventory.Equipment))
	for _, definition := range cfg.Inventory.Equipment {
		equipmentDefinitions[definition.EntityID] = definition
	}
	for _, configured := range cfg.Adapters.Shelly.Endpoints {
		capabilities := []domain.Capability{domain.CapabilitySwitch, domain.CapabilityCommandAcknowledgement, domain.CapabilityReportedState, domain.CapabilityPowerTelemetry}
		deviceID := domain.DeviceID(configured.ID)
		endpointID := domain.EndpointID(configured.ID)
		deviceName := "Shelly " + configured.EquipmentKind
		deviceMetadata := map[string]string{"adapter": "shelly", "equipmentKind": configured.EquipmentKind}
		if definition, exists := deviceDefinitions[configured.ID]; exists {
			if definition.Name != "" {
				deviceName = definition.Name
			}
			for key, value := range definition.Metadata {
				deviceMetadata[key] = value
			}
		}
		if _, err := deviceRegistry.Register(ctx, domain.Device{ID: deviceID, Name: deviceName, Capabilities: capabilities, Metadata: deviceMetadata}); err != nil {
			return err
		}
		if _, err := deviceRegistry.RegisterEndpoint(ctx, domain.Endpoint{ID: endpointID, DeviceID: deviceID, Name: "Shelly channel", Capabilities: capabilities}); err != nil {
			return err
		}
		equipmentName := configured.EquipmentKind
		if definition, exists := equipmentDefinitions[configured.EquipmentID]; exists && definition.Name != "" {
			equipmentName = definition.Name
		}
		if _, err := equipmentRegistry.Register(ctx, domain.Equipment{ID: domain.EquipmentID(configured.EquipmentID), DeviceID: deviceID, EndpointID: endpointID, Name: equipmentName, Capabilities: capabilities, Metadata: map[string]string{"adapter": "shelly"}}); err != nil {
			return err
		}
	}
	for _, configured := range cfg.Adapters.ESP32.Endpoints {
		capabilities := []domain.Capability{domain.CapabilityObserve}
		deviceID := domain.DeviceID(configured.DeviceID)
		endpointID := domain.EndpointID(configured.ID)
		deviceName := "ESP32 sensor node"
		if definition, exists := deviceDefinitions[configured.DeviceID]; exists && definition.Name != "" {
			deviceName = definition.Name
		}
		if _, err := deviceRegistry.Register(ctx, domain.Device{ID: deviceID, Name: deviceName, Capabilities: capabilities, Metadata: map[string]string{"adapter": "esp32"}}); err != nil {
			return err
		}
		if _, err := deviceRegistry.RegisterEndpoint(ctx, domain.Endpoint{ID: endpointID, DeviceID: deviceID, Name: "ESP32 dual-probe endpoint", Capabilities: capabilities}); err != nil {
			return err
		}
		for index, probeID := range configured.ProbeIDs {
			sensorName := fmt.Sprintf("Temperature probe %d", index+1)
			if definition, exists := sensorDefinitions[probeID]; exists && definition.Name != "" {
				sensorName = definition.Name
			}
			if _, err := sensorRegistry.Register(ctx, domain.Sensor{ID: domain.SensorID(probeID), DeviceID: deviceID, EndpointID: endpointID, Name: sensorName, Quantity: domain.QuantityTemperature, Unit: domain.UnitCelsius, Capabilities: capabilities, Metadata: map[string]string{"adapter": "esp32"}}); err != nil {
				return err
			}
		}
	}
	return nil
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
