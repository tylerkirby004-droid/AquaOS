// Package config owns versioned application configuration, validation, and
// safe activation planning. Configuration values are immutable snapshots once
// returned to callers.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"gopkg.in/yaml.v3"
)

const (
	// CurrentSchemaVersion is the only configuration schema accepted by this release.
	CurrentSchemaVersion = 1
	redactedSecret       = "[REDACTED]"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var haIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Config is the complete externally supplied AquaOS configuration.
type Config struct {
	SchemaVersion int         `yaml:"schema_version" json:"schemaVersion"`
	Application   Application `yaml:"application" json:"application"`
	HTTP          HTTP        `yaml:"http" json:"http"`
	MQTT          MQTT        `yaml:"mqtt" json:"mqtt"`
	Simulator     Simulator   `yaml:"simulator" json:"simulator"`
	Adapters      Adapters    `yaml:"adapters" json:"adapters"`
	Bench         Bench       `yaml:"bench" json:"bench"`
	Storage       Storage     `yaml:"storage" json:"storage"`
	Inventory     Inventory   `yaml:"inventory" json:"inventory"`
}

// Storage configures optional bounded historical persistence.
type Storage struct {
	InfluxDB InfluxDB `yaml:"influxdb" json:"influxdb"`
}

// InfluxDB configures the optional InfluxDB v2 writer.
type InfluxDB struct {
	Enabled       bool          `yaml:"enabled" json:"enabled"`
	URL           string        `yaml:"url" json:"url"`
	Organization  string        `yaml:"organization" json:"organization"`
	Bucket        string        `yaml:"bucket" json:"bucket"`
	TokenFile     string        `yaml:"token_file" json:"-"`
	QueueCapacity int           `yaml:"queue_capacity" json:"queueCapacity"`
	BatchSize     int           `yaml:"batch_size" json:"batchSize"`
	FlushInterval time.Duration `yaml:"flush_interval" json:"flushInterval"`
	RetryMinimum  time.Duration `yaml:"retry_minimum" json:"retryMinimum"`
	RetryMaximum  time.Duration `yaml:"retry_maximum" json:"retryMaximum"`
	WriteTimeout  time.Duration `yaml:"write_timeout" json:"writeTimeout"`
}

// Application contains process lifecycle and logging configuration.
type Application struct {
	LogLevel         string        `yaml:"log_level" json:"logLevel"`
	StartupTimeout   time.Duration `yaml:"startup_timeout" json:"startupTimeout"`
	ShutdownTimeout  time.Duration `yaml:"shutdown_timeout" json:"shutdownTimeout"`
	ComponentTimeout time.Duration `yaml:"component_timeout" json:"componentTimeout"`
	EventConcurrency int           `yaml:"event_concurrency" json:"eventConcurrency"`
}

// HTTP contains REST server listener and timeout configuration.
type HTTP struct {
	Address             string        `yaml:"address" json:"address"`
	ReadTimeout         time.Duration `yaml:"read_timeout" json:"readTimeout"`
	WriteTimeout        time.Duration `yaml:"write_timeout" json:"writeTimeout"`
	IdleTimeout         time.Duration `yaml:"idle_timeout" json:"idleTimeout"`
	BearerTokenFile     string        `yaml:"bearer_token_file" json:"-"`
	MaximumRequestBytes int64         `yaml:"maximum_request_bytes" json:"maximumRequestBytes"`
	MutationRate        int           `yaml:"mutation_rate" json:"mutationRate"`
	MutationBurst       int           `yaml:"mutation_burst" json:"mutationBurst"`
}

// MQTT contains broker connection configuration; it contains no topic policy.
type MQTT struct {
	Enabled             bool          `yaml:"enabled" json:"enabled"`
	SiteID              string        `yaml:"site_id" json:"siteId"`
	Broker              string        `yaml:"broker" json:"broker"`
	ClientID            string        `yaml:"client_id" json:"clientId"`
	Username            string        `yaml:"username" json:"username,omitempty"`
	Password            string        `yaml:"password" json:"password,omitempty"`
	ConnectTimeout      time.Duration `yaml:"connect_timeout" json:"connectTimeout"`
	KeepAlive           time.Duration `yaml:"keep_alive" json:"keepAlive"`
	DisconnectQuiesce   time.Duration `yaml:"disconnect_quiesce" json:"disconnectQuiesce"`
	RequiredForReady    bool          `yaml:"required_for_ready" json:"requiredForReady"`
	MaximumPayload      int           `yaml:"maximum_payload" json:"maximumPayload"`
	QueueCapacity       int           `yaml:"queue_capacity" json:"queueCapacity"`
	IdempotencyCapacity int           `yaml:"idempotency_capacity" json:"idempotencyCapacity"`
	ReconnectMinimum    time.Duration `yaml:"reconnect_minimum" json:"reconnectMinimum"`
	ReconnectMaximum    time.Duration `yaml:"reconnect_maximum" json:"reconnectMaximum"`
	ReconnectJitter     float64       `yaml:"reconnect_jitter" json:"reconnectJitter"`
	HomeAssistant       HomeAssistant `yaml:"home_assistant" json:"homeAssistant"`
}

// HomeAssistant configures the optional, non-authoritative MQTT integration.
type HomeAssistant struct {
	Enabled    bool                     `yaml:"enabled" json:"enabled"`
	CommandTTL time.Duration            `yaml:"command_ttl" json:"commandTtl"`
	Tombstones []HomeAssistantTombstone `yaml:"tombstones" json:"tombstones"`
}

// HomeAssistantTombstone explicitly removes one obsolete retained entity.
type HomeAssistantTombstone struct {
	Component string `yaml:"component" json:"component"`
	ObjectID  string `yaml:"object_id" json:"objectId"`
}

// Simulator configures the hardware-incapable simulator adapter.
type Simulator struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// Adapters contains direct local-LAN hardware boundaries. All adapters are
// disabled by default and cannot coexist with the hardware-incapable simulator.
type Adapters struct {
	Shelly ShellyAdapter `yaml:"shelly" json:"shelly"`
	ESP32  ESP32Adapter  `yaml:"esp32" json:"esp32"`
}

// ShellyAdapter configures Shelly Gen4 endpoints.
type ShellyAdapter struct {
	Enabled   bool             `yaml:"enabled" json:"enabled"`
	Endpoints []ShellyEndpoint `yaml:"endpoints" json:"endpoints"`
}

// ShellyEndpoint maps a typed equipment identity to one local switch channel.
type ShellyEndpoint struct {
	ID                string        `yaml:"id" json:"id"`
	EquipmentID       string        `yaml:"equipment_id" json:"equipmentId"`
	AlarmRuleID       string        `yaml:"alarm_rule_id" json:"alarmRuleId"`
	BaseURL           string        `yaml:"base_url" json:"baseUrl"`
	Channel           int           `yaml:"channel" json:"channel"`
	PollInterval      time.Duration `yaml:"poll_interval" json:"pollInterval"`
	RequestTimeout    time.Duration `yaml:"request_timeout" json:"requestTimeout"`
	Retries           int           `yaml:"retries" json:"retries"`
	SafeOn            bool          `yaml:"safe_on" json:"safeOn"`
	PowerReturnPolicy string        `yaml:"power_return_policy" json:"powerReturnPolicy"`
	EquipmentKind     string        `yaml:"equipment_kind" json:"equipmentKind"`
	MaximumOn         time.Duration `yaml:"maximum_on" json:"maximumOn"`
	RequiredProbeIDs  []string      `yaml:"required_probe_ids" json:"requiredProbeIds"`
}

// ESP32Adapter configures Ethernet/PoE sensor nodes.
type ESP32Adapter struct {
	Enabled   bool            `yaml:"enabled" json:"enabled"`
	Endpoints []ESP32Endpoint `yaml:"endpoints" json:"endpoints"`
}

// ESP32Endpoint maps one node and two probe identities to the v1 wire contract.
type ESP32Endpoint struct {
	ID                string        `yaml:"id" json:"id"`
	DeviceID          string        `yaml:"device_id" json:"deviceId"`
	AlarmRuleID       string        `yaml:"alarm_rule_id" json:"alarmRuleId"`
	BaseURL           string        `yaml:"base_url" json:"baseUrl"`
	BearerTokenFile   string        `yaml:"bearer_token_file" json:"bearerTokenFile,omitempty"`
	ProbeIDs          []string      `yaml:"probe_ids" json:"probeIds"`
	PollInterval      time.Duration `yaml:"poll_interval" json:"pollInterval"`
	RequestTimeout    time.Duration `yaml:"request_timeout" json:"requestTimeout"`
	FreshFor          time.Duration `yaml:"fresh_for" json:"freshFor"`
	MaximumClockSkew  time.Duration `yaml:"maximum_clock_skew" json:"maximumClockSkew"`
	MaximumDifference float64       `yaml:"maximum_difference_celsius" json:"maximumDifferenceCelsius"`
}

// Bench is an explicit activation guard for real Prompt 8 hardware.
type Bench struct {
	Enabled              bool `yaml:"enabled" json:"enabled"`
	SafeLoadAcknowledged bool `yaml:"safe_load_acknowledged" json:"safeLoadAcknowledged"`
}

// Inventory contains declarative identities needed for cross-reference and
// unit validation. Runtime registry behavior belongs to later milestones.
type Inventory struct {
	Devices   []Device    `yaml:"devices" json:"devices"`
	Sensors   []Sensor    `yaml:"sensors" json:"sensors"`
	Equipment []Equipment `yaml:"equipment" json:"equipment"`
}

// Device declares an adapter-owned physical device identity.
type Device struct {
	ID string `yaml:"id" json:"id"`
}

// Sensor declares a generic measurement boundary without sensor business logic.
type Sensor struct {
	ID       string   `yaml:"id" json:"id"`
	DeviceID string   `yaml:"device_id" json:"deviceId"`
	Unit     string   `yaml:"unit" json:"unit"`
	Minimum  *float64 `yaml:"minimum" json:"minimum,omitempty"`
	Maximum  *float64 `yaml:"maximum" json:"maximum,omitempty"`
}

// Equipment declares a generic output identity and its owning device.
type Equipment struct {
	ID       string `yaml:"id" json:"id"`
	DeviceID string `yaml:"device_id" json:"deviceId"`
}

// ValidationError is a stable, machine-inspectable configuration error.
type ValidationError struct {
	Path    string
	Code    string
	Message string
}

// Error formats the stable path before its human-readable explanation.
func (e *ValidationError) Error() string { return e.Path + ": " + e.Message }

// Defaults returns conservative values which cannot reach physical hardware or
// require an external broker. The explicit file remains the source of truth.
func Defaults() Config {
	return Config{
		SchemaVersion: CurrentSchemaVersion,
		Application:   Application{LogLevel: "info", StartupTimeout: 30 * time.Second, ShutdownTimeout: 15 * time.Second, ComponentTimeout: 5 * time.Second, EventConcurrency: 64},
		HTTP:          HTTP{Address: "localhost:8080", ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaximumRequestBytes: 64 * 1024, MutationRate: 10, MutationBurst: 20},
		MQTT:          MQTT{ConnectTimeout: 10 * time.Second, KeepAlive: 30 * time.Second, DisconnectQuiesce: 250 * time.Millisecond, MaximumPayload: 256 * 1024, QueueCapacity: 256, IdempotencyCapacity: 4096, ReconnectMinimum: time.Second, ReconnectMaximum: time.Minute, ReconnectJitter: 0.2, HomeAssistant: HomeAssistant{CommandTTL: 10 * time.Second}},
		Simulator:     Simulator{Enabled: true},
		Storage:       Storage{InfluxDB: InfluxDB{QueueCapacity: 4096, BatchSize: 200, FlushInterval: 5 * time.Second, RetryMinimum: time.Second, RetryMaximum: time.Minute, WriteTimeout: 5 * time.Second}},
	}
}

// Load reads strict YAML, rejects inline secrets, applies documented
// environment overrides, and validates the complete effective snapshot.
func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("configuration file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var envelope struct {
		SchemaVersion *int `yaml:"schema_version"`
	}
	if err := yaml.Unmarshal(data, &envelope); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if envelope.SchemaVersion == nil {
		return Config{}, validationError("schema_version", "required", "is required")
	}
	cfg := Defaults()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if cfg.MQTT.Password != "" {
		return Config{}, validationError("mqtt.password", "secret_inline", "must be supplied through AQUAOS_MQTT_PASSWORD")
	}
	if err := applyEnvironment(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg.Clone(), nil
}

// DecodeCandidate strictly decodes and validates an untrusted candidate
// configuration without applying environment overrides or activating it.
func DecodeCandidate(data []byte) (Config, error) {
	if len(data) == 0 {
		return Config{}, errors.New("candidate configuration is empty")
	}
	if len(data) > 1024*1024 {
		return Config{}, errors.New("candidate configuration exceeds 1048576 bytes")
	}
	var envelope struct {
		SchemaVersion *int `yaml:"schema_version"`
	}
	if err := yaml.Unmarshal(data, &envelope); err != nil {
		return Config{}, fmt.Errorf("decode candidate config: %w", err)
	}
	if envelope.SchemaVersion == nil {
		return Config{}, validationError("schema_version", "required", "is required")
	}
	cfg := Defaults()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode candidate config: %w", err)
	}
	if cfg.MQTT.Password != "" {
		return Config{}, validationError("mqtt.password", "secret_inline", "must not be supplied in candidate configuration")
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg.Clone(), nil
}

func applyEnvironment(cfg *Config) error {
	stringOverrides := []struct {
		name   string
		target *string
	}{
		{"AQUAOS_LOG_LEVEL", &cfg.Application.LogLevel}, {"AQUAOS_HTTP_ADDRESS", &cfg.HTTP.Address},
		{"AQUAOS_MQTT_BROKER", &cfg.MQTT.Broker}, {"AQUAOS_MQTT_CLIENT_ID", &cfg.MQTT.ClientID},
		{"AQUAOS_MQTT_SITE_ID", &cfg.MQTT.SiteID},
		{"AQUAOS_MQTT_USERNAME", &cfg.MQTT.Username}, {"AQUAOS_MQTT_PASSWORD", &cfg.MQTT.Password},
	}
	for _, override := range stringOverrides {
		if value, ok := os.LookupEnv(override.name); ok {
			*override.target = value
		}
	}
	boolOverrides := []struct {
		name   string
		target *bool
	}{
		{"AQUAOS_MQTT_ENABLED", &cfg.MQTT.Enabled},
		{"AQUAOS_MQTT_REQUIRED_FOR_READY", &cfg.MQTT.RequiredForReady},
		{"AQUAOS_SIMULATOR_ENABLED", &cfg.Simulator.Enabled},
	}
	for _, override := range boolOverrides {
		if value, ok := os.LookupEnv(override.name); ok {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return validationError("environment."+override.name, "invalid_boolean", err.Error())
			}
			*override.target = parsed
		}
	}
	return nil
}

