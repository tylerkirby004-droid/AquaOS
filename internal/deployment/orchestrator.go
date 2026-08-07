// Package deployment plans and executes an explicit multi-VM AquaOS rollout.
// It runs from an administrator workstation and never installs Core on the
// Proxmox host.
package deployment

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var safeToken = regexp.MustCompile(`^[A-Za-z0-9._@-]+$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Config is the complete external deployment input. It contains paths and
// identities but no passwords or private-key contents.
type Config struct {
	Proxmox             Proxmox `json:"proxmox"`
	Control             Guest   `json:"control"`
	Services            Guest   `json:"services"`
	HomeAssistant       Guest   `json:"homeAssistant"`
	Release             Release `json:"release"`
	RepositoryDirectory string  `json:"repositoryDirectory"`
}

// Proxmox describes the remote hypervisor and approved templates.
type Proxmox struct {
	Host             string `json:"host"`
	User             string `json:"user"`
	Node             string `json:"node"`
	Storage          string `json:"storage"`
	Bridge           string `json:"bridge"`
	IdentityFile     string `json:"identityFile"`
	PublicKeyFile    string `json:"publicKeyFile"`
	DebianTemplateID int    `json:"debianTemplateId"`
	HAOSTemplateID   int    `json:"haosTemplateId"`
}

// Guest declares one isolated VM and its bridged-LAN address.
type Guest struct {
	VMID      int    `json:"vmid"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	CIDR      string `json:"cidr"`
	Gateway   string `json:"gateway"`
	Cores     int    `json:"cores"`
	MemoryMiB int    `json:"memoryMiB"`
	DiskGiB   int    `json:"diskGiB"`
}

// Release points to a locally verified signed release directory.
type Release struct {
	Directory string `json:"directory"`
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
}

// Action is one reviewable external command.
type Action struct {
	Description  string   `json:"description"`
	Command      string   `json:"command"`
	Arguments    []string `json:"arguments"`
	MustFail     bool     `json:"mustFail,omitempty"`
	Attempts     int      `json:"attempts,omitempty"`
	RetrySeconds int      `json:"retrySeconds,omitempty"`
}

// Runner executes one command without invoking an intermediate local shell.
type Runner interface {
	Run(context.Context, Action) error
}

// CommandRunner is the production external-command boundary.
type CommandRunner struct{}

