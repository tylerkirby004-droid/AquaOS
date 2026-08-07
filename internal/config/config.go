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
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// CurrentSchemaVersion is the only configuration schema accepted by this release.
	CurrentSchemaVersion = 1
	redactedSecret       = "[REDACTED]"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// Config is the complete externally supplied AquaOS configuration.
type Config struct {
	SchemaVersion int         `yaml:"schema_version" json:"schemaVersion"`
	Application   Application `yaml:"application" json:"application"`
	HTTP          HTTP        `yaml:"http" json:"http"`
	MQTT          MQTT        `yaml:"mqtt" json:"mqtt"`
	Simulator     Simulator   `yaml:"simulator" json:"simulator"`
	Inventory     Inventory   `yaml:"inventory" json:"inventory"`
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
	Address      string        `yaml:"address" json:"address"`
	ReadTimeout  time.Duration `yaml:"read_timeout" json:"readTimeout"`
	WriteTimeout time.Duration `yaml:"write_timeout" json:"writeTimeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout" json:"idleTimeout"`
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
}

// Simulator configures the hardware-incapable simulator adapter.
type Simulator struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
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
		HTTP:          HTTP{Address: "localhost:8080", ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second},
		MQTT:          MQTT{ConnectTimeout: 10 * time.Second, KeepAlive: 30 * time.Second, DisconnectQuiesce: 250 * time.Millisecond, MaximumPayload: 256 * 1024, QueueCapacity: 256, IdempotencyCapacity: 4096, ReconnectMinimum: time.Second, ReconnectMaximum: time.Minute, ReconnectJitter: 0.2},
		Simulator:     Simulator{Enabled: true},
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
	return c.Inventory.validate()
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
	return cloned
}