// Validate rejects incomplete, ambiguous, or unsafe operational configuration.
func (c Config) Validate() error {
	if c.SchemaVersion != CurrentSchemaVersion {
		return validationError("schema_version", "unsupported_version", fmt.Sprintf("must equal %d", CurrentSchemaVersion))
	}
	if !oneOf(strings.ToLower(c.Application.LogLevel), "debug", "info", "warn", "error") {
		return validationError("application.log_level", "invalid_enum", "must be debug, info, warn, or error")
	}
	for path, value := range map[string]time.Duration{
		"application.startup_timeout":   c.Application.StartupTimeout,
		"application.shutdown_timeout":  c.Application.ShutdownTimeout,
		"application.component_timeout": c.Application.ComponentTimeout,
		"http.read_timeout":             c.HTTP.ReadTimeout, "http.write_timeout": c.HTTP.WriteTimeout, "http.idle_timeout": c.HTTP.IdleTimeout,
	} {
		if value <= 0 || value > 10*time.Minute {
			return validationError(path, "out_of_range", "must be greater than zero and at most 10m")
		}
	}
	if strings.TrimSpace(c.HTTP.Address) == "" {
		return validationError("http.address", "required", "is required")
	}
	host, _, err := net.SplitHostPort(c.HTTP.Address)
	if err != nil {
		return validationError("http.address", "invalid_address", "must contain a host and port")
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if !loopback && c.HTTP.BearerTokenFile == "" {
		return validationError("http.bearer_token_file", "required", "is required for a non-loopback listener")
	}
	if c.HTTP.MaximumRequestBytes < 1024 || c.HTTP.MaximumRequestBytes > 1024*1024 {
		return validationError("http.maximum_request_bytes", "out_of_range", "must be between 1024 and 1048576")
	}
	if c.HTTP.MutationRate < 1 || c.HTTP.MutationRate > 1000 || c.HTTP.MutationBurst < 1 || c.HTTP.MutationBurst > 1000 {
		return validationError("http.mutation_rate", "out_of_range", "mutation rate and burst must be between 1 and 1000")
	}
	if c.Application.EventConcurrency < 1 || c.Application.EventConcurrency > 4096 {
		return validationError("application.event_concurrency", "out_of_range", "must be between 1 and 4096")
	}
	if c.MQTT.Enabled {
		if !identifierPattern.MatchString(c.MQTT.SiteID) {
			return validationError("mqtt.site_id", "invalid_id", "must be lowercase kebab-case when MQTT is enabled")
		}
		if c.MQTT.Broker == "" {
			return validationError("mqtt.broker", "required", "is required when MQTT is enabled")
		}
		parsed, err := url.Parse(c.MQTT.Broker)
		if err != nil || (parsed.Scheme != "tcp" && parsed.Scheme != "ssl") || parsed.Host == "" {
			return validationError("mqtt.broker", "invalid_uri", "must be a tcp:// or ssl:// URI with a host")
		}
		if parsed.User != nil {
			return validationError("mqtt.broker", "secret_inline", "must not contain credentials; use explicit environment overrides")
		}
		if c.MQTT.ClientID == "" {
			return validationError("mqtt.client_id", "required", "is required when MQTT is enabled")
		}
		if c.MQTT.ConnectTimeout <= 0 || c.MQTT.ConnectTimeout > time.Minute {
			return validationError("mqtt.connect_timeout", "out_of_range", "must be greater than zero and at most 1m")
		}
		if c.MQTT.KeepAlive < time.Second || c.MQTT.KeepAlive > 10*time.Minute {
			return validationError("mqtt.keep_alive", "out_of_range", "must be between 1s and 10m")
		}
		if c.MQTT.MaximumPayload < 256 || c.MQTT.MaximumPayload > 16*1024*1024 {
			return validationError("mqtt.maximum_payload", "out_of_range", "must be between 256 and 16777216 bytes")
		}
		if c.MQTT.QueueCapacity < 1 || c.MQTT.QueueCapacity > 65536 || c.MQTT.IdempotencyCapacity < 1 || c.MQTT.IdempotencyCapacity > 1000000 {
			return validationError("mqtt.queue_capacity", "out_of_range", "queue and idempotency capacities are outside safe bounds")
		}
		if c.MQTT.ReconnectMinimum <= 0 || c.MQTT.ReconnectMaximum < c.MQTT.ReconnectMinimum || c.MQTT.ReconnectMaximum > 10*time.Minute {
			return validationError("mqtt.reconnect_maximum", "out_of_range", "reconnect bounds are invalid")
		}
		if c.MQTT.ReconnectJitter < 0 || c.MQTT.ReconnectJitter > 0.5 {
			return validationError("mqtt.reconnect_jitter", "out_of_range", "must be between 0 and 0.5")
		}
	}
	if c.MQTT.HomeAssistant.Enabled && !c.MQTT.Enabled {
		return validationError("mqtt.home_assistant.enabled", "dependency_disabled", "requires MQTT to be enabled")
	}
	if c.MQTT.HomeAssistant.CommandTTL <= 0 || c.MQTT.HomeAssistant.CommandTTL > time.Minute {
		return validationError("mqtt.home_assistant.command_ttl", "out_of_range", "must be greater than zero and at most 1m")
	}
	storage := c.Storage.InfluxDB
	if storage.QueueCapacity < 1 || storage.QueueCapacity > 1_000_000 || storage.BatchSize < 1 || storage.BatchSize > storage.QueueCapacity {
		return validationError("storage.influxdb.queue_capacity", "out_of_range", "queue and batch bounds are invalid")
	}
	if storage.FlushInterval <= 0 || storage.RetryMinimum <= 0 || storage.RetryMaximum < storage.RetryMinimum || storage.RetryMaximum > 10*time.Minute || storage.WriteTimeout <= 0 || storage.WriteTimeout > time.Minute {
		return validationError("storage.influxdb.retry_maximum", "out_of_range", "storage timing bounds are invalid")
	}
	if storage.Enabled {
		parsed, err := url.Parse(storage.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return validationError("storage.influxdb.url", "invalid_uri", "must be an HTTP or HTTPS origin")
		}
		if parsed.User != nil {
			return validationError("storage.influxdb.url", "secret_inline", "must not contain credentials")
		}
		if storage.Organization == "" || storage.Bucket == "" || storage.TokenFile == "" {
			return validationError("storage.influxdb", "required", "organization, bucket, and token_file are required when enabled")
		}
	}
	for index, item := range c.MQTT.HomeAssistant.Tombstones {
		if !haIdentifierPattern.MatchString(item.Component) || !identifierPattern.MatchString(item.ObjectID) {
			return validationError(fmt.Sprintf("mqtt.home_assistant.tombstones[%d]", index), "invalid_id", "component or object_id is invalid")
		}
	}
	if err := c.validateAdapters(); err != nil {
		return err
	}
	return c.Inventory.validate()
}

func (c Config) validateAdapters() error {
	realEnabled := c.Adapters.Shelly.Enabled || c.Adapters.ESP32.Enabled
	if realEnabled && c.Simulator.Enabled {
		return validationError("simulator.enabled", "adapter_conflict", "must be false when a real adapter is enabled")
	}
	if realEnabled && (!c.Bench.Enabled || !c.Bench.SafeLoadAcknowledged) {
		return validationError("bench.safe_load_acknowledged", "activation_guard", "must be true with bench.enabled before real adapters can start")
	}
	if c.Adapters.Shelly.Enabled && len(c.Adapters.Shelly.Endpoints) == 0 {
		return validationError("adapters.shelly.endpoints", "required", "must contain at least one endpoint when enabled")
	}
	seenEndpoints := make(map[string]struct{})
	seenEquipment := make(map[string]struct{})
	for index, endpoint := range c.Adapters.Shelly.Endpoints {
		base := fmt.Sprintf("adapters.shelly.endpoints[%d]", index)
		for path, value := range map[string]string{"id": endpoint.ID, "equipment_id": endpoint.EquipmentID, "alarm_rule_id": endpoint.AlarmRuleID} {
			if err := validateUUID(base+"."+path, value); err != nil {
				return err
			}
		}
		if _, exists := seenEndpoints[endpoint.ID]; exists {
			return validationError(base+".id", "duplicate_id", "duplicates endpoint "+endpoint.ID)
		}
		seenEndpoints[endpoint.ID] = struct{}{}
		if _, exists := seenEquipment[endpoint.EquipmentID]; exists {
			return validationError(base+".equipment_id", "duplicate_id", "equipment has more than one owning adapter")
		}
		seenEquipment[endpoint.EquipmentID] = struct{}{}
		parsed, err := url.Parse(endpoint.BaseURL)
		if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return validationError(base+".base_url", "invalid_uri", "must be an unauthenticated http URL with a host")
		}
		if endpoint.Channel < 0 || endpoint.Channel > 31 {
			return validationError(base+".channel", "out_of_range", "must be between 0 and 31")
		}
		if endpoint.PollInterval <= 0 || endpoint.RequestTimeout <= 0 || endpoint.RequestTimeout > endpoint.PollInterval {
			return validationError(base+".request_timeout", "out_of_range", "must be positive and no greater than poll_interval")
		}
		if endpoint.Retries < 0 || endpoint.Retries > 5 {
			return validationError(base+".retries", "out_of_range", "must be between 0 and 5")
		}
		if endpoint.PowerReturnPolicy != "off" && endpoint.PowerReturnPolicy != "restore" {
			return validationError(base+".power_return_policy", "invalid_enum", "must be off or restore")
		}
		if endpoint.EquipmentKind != "outlet" && endpoint.EquipmentKind != "heater" {
			return validationError(base+".equipment_kind", "invalid_enum", "must be outlet or heater")
		}
		if endpoint.EquipmentKind == "heater" {
			if endpoint.SafeOn {
				return validationError(base+".safe_on", "unsafe_value", "must be false for heater equipment")
			}
			if endpoint.MaximumOn <= 0 {
				return validationError(base+".maximum_on", "out_of_range", "must be positive for heater equipment")
			}
			if len(endpoint.RequiredProbeIDs) != 2 {
				return validationError(base+".required_probe_ids", "invalid_count", "must contain both independent probe IDs for heater equipment")
			}
			for probeIndex, probeID := range endpoint.RequiredProbeIDs {
				if err := validateUUID(fmt.Sprintf("%s.required_probe_ids[%d]", base, probeIndex), probeID); err != nil {
					return err
				}
			}
			if endpoint.RequiredProbeIDs[0] == endpoint.RequiredProbeIDs[1] {
				return validationError(base+".required_probe_ids", "duplicate_id", "heater probes must be independent")
			}
		} else if endpoint.MaximumOn < 0 || len(endpoint.RequiredProbeIDs) != 0 {
			return validationError(base+".maximum_on", "invalid_combination", "outlet equipment cannot declare heater probe requirements and maximum_on cannot be negative")
		}
	}
	if c.Adapters.ESP32.Enabled && len(c.Adapters.ESP32.Endpoints) == 0 {
		return validationError("adapters.esp32.endpoints", "required", "must contain at least one endpoint when enabled")
	}
	seenDevices := make(map[string]struct{})
	seenProbes := make(map[string]struct{})
	for index, endpoint := range c.Adapters.ESP32.Endpoints {
		base := fmt.Sprintf("adapters.esp32.endpoints[%d]", index)
		for path, value := range map[string]string{"id": endpoint.ID, "device_id": endpoint.DeviceID, "alarm_rule_id": endpoint.AlarmRuleID} {
			if err := validateUUID(base+"."+path, value); err != nil {
				return err
			}
		}
		if _, exists := seenEndpoints[endpoint.ID]; exists {
			return validationError(base+".id", "duplicate_id", "duplicates endpoint "+endpoint.ID)
		}
		seenEndpoints[endpoint.ID] = struct{}{}
		if _, exists := seenDevices[endpoint.DeviceID]; exists {
			return validationError(base+".device_id", "duplicate_id", "device has more than one node endpoint")
		}
		seenDevices[endpoint.DeviceID] = struct{}{}
		parsed, err := url.Parse(endpoint.BaseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return validationError(base+".base_url", "invalid_uri", "must be an http or https URL with a host and no credentials")
		}
		if len(endpoint.ProbeIDs) != 2 {
			return validationError(base+".probe_ids", "invalid_count", "must contain exactly two probe IDs")
		}
		for probeIndex, probeID := range endpoint.ProbeIDs {
			if err := validateUUID(fmt.Sprintf("%s.probe_ids[%d]", base, probeIndex), probeID); err != nil {
				return err
			}
			if _, exists := seenProbes[probeID]; exists {
				return validationError(base+".probe_ids", "duplicate_id", "probe identity is reused")
			}
			seenProbes[probeID] = struct{}{}
		}
		if endpoint.PollInterval <= 0 || endpoint.RequestTimeout <= 0 || endpoint.RequestTimeout > endpoint.PollInterval || endpoint.FreshFor < endpoint.PollInterval {
			return validationError(base+".fresh_for", "out_of_range", "poll, timeout, and freshness bounds are invalid")
		}
		if endpoint.MaximumClockSkew < 0 || endpoint.MaximumClockSkew > time.Minute {
			return validationError(base+".maximum_clock_skew", "out_of_range", "must be between zero and 1m")
		}
		if endpoint.MaximumDifference <= 0 || endpoint.MaximumDifference > 10 {
			return validationError(base+".maximum_difference_celsius", "out_of_range", "must be greater than zero and at most 10")
		}
	}
	for index, endpoint := range c.Adapters.Shelly.Endpoints {
		if endpoint.EquipmentKind != "heater" || !c.Adapters.Shelly.Enabled {
			continue
		}
		path := fmt.Sprintf("adapters.shelly.endpoints[%d].required_probe_ids", index)
		if !c.Adapters.ESP32.Enabled {
			return validationError(path, "adapter_dependency", "heater bench equipment requires the direct ESP32 adapter")
		}
		for _, probeID := range endpoint.RequiredProbeIDs {
			if _, exists := seenProbes[probeID]; !exists {
				return validationError(path, "invalid_reference", "references an unknown ESP32 probe "+probeID)
			}
		}
	}
	return nil
}

