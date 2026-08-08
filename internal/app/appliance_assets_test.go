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
	for _, required := range []string{"refusing to install on a hypervisor host", "aquaosctl-linux-amd64\" install", "--dry-run", "CPUWeight=100", "aquaos.service", "ack-independent-safeguards"} {
		if !strings.Contains(text, required) {
			t.Fatalf("installer is missing safety control %q", required)
		}
	}
	if strings.Contains(text, "qm destroy") || strings.Contains(text, "docker run aquaos") {
		t.Fatal("installer introduced an unsafe Core deployment path")
	}
}

func TestApplianceInstallerRepairsInstalledMediaSources(t *testing.T) {
	payload, err := os.ReadFile("../../scripts/install-appliance.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, required := range []string{"Disabled by AquaOS: deb cdrom:", "https://deb.debian.org/debian", "https://security.debian.org/debian-security", "debian-archive-keyring.gpg", "ca-certificates sudo"} {
		if !strings.Contains(text, required) {
			t.Fatalf("installer does not contain %q", required)
		}
	}
}

func TestBootableImageRequiresAuthenticatedFirstBoot(t *testing.T) {
	service, err := os.ReadFile("../../packaging/appliance-image/aquaos-firstboot.service")
	if err != nil {
		t.Fatal(err)
	}
	builder, err := os.ReadFile("../../scripts/build-appliance-image.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"ConditionPathExists=!/var/lib/aquaos/firstboot.complete", "ExecCondition=/usr/bin/test ! -e /run/live/medium", "Conflicts=getty@tty1.service", "TTYPath=/dev/tty1", "ListenAndServeTLS"} {
		contents := string(service) + string(builder)
		if required == "ListenAndServeTLS" {
			main, readErr := os.ReadFile("../../cmd/aquaos-firstboot/main.go")
			if readErr != nil {
				t.Fatal(readErr)
			}
			contents += string(main)
		}
		if !strings.Contains(contents, required) {
			t.Fatalf("bootable image is missing first-boot control %q", required)
		}
	}
	if !strings.Contains(string(builder), "aquaos-linux-amd64.sha256") {
		t.Fatal("image builder does not verify the signed release digest")
	}
	liveHelp, err := os.ReadFile("../../packaging/appliance-image/aquaos-live-help.service")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(liveHelp), "choose Graphical Install") || !strings.Contains(string(builder), "aquaos-live-help.service") {
		t.Fatal("image does not guide users who accidentally enter Live mode")
	}
}
