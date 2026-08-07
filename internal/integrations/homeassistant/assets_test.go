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
	payload, err := Dashboard(cfg, "http://appliance.local:3000")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"sensor.aquaos_sensor_tank_temperature", "switch.aquaos_equipment_return_pump", "binary_sensor.aquaos_display_reef_alarm", "aquaos-overview"} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("dashboard does not contain %q", expected)
		}
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

func TestDashboardRejectsNonHTTPGrafanaURL(t *testing.T) {
	if _, err := Dashboard(config.Defaults(), "file:///etc/passwd"); err == nil {
		t.Fatal("unsafe Grafana URL accepted")
	}
}