func validateUUID(path, value string) error {
	if err := domain.EntityID(value).Validate(); err != nil {
		return validationError(path, "invalid_uuid", err.Error())
	}
	return nil
}

func (i Inventory) validate() error {
	devices := make(map[string]struct{}, len(i.Devices))
	for index, device := range i.Devices {
		path := fmt.Sprintf("inventory.devices[%d].id", index)
		if err := validateID(path, device.ID); err != nil {
			return err
		}
		if _, exists := devices[device.ID]; exists {
			return validationError(path, "duplicate_id", "duplicates "+device.ID)
		}
		devices[device.ID] = struct{}{}
	}
	seen := make(map[string]string, len(i.Sensors)+len(i.Equipment))
	for index, sensor := range i.Sensors {
		base := fmt.Sprintf("inventory.sensors[%d]", index)
		if err := validateID(base+".id", sensor.ID); err != nil {
			return err
		}
		if previous, exists := seen[sensor.ID]; exists {
			return validationError(base+".id", "duplicate_id", "duplicates "+previous)
		}
		seen[sensor.ID] = base + ".id"
		if _, exists := devices[sensor.DeviceID]; !exists {
			return validationError(base+".device_id", "invalid_reference", "references unknown device "+sensor.DeviceID)
		}
		if !oneOf(sensor.Unit, "celsius", "fahrenheit", "pH", "ppt", "liters_per_hour", "boolean") {
			return validationError(base+".unit", "unsupported_unit", "is not a supported canonical unit")
		}
		if sensor.Minimum != nil && (math.IsNaN(*sensor.Minimum) || math.IsInf(*sensor.Minimum, 0)) {
			return validationError(base+".minimum", "out_of_range", "must be finite")
		}
		if sensor.Maximum != nil && (math.IsNaN(*sensor.Maximum) || math.IsInf(*sensor.Maximum, 0)) {
			return validationError(base+".maximum", "out_of_range", "must be finite")
		}
		if sensor.Minimum != nil && sensor.Maximum != nil && *sensor.Minimum >= *sensor.Maximum {
			return validationError(base+".maximum", "out_of_range", "must be greater than minimum")
		}
	}
	for index, equipment := range i.Equipment {
		base := fmt.Sprintf("inventory.equipment[%d]", index)
		if err := validateID(base+".id", equipment.ID); err != nil {
			return err
		}
		if previous, exists := seen[equipment.ID]; exists {
			return validationError(base+".id", "duplicate_id", "duplicates "+previous)
		}
		seen[equipment.ID] = base + ".id"
		if _, exists := devices[equipment.DeviceID]; !exists {
			return validationError(base+".device_id", "invalid_reference", "references unknown device "+equipment.DeviceID)
		}
	}
	return nil
}