// Run executes and joins a bounded command under the caller's context.
func (CommandRunner) Run(ctx context.Context, action Action) error {
	attempts := action.Attempts
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		command := exec.CommandContext(ctx, action.Command, action.Arguments...)
		output, err := command.CombinedOutput()
		if action.MustFail {
			if err == nil {
				return fmt.Errorf("preflight unexpectedly succeeded: %s", action.Description)
			}
			return nil
		}
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("%s: %w: %s", action.Description, err, strings.TrimSpace(string(output)))
		if attempt < attempts {
			timer := time.NewTimer(time.Duration(action.RetrySeconds) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return lastErr
}

// Orchestrator validates, plans, and sequentially executes deployment actions.
type Orchestrator struct{ runner Runner }

// New constructs an orchestrator with an explicit command boundary.
func New(runner Runner) (*Orchestrator, error) {
	if runner == nil {
		return nil, errors.New("deployment runner is required")
	}
	return &Orchestrator{runner: runner}, nil
}

// Apply executes a previously reviewable plan and stops at the first failure.
func (o *Orchestrator) Apply(ctx context.Context, cfg Config) error {
	actions, err := Plan(cfg)
	if err != nil {
		return err
	}
	for _, action := range actions {
		if err = o.runner.Run(ctx, action); err != nil {
			return err
		}
	}
	return nil
}

// Plan returns deterministic non-destructive provisioning and installation
// actions. Existing VM IDs make preflight fail before any clone action.
func Plan(cfg Config) ([]Action, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	remote := cfg.Proxmox.User + "@" + cfg.Proxmox.Host
	sshBase := []string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-i", cfg.Proxmox.IdentityFile, remote}
	ssh := func(description string, arguments ...string) Action {
		return Action{Description: description, Command: "ssh", Arguments: append(append([]string(nil), sshBase...), arguments...)}
	}
	actions := []Action{ssh("verify Proxmox API", "pvesh", "get", "/version")}
	guests := []struct {
		guest    Guest
		template int
		role     string
	}{{cfg.Control, cfg.Proxmox.DebianTemplateID, "control"}, {cfg.Services, cfg.Proxmox.DebianTemplateID, "services"}, {cfg.HomeAssistant, cfg.Proxmox.HAOSTemplateID, "home-assistant"}}
	for _, item := range guests {
		check := ssh("verify VM ID is unused: "+item.role, "qm", "status", strconv.Itoa(item.guest.VMID))
		check.MustFail = true
		actions = append(actions, check)
	}
	for _, item := range guests {
		id := strconv.Itoa(item.guest.VMID)
		actions = append(actions,
			ssh("clone "+item.role+" VM", "qm", "clone", strconv.Itoa(item.template), id, "--name", item.guest.Name, "--full", "1", "--storage", cfg.Proxmox.Storage),
			ssh("configure "+item.role+" resources", "qm", "set", id, "--cores", strconv.Itoa(item.guest.Cores), "--memory", strconv.Itoa(item.guest.MemoryMiB), "--net0", "virtio,bridge="+cfg.Proxmox.Bridge, "--onboot", "1", "--startup", "order="+startupOrder(item.role)),
		)
		if item.role != "home-assistant" {
			actions = append(actions,
				ssh("configure "+item.role+" cloud-init", "qm", "set", id, "--ciuser", "aquaosadmin", "--sshkeys", cfg.Proxmox.PublicKeyFile, "--ipconfig0", "ip="+item.guest.CIDR+",gw="+item.guest.Gateway, "--nameserver", item.guest.Gateway),
				ssh("resize "+item.role+" disk", "qm", "resize", id, "scsi0", strconv.Itoa(item.guest.DiskGiB)+"G"),
			)
		}
		actions = append(actions, ssh("start "+item.role+" VM", "qm", "start", id))
	}
	controlRemote := "aquaosadmin@" + cfg.Control.Address
	servicesRemote := "aquaosadmin@" + cfg.Services.Address
	commonSCP := []string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-i", cfg.Proxmox.IdentityFile, "-r"}
	actions = append(actions,
		Action{Description: "copy signed AquaOS release", Command: "scp", Arguments: append(append([]string(nil), commonSCP...), cfg.Release.Directory, controlRemote+":/tmp/aquaos-release"), Attempts: 30, RetrySeconds: 10},
		Action{Description: "install AquaOS Core", Command: "ssh", Arguments: append(append([]string(nil), sshBase[:len(sshBase)-1]...), controlRemote, controlInstallCommand(cfg.Release))},
		Action{Description: "prepare optional services staging directory", Command: "ssh", Arguments: append(append([]string(nil), sshBase[:len(sshBase)-1]...), servicesRemote, "mkdir", "-p", "/tmp/aquaos-services"), Attempts: 30, RetrySeconds: 10},
		Action{Description: "copy optional services assets", Command: "scp", Arguments: append(append([]string(nil), commonSCP...), filepath.Join(cfg.RepositoryDirectory, "infrastructure", "docker"), filepath.Join(cfg.RepositoryDirectory, "scripts", "install-optional-services.sh"), servicesRemote+":/tmp/aquaos-services")},
		Action{Description: "install optional services", Command: "ssh", Arguments: append(append([]string(nil), sshBase[:len(sshBase)-1]...), servicesRemote, "sudo", "/tmp/aquaos-services/install-optional-services.sh", "/tmp/aquaos-services/docker")},
		Action{Description: "verify AquaOS Core", Command: "ssh", Arguments: append(append([]string(nil), sshBase[:len(sshBase)-1]...), controlRemote, "sudo", "/opt/aquaos/bin/aquaosctl", "verify")},
	)
	return actions, nil
}

func controlInstallCommand(release Release) string {
	root := "/tmp/aquaos-release"
	return "chmod 0755 " + root + "/aquaosctl-linux-amd64 && sudo " + root + "/aquaosctl-linux-amd64 install --binary " + root + "/aquaos-linux-amd64 --config " + root + "/aquaos.yaml --version " + release.Version + " --sha256 " + release.SHA256 + " --signature " + root + "/aquaos-linux-amd64.sig.hex --public-key " + root + "/aquaos-ed25519-public-key.hex --ack-control-vm && sudo install -m 0755 " + root + "/aquaosctl-linux-amd64 /opt/aquaos/bin/aquaosctl && sudo install -m 0755 " + root + "/aquaos-admin-linux-amd64 /opt/aquaos/bin/aquaos-admin"
}

func startupOrder(role string) string {
	if role == "control" {
		return "1"
	}
	if role == "services" {
		return "2"
	}
	return "3"
}

// Validate rejects ambiguous identifiers, public target addresses, missing
// signed artifacts, and overlapping VM identities before external mutation.
func (c Config) Validate() error {
	for name, value := range map[string]string{"host": c.Proxmox.Host, "user": c.Proxmox.User, "node": c.Proxmox.Node, "storage": c.Proxmox.Storage, "bridge": c.Proxmox.Bridge, "version": c.Release.Version} {
		if !safeToken.MatchString(value) {
			return fmt.Errorf("%s contains unsupported characters", name)
		}
	}
	if !digestPattern.MatchString(c.Release.SHA256) {
		return errors.New("release SHA-256 must be 64 lowercase hexadecimal characters")
	}
	for _, path := range []string{c.Proxmox.IdentityFile, c.Release.Directory, c.RepositoryDirectory} {
		if path == "" {
			return errors.New("deployment paths are required")
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("deployment path is unavailable: %w", err)
		}
	}
	for _, name := range []string{"aquaos-linux-amd64", "aquaosctl-linux-amd64", "aquaos-admin-linux-amd64", "aquaos-linux-amd64.sig.hex", "aquaos-ed25519-public-key.hex", "aquaos.yaml"} {
		if _, err := os.Stat(filepath.Join(c.Release.Directory, name)); err != nil {
			return fmt.Errorf("signed release is incomplete: %s", name)
		}
	}
	for _, path := range []string{filepath.Join(c.RepositoryDirectory, "infrastructure", "docker", "compose.yaml"), filepath.Join(c.RepositoryDirectory, "scripts", "install-optional-services.sh")} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("optional-services installer is incomplete: %w", err)
		}
	}
	if c.Proxmox.PublicKeyFile == "" || strings.ContainsAny(c.Proxmox.PublicKeyFile, "\r\n;|&`$()") {
		return errors.New("Proxmox public key path is invalid")
	}
	ids := map[int]struct{}{}
	for name, guest := range map[string]Guest{"control": c.Control, "services": c.Services, "homeAssistant": c.HomeAssistant} {
		if guest.VMID < 100 || guest.VMID > 999999 || guest.Cores < 1 || guest.Cores > 64 || guest.MemoryMiB < 2048 || guest.DiskGiB < 16 || !safeToken.MatchString(guest.Name) {
			return fmt.Errorf("%s VM resources or identity are invalid", name)
		}
		if _, exists := ids[guest.VMID]; exists {
			return errors.New("VM IDs must be unique")
		}
		ids[guest.VMID] = struct{}{}
		if net.ParseIP(guest.Address) == nil {
			return fmt.Errorf("%s address must be an IP", name)
		}
		ip, network, err := net.ParseCIDR(guest.CIDR)
		if err != nil || !ip.IsPrivate() || !network.Contains(ip) || net.ParseIP(guest.Gateway) == nil || !network.Contains(net.ParseIP(guest.Gateway)) {
			return fmt.Errorf("%s requires a private CIDR and in-subnet gateway", name)
		}
		if !ip.Equal(net.ParseIP(guest.Address)) {
			return fmt.Errorf("%s address and CIDR do not match", name)
		}
	}
	if c.Proxmox.DebianTemplateID < 100 || c.Proxmox.HAOSTemplateID < 100 {
		return errors.New("approved Debian and HAOS template IDs are required")
	}
	parsed, err := url.Parse("ssh://" + c.Proxmox.Host)
	if err != nil || parsed.Host == "" {
		return errors.New("Proxmox host is invalid")
	}
	return nil
}
