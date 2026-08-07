package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOptionalServicesComposeUsesCurrentAquaOSPipeline(t *testing.T) {
	payload, err := os.ReadFile("../../infrastructure/docker/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Services map[string]struct {
			Profiles []string `yaml:"profiles"`
		} `yaml:"services"`
	}
	if err = yaml.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"mosquitto", "influxdb", "grafana"} { //nolint:misspell // Mosquitto is the broker product name.
		if _, exists := document.Services[required]; !exists {
			t.Fatalf("required optional service %q missing", required)
		}
	}
	for _, obsolete := range []string{"telegraf", "telemetry-simulator"} {
		if _, exists := document.Services[obsolete]; exists {
			t.Fatalf("obsolete pre-v1 pipeline %q is enabled", obsolete)
		}
	}
	if _, exists := document.Services["nodered"]; exists {
		t.Fatal("Node-RED must not be part of the standard services stack")
	}
}

func TestNodeREDIsDefinedOnlyAsAnAdvancedAddOn(t *testing.T) {
	payload, err := os.ReadFile("../../infrastructure/docker/compose.nodered.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Services map[string]any `yaml:"services"`
	}
	if err = yaml.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	if _, exists := document.Services["nodered"]; !exists {
		t.Fatal("advanced Node-RED add-on definition is missing")
	}
}

func TestProvisionedDashboardUsesVersionedStorageMeasurements(t *testing.T) {
	payload, err := os.ReadFile("../../infrastructure/docker/grafana/provisioning/dashboards/json/aquaos-overview.json")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	contents := string(payload)
	for _, measurement := range []string{"aquaos_measurements_v1", "aquaos_equipment_state_v1", "aquaos_alarms_v1"} {
		if !strings.Contains(contents, measurement) {
			t.Fatalf("dashboard omits %q", measurement)
		}
	}
	if strings.Contains(contents, "aquarium_sensor") {
		t.Fatal("dashboard still references obsolete pre-v1 measurement")
	}
}
