package deployment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validConfig(t *testing.T) Config {
	t.Helper()
	directory := t.TempDir()
	for _, name := range []string{"aquaos-linux-amd64", "aquaosctl-linux-amd64", "aquaos-admin-linux-amd64", "aquaos-linux-amd64.sig.hex", "aquaos-ed25519-public-key.hex", "aquaos.yaml", filepath.Join("infrastructure", "docker", "compose.yaml"), filepath.Join("scripts", "install-optional-services.sh")} {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Config{
		SiteID:        "home-reef",
		Proxmox:       Proxmox{Host: "pve.local", User: "root", Node: "pve", Storage: "local-lvm", Bridge: "vmbr0", IdentityFile: directory, PublicKeyFile: "/root/.ssh/aquaos.pub", DebianTemplateID: 9000, HAOSTemplateID: 9001},
		Control:       Guest{VMID: 200, Name: "aquaos-control", Address: "192.168.10.20", CIDR: "192.168.10.20/24", Gateway: "192.168.10.1", Cores: 2, MemoryMiB: 4096, DiskGiB: 32},
		Services:      Guest{VMID: 201, Name: "aquaos-services", Address: "192.168.10.21", CIDR: "192.168.10.21/24", Gateway: "192.168.10.1", Cores: 4, MemoryMiB: 8192, DiskGiB: 80},
		HomeAssistant: Guest{VMID: 202, Name: "home-assistant", Address: "192.168.10.22", CIDR: "192.168.10.22/24", Gateway: "192.168.10.1", Cores: 2, MemoryMiB: 4096, DiskGiB: 32},
		Release:       Release{Directory: directory, Version: "v1.0.0", SHA256: strings.Repeat("a", 64)}, RepositoryDirectory: directory,
	}
}

func TestPlanPreflightsAllVMIDsAndContainsNoDeletion(t *testing.T) {
	actions, err := Plan(validConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	preflights := 0
	for _, action := range actions {
		joined := action.Command + " " + strings.Join(action.Arguments, " ")
		if strings.Contains(joined, " qm status ") && action.MustFail {
			preflights++
		}
		if strings.Contains(joined, " destroy ") || strings.Contains(joined, " remove ") || strings.Contains(joined, " rm ") {
			t.Fatalf("destructive action planned: %s", joined)
		}
	}
	if preflights != 3 {
		t.Fatalf("VM preflights = %d", preflights)
	}
}

func TestValidateRejectsPublicGuestAndDuplicateVMID(t *testing.T) {
	cfg := validConfig(t)
	cfg.Control.Address, cfg.Control.CIDR = "8.8.8.8", "8.8.8.8/24"
	if err := cfg.Validate(); err == nil {
		t.Fatal("public guest address accepted")
	}
	cfg = validConfig(t)
	cfg.Services.VMID = cfg.Control.VMID
	if err := cfg.Validate(); err == nil {
		t.Fatal("duplicate VM ID accepted")
	}
}

type recordingRunner struct {
	actions []Action
	failAt  int
}

func (r *recordingRunner) Run(_ context.Context, action Action) error {
	r.actions = append(r.actions, action)
	if r.failAt > 0 && len(r.actions) == r.failAt {
		return errors.New("injected failure")
	}
	return nil
}

func TestApplyStopsAtFirstFailure(t *testing.T) {
	runner := &recordingRunner{failAt: 2}
	orchestrator, _ := New(runner)
	if err := orchestrator.Apply(context.Background(), validConfig(t)); err == nil || len(runner.actions) != 2 {
		t.Fatalf("actions=%d err=%v", len(runner.actions), err)
	}
}
