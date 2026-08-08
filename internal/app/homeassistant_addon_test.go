package app

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHomeAssistantAddonImagesAreDistinct(t *testing.T) {
	core := readAddonManifest(t, "../../aquaos/config.yaml")
	history := readAddonManifest(t, "../../aquaos-history/config.yaml")
	if core.Image != "ghcr.io/tylerkirby004-droid/aquaos-addon" {
		t.Fatalf("core add-on image = %q", core.Image)
	}
	if history.Image != "ghcr.io/tylerkirby004-droid/aquaos-history-addon" {
		t.Fatalf("history add-on image = %q", history.Image)
	}
	if core.Image == history.Image {
		t.Fatal("advanced trends must not run the core add-on image")
	}
	if history.IngressPort != 1337 {
		t.Fatalf("history add-on ingress port = %d", history.IngressPort)
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
