package api

import (
	"os"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIContainsEveryV1Operation(t *testing.T) {
	payload, err := os.ReadFile("../../docs/openapi-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		OpenAPI string                    `yaml:"openapi"`
		Paths   map[string]map[string]any `yaml:"paths"`
	}
	if err = yaml.Unmarshal(payload, &document); err != nil {
		t.Fatal(err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("OpenAPI version = %q", document.OpenAPI)
	}
	actual := make([]string, 0)
	for path, methods := range document.Paths {
		for method := range methods {
			actual = append(actual, method+" "+path)
		}
	}
	sort.Strings(actual)
	expected := []string{"get /alarms", "get /commands/{id}", "get /devices", "get /devices/{id}", "get /diagnostics", "get /equipment/{id}/state", "get /health", "get /sensors/{id}/state", "get /state", "get /system", "post /alarms/{id}/ack", "post /config/reload", "post /config/validate", "post /equipment/{id}/commands"}
	sort.Strings(expected)
	if len(actual) != len(expected) {
		t.Fatalf("operations = %v, want %v", actual, expected)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			t.Fatalf("operations = %v, want %v", actual, expected)
		}
	}
}
