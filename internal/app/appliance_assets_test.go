package app

import (
	"os"
	"strings"
	"testing"
)

func TestApplianceInstallerPreservesCriticalBoundary(t *testing.T) {
	payload, err := os.ReadFile("../../scripts/install-appliance.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, required := range []string{"refusing to install on a Proxmox host", "aquaosctl-linux-amd64\" install", "--dry-run", "CPUWeight=100", "aquaos.service", "ack-independent-safeguards"} {
		if !strings.Contains(text, required) {
			t.Fatalf("installer is missing safety control %q", required)
		}
	}
	if strings.Contains(text, "qm destroy") || strings.Contains(text, "docker run aquaos") {
		t.Fatal("installer introduced an unsafe Core deployment path")
	}
}
