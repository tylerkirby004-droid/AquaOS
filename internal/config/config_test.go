package config

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/events"
)

const validYAML = `schema_version: 1
application:
  log_level: info
  startup_timeout: 20s
  shutdown_timeout: 15s
  component_timeout: 3s
http:
  address: "localhost:8080"
  read_timeout: 5s
  write_timeout: 10s
  idle_timeout: 60s
mqtt:
  enabled: true
  site_id: test-reef
  broker: tcp://localhost:1883
  client_id: aquaos-test
  connect_timeout: 10s
  keep_alive: 30s
  disconnect_quiesce: 250ms
  required_for_ready: true
simulator:
  enabled: false
inventory:
  devices:
    - id: controller-1
  sensors:
    - id: water-temperature
      device_id: controller-1
      unit: celsius
      minimum: 0
      maximum: 50
  equipment:
    - id: return-pump
      device_id: controller-1
`

func TestLoadAppliesExplicitEnvironmentOverrides(t *testing.T) {
	t.Setenv("AQUAOS_HTTP_ADDRESS", "localhost:9090")
	t.Setenv("AQUAOS_MQTT_PASSWORD", "secret")
	cfg, err := Load(writeConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Address != "localhost:9090" || cfg.MQTT.Password != "secret" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadRejectsUnknownFieldAndMissingVersion(t *testing.T) {
	if _, err := Load(writeConfig(t, strings.Replace(validYAML, "schema_version: 1\n", "", 1))); validationCode(err) != "required" {
		t.Fatalf("missing version error = %v", err)
	}
	if _, err := Load(writeConfig(t, validYAML+"unknown: true\n")); err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestLoadRejectsInlineSecretAndRedactsEnvironmentSecret(t *testing.T) {
	inline := strings.Replace(validYAML, "  client_id: aquaos-test\n", "  client_id: aquaos-test\n  password: exposed\n", 1)
	if _, err := Load(writeConfig(t, inline)); validationCode(err) != "secret_inline" {
		t.Fatalf("inline secret error = %v", err)
	}
	t.Setenv("AQUAOS_MQTT_PASSWORD", "environment-secret")
	cfg, err := Load(writeConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	encoded := cfg.Redacted().MQTT.Password
	if encoded != redactedSecret || strings.Contains(encoded, "environment-secret") {
		t.Fatalf("redacted password = %q", encoded)
	}
}

func TestLoadRejectsCredentialsEmbeddedInBrokerURI(t *testing.T) {
	contents := strings.Replace(validYAML, "tcp://localhost:1883", "tcp://user:secret@localhost:1883", 1)
	if _, err := Load(writeConfig(t, contents)); validationCode(err) != "secret_inline" {
		t.Fatalf("broker credential error = %v", err)
	}
}

func TestValidationStablePaths(t *testing.T) {
	tests := []struct{ name, replace, with, path, code string }{
		{"duplicate ID", "    - id: controller-1\n  sensors:", "    - id: controller-1\n    - id: controller-1\n  sensors:", "inventory.devices[1].id", "duplicate_id"},
		{"invalid reference", "device_id: controller-1", "device_id: missing-device", "inventory.sensors[0].device_id", "invalid_reference"},
		{"unsupported unit", "unit: celsius", "unit: kelvins-ish", "inventory.sensors[0].unit", "unsupported_unit"},
		{"invalid range", "maximum: 50", "maximum: 0", "inventory.sensors[0].maximum", "out_of_range"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, strings.Replace(validYAML, test.replace, test.with, 1)))
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Path != test.path || validation.Code != test.code {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestValidateAllowsBrokerFreeSafeDefaults(t *testing.T) {
	cfg := Defaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected defaults: %v", err)
	}
}

func TestNonLoopbackHTTPRequiresExternalCredential(t *testing.T) {
	cfg := Defaults()
	cfg.HTTP.Address = "0.0.0.0:8080"
	if err := cfg.Validate(); err == nil {
		t.Fatal("non-loopback listener without credentials validated")
	}
	cfg.HTTP.BearerTokenFile = "/etc/aquaos/secrets/api.token"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("credentialed listener: %v", err)
	}
}

func TestDecodeCandidateRejectsInlineSecret(t *testing.T) {
	payload := []byte("schema_version: 1\nmqtt:\n  password: forbidden\n")
	if _, err := DecodeCandidate(payload); err == nil {
		t.Fatal("candidate inline secret was accepted")
	}
}

func TestDigestIsStableAndSecretIndependent(t *testing.T) {
	cfg := Defaults()
	cfg.MQTT.Password = "one"
	one, err := cfg.Digest()
	if err != nil {
		t.Fatal(err)
	}
	cfg.MQTT.Password = "two"
	two, err := cfg.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if one != two || len(one) != 64 {
		t.Fatalf("digests = %q, %q", one, two)
	}
}

func TestCurrentReturnsDeeplyImmutableSnapshot(t *testing.T) {
	initial, err := Load(writeConfig(t, validYAML))
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(initial, nil, nil, discardLogger())
	snapshot := manager.Current()
	snapshot.Inventory.Sensors[0].ID = "changed"
	*snapshot.Inventory.Sensors[0].Minimum = 99
	current := manager.Current()
	if current.Inventory.Sensors[0].ID == "changed" || *current.Inventory.Sensors[0].Minimum == 99 {
		t.Fatal("Current() exposed mutable snapshot data")
	}
}

func TestManagerReloadIsAtomic(t *testing.T) {
	initial := Defaults()
	manager := NewManager(initial, staticLoader{config: func() Config { next := initial.Clone(); next.HTTP.Address = "localhost:9090"; return next }()}, events.NewBus(), discardLogger())
	oldDigest := manager.Digest()
	err := manager.Reload(context.Background())
	var rejected *ReloadRejectedError
	if !errors.As(err, &rejected) || manager.Current().HTTP.Address != initial.HTTP.Address || manager.Digest() != oldDigest {
		t.Fatalf("unsafe reload was not atomic: err=%v", err)
	}
}

func TestManagerReloadsHarmlessSettingAndPublishesDigest(t *testing.T) {
	initial := Defaults()
	next := initial.Clone()
	next.Application.LogLevel = "debug"
	bus := events.NewBus()
	received := false
	_, _ = bus.Subscribe(events.ConfigurationChanged, func(context.Context, events.Event) error { received = true; return nil })
	setter := &recordingLevelSetter{}
	manager := NewManager(initial, staticLoader{config: next}, bus, discardLogger(), WithLogLevelSetter(setter))
	if err := manager.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if manager.Current().Application.LogLevel != "debug" || setter.level != "debug" || !received {
		t.Fatal("harmless reload was not committed, applied, and audited")
	}
}

func TestManagerPreservesSnapshotOnLoadFailure(t *testing.T) {
	initial := Defaults()
	manager := NewManager(initial, staticLoader{err: errors.New("broken")}, events.NewBus(), discardLogger())
	if err := manager.Reload(context.Background()); err == nil {
		t.Fatal("Reload() expected error")
	}
	if manager.Current().Application.LogLevel != initial.Application.LogLevel {
		t.Fatal("failed reload replaced snapshot")
	}
}

func TestManagerRollsBackSnapshotAndLiveSettingOnAuditFailure(t *testing.T) {
	initial := Defaults()
	next := initial.Clone()
	next.Application.LogLevel = "debug"
	setter := &recordingLevelSetter{level: initial.Application.LogLevel}
	manager := NewManager(initial, staticLoader{config: next}, failingPublisher{}, discardLogger(), WithLogLevelSetter(setter))
	oldDigest := manager.Digest()
	if err := manager.Reload(context.Background()); err == nil {
		t.Fatal("Reload() expected audit failure")
	}
	if manager.Current().Application.LogLevel != initial.Application.LogLevel || manager.Digest() != oldDigest || setter.level != initial.Application.LogLevel {
		t.Fatal("audit failure did not roll back atomically")
	}
}

func TestLoadRejectsInvalidEnvironmentValue(t *testing.T) {
	t.Setenv("AQUAOS_MQTT_ENABLED", "occasionally")
	if _, err := Load(writeConfig(t, validYAML)); validationCode(err) != "invalid_boolean" {
		t.Fatalf("error = %v", err)
	}
}

func TestRealAdaptersRequireExplicitBenchActivationAndTypedBounds(t *testing.T) {
	cfg := Defaults()
	cfg.Simulator.Enabled = false
	cfg.Adapters.Shelly.Enabled = true
	cfg.Adapters.Shelly.Endpoints = []ShellyEndpoint{{
		ID: "11111111-1111-4111-8111-111111111111", EquipmentID: "22222222-2222-4222-8222-222222222222", AlarmRuleID: "33333333-3333-4333-8333-333333333333",
		BaseURL: "http://shelly.local", Channel: 0, PollInterval: time.Second, RequestTimeout: 100 * time.Millisecond, Retries: 1, PowerReturnPolicy: "off", EquipmentKind: "outlet",
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("real adapter started without explicit bench acknowledgement")
	}
	cfg.Bench = Bench{Enabled: true, SafeLoadAcknowledged: true}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Simulator.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("simulator and real adapter conflict was accepted")
	}
}

func TestAdapterChangesCannotHotReload(t *testing.T) {
	current := Defaults()
	manager := NewManager(current, nil, nil, discardLogger())
	next := current.Clone()
	next.Bench.Enabled = true
	plan, err := manager.Plan(next)
	if err == nil {
		t.Fatal("bench activation unexpectedly hot reloaded")
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Path != "bench" || plan.Changes[0].Reloadable {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestCheckedInBenchExampleIsSafeAndValid(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "configs", "bench.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Adapters.Shelly.Enabled || cfg.Adapters.ESP32.Enabled || cfg.Bench.SafeLoadAcknowledged || cfg.Simulator.Enabled {
		t.Fatalf("bench example activates hardware or simulator unexpectedly: %+v", cfg)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aquaos.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func validationCode(err error) string {
	var target *ValidationError
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}
func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type staticLoader struct {
	config Config
	err    error
}

func (l staticLoader) Load(context.Context) (Config, error) { return l.config.Clone(), l.err }

type recordingLevelSetter struct{ level string }

func (s *recordingLevelSetter) SetLogLevel(level string) { s.level = level }

type failingPublisher struct{}

func (failingPublisher) Publish(context.Context, events.Event) error {
	return errors.New("audit unavailable")
}