func validateID(path, id string) error {
	if !identifierPattern.MatchString(id) {
		return validationError(path, "invalid_id", "must match ^[a-z][a-z0-9-]{0,62}$")
	}
	return nil
}

func validationError(path, code, message string) error {
	return &ValidationError{Path: path, Code: code, Message: message}
}
func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// Redacted returns a deep copy with secret values removed.
func (c Config) Redacted() Config {
	redacted := c.Clone()
	if redacted.MQTT.Password != "" {
		redacted.MQTT.Password = redactedSecret
	}
	return redacted
}

// Digest returns a stable SHA-256 digest of the redacted effective snapshot.
func (c Config) Digest() (string, error) {
	encoded, err := json.Marshal(c.Redacted())
	if err != nil {
		return "", fmt.Errorf("encode configuration digest: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// Clone returns a deep copy safe for use as an immutable snapshot.
func (c Config) Clone() Config {
	cloned := c
	cloned.MQTT.HomeAssistant.Tombstones = append([]HomeAssistantTombstone(nil), c.MQTT.HomeAssistant.Tombstones...)
	cloned.Inventory.Devices = append([]Device(nil), c.Inventory.Devices...)
	cloned.Inventory.Sensors = append([]Sensor(nil), c.Inventory.Sensors...)
	for index := range cloned.Inventory.Sensors {
		if c.Inventory.Sensors[index].Minimum != nil {
			value := *c.Inventory.Sensors[index].Minimum
			cloned.Inventory.Sensors[index].Minimum = &value
		}
		if c.Inventory.Sensors[index].Maximum != nil {
			value := *c.Inventory.Sensors[index].Maximum
			cloned.Inventory.Sensors[index].Maximum = &value
		}
	}
	cloned.Inventory.Equipment = append([]Equipment(nil), c.Inventory.Equipment...)
	cloned.Adapters.Shelly.Endpoints = append([]ShellyEndpoint(nil), c.Adapters.Shelly.Endpoints...)
	for index := range cloned.Adapters.Shelly.Endpoints {
		cloned.Adapters.Shelly.Endpoints[index].RequiredProbeIDs = append([]string(nil), c.Adapters.Shelly.Endpoints[index].RequiredProbeIDs...)
	}
	cloned.Adapters.ESP32.Endpoints = append([]ESP32Endpoint(nil), c.Adapters.ESP32.Endpoints...)
	for index := range cloned.Adapters.ESP32.Endpoints {
		cloned.Adapters.ESP32.Endpoints[index].ProbeIDs = append([]string(nil), c.Adapters.ESP32.Endpoints[index].ProbeIDs...)
	}
	return cloned
}
