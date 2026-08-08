package app

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHomeAssistantAddonImagesAreDistinct(t *testing.T) {
	core := readAddonManifest(t, "../../aquaos/config.yaml")
	if core.Image != "ghcr.io/tylerkirby004-droid/aquaos-addon" {
		t.Fatalf("core add-on image = %q", core.Image)
	}
	if _, err := os.Stat("../../aquaos-history/config.yaml"); !os.IsNotExist(err) {
		t.Fatal("Grafana Advanced Trends add-on manifest must not be installable")
	}
}

func TestCoreAddonVersionMatchesRuntimeVersion(t *testing.T) {
	core := readAddonManifest(t, "../../aquaos/config.yaml")
	runScript, err := os.ReadFile("../../aquaos/run.sh")
	if err != nil {
		t.Fatal(err)
	}
	if core.Version == "" || !strings.Contains(string(runScript), "'"+core.Version+"' > /var/lib/aquaos/current-version") {
		t.Fatalf("core add-on version %q is not written by run.sh", core.Version)
	}
}

func TestHomeAssistantWorkflowPublishesManifestImages(t *testing.T) {
	core := readAddonManifest(t, "../../aquaos/config.yaml")
	workflow, err := os.ReadFile("../../.github/workflows/home-assistant-app.yml")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(workflow)
	for _, required := range []string{
		"BUILD_VERSION=" + core.Version,
		core.Image + ":" + core.Version,
	} {
		if !strings.Contains(contents, required) {
			t.Fatalf("Home Assistant workflow does not publish %q", required)
		}
	}
	if strings.Contains(contents, "aquaos-history-addon") || strings.Contains(contents, "aquaos-history/Dockerfile") {
		t.Fatal("Home Assistant workflow must not publish the removed Grafana add-on")
	}
}

type addonManifest struct {
	Image       string `yaml:"image"`
	IngressPort int    `yaml:"ingress_port"`
	Version     string `yaml:"version"`
}

func readAddonManifest(t *testing.T, path string) addonManifest {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest addonManifest
	if err = yaml.Unmarshal(payload, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}
