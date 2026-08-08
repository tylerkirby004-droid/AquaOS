package homeassistant

import (
	"strings"
	"testing"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"gopkg.in/yaml.v3"
)

func TestDashboardUsesStableDiscoveryEntities(t *testing.T) {
	cfg := config.Defaults()
	cfg.MQTT.SiteID = "display-reef"
	cfg.Inventory.Sensors = []config.Sensor{{EntityID: "tank-temperature"}}
	cfg.Inventory.Equipment = []config.Equipment{{EntityID: "return-pump"}}
	payload, err := Dashboard(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"sensor.aquaos_sensor_tank_temperature", "switch.aquaos_equipment_return_pump", "binary_sensor.aquaos_display_reef_alarm", "aquaos-overview"} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("dashboard does not contain %q", expected)
		}
	}
}

func TestStandardDashboardUsesHomeAssistantHistoryWithoutGrafana(t *testing.T) {
	cfg := config.Defaults()
	cfg.Inventory.Sensors = []config.Sensor{{EntityID: "temperature"}}
	payload, err := Dashboard(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "type: history-graph") || strings.Contains(string(payload), "type: iframe") {
		t.Fatalf("standard dashboard is not Home Assistant-only:\n%s", payload)
	}
}

func TestEmptyDashboardIsValidYAML(t *testing.T) {
	payload, err := Dashboard(config.Defaults(), "https://grafana.local")
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err = yaml.Unmarshal(payload, &document); err != nil {
		t.Fatalf("invalid dashboard YAML: %v\n%s", err, payload)
	}
}

func TestDashboardIgnoresLegacyGrafanaURL(t *testing.T) {
	payload, err := Dashboard(config.Defaults(), "file:///etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "iframe") || strings.Contains(string(payload), "grafana") {
		t.Fatalf("dashboard still renders Grafana content:\n%s", payload)
	}
}
