package operations

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
)

const (
	binaryPath   = "/opt/aquaos/bin/aquaos"
	configPath   = "/etc/aquaos/aquaos.yaml"
	versionPath  = "/var/lib/aquaos/current-version"
	unitPath     = "/etc/systemd/system/aquaos.service"
	sysusersPath = "/usr/lib/sysusers.d/aquaos.conf"
	tmpfilesPath = "/usr/lib/tmpfiles.d/aquaos.conf"
)

// Actor is an authenticated operations identity.
type Actor struct {
	ID            string
	Administrator bool
}

// InstallRequest contains an already acquired release artifact and candidate configuration.
type InstallRequest struct {
	Actor                 Actor
	Version               string
	Binary                []byte
	SHA256                string
	Signature             []byte
	PublicKey             ed25519.PublicKey
	Configuration         []byte
	ControlVMAcknowledged bool
	DryRun                bool
}

// UpgradeRequest contains a signed release artifact and verification material.
type UpgradeRequest struct {
	Actor     Actor
	Version   string
	Binary    []byte
	SHA256    string
	Signature []byte
	PublicKey ed25519.PublicKey
	DryRun    bool
}

// Result is a stable operation outcome.
type Result struct {
	Operation  string   `json:"operation"`
	Changed    bool     `json:"changed"`
	Version    string   `json:"version,omitempty"`
	Actions    []string `json:"actions"`
	RolledBack bool     `json:"rolledBack,omitempty"`
}

// Status is a redacted deployment snapshot.
type Status struct {
	Installed   bool   `json:"installed"`
	Version     string `json:"version,omitempty"`
	Configured  bool   `json:"configured"`
	ServiceUnit bool   `json:"serviceUnit"`
	Platform    string `json:"platform"`
}

// Diagnostics is a redacted recovery bundle payload.
type Diagnostics struct {
	Status       Status            `json:"status"`
	ConfigDigest string            `json:"configDigest,omitempty"`
	Checks       map[string]string `json:"checks"`
}

// ConfigurationResult is a redacted candidate validation or activation result.
type ConfigurationResult struct {
	Digest          string `json:"digest"`
	Valid           bool   `json:"valid"`
	Changed         bool   `json:"changed"`
	RestartRequired bool   `json:"restartRequired"`
	RolledBack      bool   `json:"rolledBack,omitempty"`
}

// Service owns authorized, exact-path dedicated-appliance operations.
type Service struct {
	host   Host
	logger *slog.Logger
	now    func() time.Time
}

// New constructs the shared operations application service.
func New(host Host, logger *slog.Logger) (*Service, error) {
	if host == nil || logger == nil {
		return nil, errors.New("operations host and logger are required")
	}
	return &Service{host: host, logger: logger, now: time.Now}, nil
}

func authorize(actor Actor) error {
	if strings.TrimSpace(actor.ID) == "" || !actor.Administrator {
		return errors.New("administrator authorization is required")
	}
	return nil
}
func (s *Service) preflight(ack bool) error {
	if !ack {
		return errors.New("dedicated AquaOS control host acknowledgement is required")
	}
	if s.host.GOOS() != "linux" || s.host.GOARCH() != "amd64" {
		return fmt.Errorf("unsupported production platform %s/%s; require linux/amd64", s.host.GOOS(), s.host.GOARCH())
	}
	if exists, _ := s.host.Exists("/etc/pve"); exists {
		return errors.New("refusing to install on a hypervisor host")
	}
	return nil
}

// Install validates and atomically installs AquaOS without overwriting an existing configuration.
func (s *Service) Install(ctx context.Context, request InstallRequest) (Result, error) {
	if err := authorize(request.Actor); err != nil {
		return Result{}, err
	}
	if err := s.preflight(request.ControlVMAcknowledged); err != nil {
		return Result{}, err
	}
	if request.Version == "" {
		return Result{}, errors.New("version is required")
	}
	if err := verifyArtifact(request.Binary, request.SHA256, request.Signature, request.PublicKey); err != nil {
		return Result{}, err
	}
	if _, err := config.DecodeCandidate(request.Configuration); err != nil {
		return Result{}, fmt.Errorf("validate install configuration: %w", err)
	}
	actions := []string{"create least-privilege aquaos account", "install binary atomically", "preserve or create validated configuration", "install native systemd unit", "enable and start aquaos.service"}
	result := Result{Operation: "install", Version: request.Version, Actions: actions}
	if request.DryRun {
		return result, nil
	}
	if err := s.host.MkdirAll("/var/lib/aquaos", 0o750); err != nil {
		return Result{}, err
	}
	if err := s.host.MkdirAll("/var/log/aquaos", 0o750); err != nil {
		return Result{}, err
	}
	if err := s.host.WriteFileAtomic(sysusersPath, []byte("u aquaos - \"AquaOS service\" /var/lib/aquaos /usr/sbin/nologin\n"), 0o644); err != nil {
		return Result{}, err
	}
	if err := s.host.WriteFileAtomic(tmpfilesPath, []byte("d /var/lib/aquaos 0750 aquaos aquaos -\nd /var/log/aquaos 0750 aquaos aquaos -\n"), 0o644); err != nil {
		return Result{}, err
	}
	if err := s.host.Run(ctx, "systemd-sysusers", sysusersPath); err != nil {
		return Result{}, err
	}
	if err := s.host.Run(ctx, "systemd-tmpfiles", "--create", tmpfilesPath); err != nil {
		return Result{}, err
	}
	existing, err := s.host.Exists(configPath)
	if err != nil {
		return Result{}, err
	}
	if !existing {
		if err = s.host.WriteFileAtomic(configPath, request.Configuration, 0o640); err != nil {
			return Result{}, err
		}
		result.Changed = true
	}
	current, readErr := s.host.ReadFile(binaryPath)
	if readErr != nil || !bytes.Equal(current, request.Binary) {
		if err = s.host.WriteFileAtomic(binaryPath, request.Binary, 0o755); err != nil {
			return Result{}, err
		}
		result.Changed = true
	}
	if err = s.host.WriteFileAtomic(unitPath, []byte(systemdUnit), 0o644); err != nil {
		return Result{}, err
	}
	if err = s.host.WriteFileAtomic(versionPath, []byte(request.Version+"\n"), 0o640); err != nil {
		return Result{}, err
	}
	if err = s.host.Run(ctx, "systemctl", "daemon-reload"); err != nil {
		return Result{}, err
	}
	if err = s.host.Run(ctx, "systemctl", "enable", "--now", "aquaos.service"); err != nil {
		return Result{}, err
	}
	s.logger.InfoContext(ctx, "AquaOS installation completed", "actor", request.Actor.ID, "version", request.Version, "changed", result.Changed)
	return result, nil
}

// GetStatus returns exact managed-path state without exposing configuration.
func (s *Service) GetStatus(ctx context.Context, actor Actor) (Status, error) {
	if err := authorize(actor); err != nil {
		return Status{}, err
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	binary, _ := s.host.Exists(binaryPath)
	configured, _ := s.host.Exists(configPath)
	unit, _ := s.host.Exists(unitPath)
	version, _ := s.host.ReadFile(versionPath)
	return Status{Installed: binary, Version: strings.TrimSpace(string(version)), Configured: configured, ServiceUnit: unit, Platform: s.host.GOOS() + "/" + s.host.GOARCH()}, nil
}

// GetConfiguration returns the validated, redacted active configuration for
// guided administrative editing. Secret values are never returned.
func (s *Service) GetConfiguration(ctx context.Context, actor Actor) (config.Config, error) {
	if err := authorize(actor); err != nil {
		return config.Config{}, err
	}
	if err := ctx.Err(); err != nil {
		return config.Config{}, err
	}
	payload, err := s.host.ReadFile(configPath)
	if err != nil {
		return config.Config{}, fmt.Errorf("read active configuration: %w", err)
	}
	value, err := config.DecodeCandidate(payload)
	if err != nil {
		return config.Config{}, fmt.Errorf("decode active configuration: %w", err)
	}
	return value.Redacted(), nil
}

// Verify validates platform, managed files, configuration, and service unit.
func (s *Service) Verify(ctx context.Context, actor Actor) (Diagnostics, error) {
	status, err := s.GetStatus(ctx, actor)
	if err != nil {
		return Diagnostics{}, err
	}
	checks := map[string]string{"platform": "fail", "binary": "fail", "configuration": "fail", "systemd": "fail", "service": "fail"}
	if status.Platform == "linux/amd64" {
		checks["platform"] = "pass"
	}
	if status.Installed {
		checks["binary"] = "pass"
	}
	if status.ServiceUnit {
		checks["systemd"] = "pass"
	}
	if s.host.Run(ctx, "systemctl", "is-active", "--quiet", "aquaos.service") == nil {
		checks["service"] = "pass"
	}
	digest := ""
	payload, readErr := s.host.ReadFile(configPath)
	if readErr == nil {
		cfg, decodeErr := config.DecodeCandidate(payload)
		if decodeErr == nil {
			checks["configuration"] = "pass"
			digest, _ = cfg.Digest()
		}
	}
	result := Diagnostics{Status: status, ConfigDigest: digest, Checks: checks}
	for _, value := range checks {
		if value != "pass" {
			return result, errors.New("deployment verification failed")
		}
	}
	return result, nil
}

// Repair restores managed unit/account definitions without replacing configuration.
func (s *Service) Repair(ctx context.Context, actor Actor, dryRun bool) (Result, error) {
	if err := authorize(actor); err != nil {
		return Result{}, err
	}
	actions := []string{"restore managed systemd definitions", "reload systemd", "restart AquaOS"}
	result := Result{Operation: "repair", Actions: actions}
	if dryRun {
		return result, nil
	}
	if exists, _ := s.host.Exists(configPath); !exists {
		return Result{}, errors.New("repair requires an existing configuration")
	}
	if exists, _ := s.host.Exists(binaryPath); !exists {
		return Result{}, errors.New("repair requires an installed binary")
	}
	if err := s.host.WriteFileAtomic(unitPath, []byte(systemdUnit), 0o644); err != nil {
		return Result{}, err
	}
	if err := s.host.Run(ctx, "systemctl", "daemon-reload"); err != nil {
		return Result{}, err
	}
	if err := s.host.Run(ctx, "systemctl", "restart", "aquaos.service"); err != nil {
		return Result{}, err
	}
	result.Changed = true
	return result, nil
}

// ValidateConfiguration validates an entire candidate without writing it.
func (s *Service) ValidateConfiguration(_ context.Context, actor Actor, payload []byte) (ConfigurationResult, error) {
	if err := authorize(actor); err != nil {
		return ConfigurationResult{}, err
	}
	candidate, err := config.DecodeCandidate(payload)
	if err != nil {
		return ConfigurationResult{}, err
	}
	digest, err := candidate.Digest()
	if err != nil {
		return ConfigurationResult{}, err
	}
	return ConfigurationResult{Digest: digest, Valid: true, RestartRequired: true}, nil
}

// ApplyConfiguration validates, atomically replaces, restarts, and rolls back on failure.
func (s *Service) ApplyConfiguration(ctx context.Context, actor Actor, payload []byte, dryRun bool) (ConfigurationResult, error) {
	result, err := s.ValidateConfiguration(ctx, actor, payload)
	if err != nil {
		return ConfigurationResult{}, err
	}
	current, err := s.host.ReadFile(configPath)
	if err != nil {
		return ConfigurationResult{}, errors.New("configuration activation requires an existing configuration")
	}
	if bytes.Equal(current, payload) {
		return result, nil
	}
	if _, err := config.DecodeCandidate(current); err != nil {
		return ConfigurationResult{}, fmt.Errorf("installed configuration is invalid: %w", err)
	}
	result.Changed = true
	if dryRun {
		return result, nil
	}
	if err = s.host.WriteFileAtomic("/var/lib/aquaos/rollback/aquaos.yaml", current, 0o640); err != nil {
		return ConfigurationResult{}, err
	}
	if err = s.host.WriteFileAtomic(configPath, payload, 0o640); err != nil {
		return ConfigurationResult{}, err
	}
	if err = s.host.Run(ctx, "systemctl", "restart", "aquaos.service"); err != nil {
		_ = s.host.WriteFileAtomic(configPath, current, 0o640)
		_ = s.host.Run(ctx, "systemctl", "restart", "aquaos.service")
		result.RolledBack = true
		return result, fmt.Errorf("configuration activation failed and rollback attempted: %w", err)
	}
	s.logger.InfoContext(ctx, "AquaOS configuration activated", "actor", actor.ID, "digest", result.Digest)
	return result, nil
}

// Upgrade verifies signature/checksum and rolls back if restart verification fails.
func (s *Service) Upgrade(ctx context.Context, request UpgradeRequest) (Result, error) {
	if err := authorize(request.Actor); err != nil {
		return Result{}, err
	}
	if err := verifyArtifact(request.Binary, request.SHA256, request.Signature, request.PublicKey); err != nil {
		return Result{}, err
	}
	result := Result{Operation: "upgrade", Version: request.Version, Actions: []string{"verify checksum and Ed25519 signature", "stage rollback binary", "activate release atomically", "restart and verify"}}
	if request.DryRun {
		return result, nil
	}
	previous, err := s.host.ReadFile(binaryPath)
	if err != nil {
		return Result{}, errors.New("upgrade requires an installed binary")
	}
	previousVersion, _ := s.host.ReadFile(versionPath)
	rollbackPath := "/var/lib/aquaos/rollback/aquaos"
	if err = s.host.WriteFileAtomic(rollbackPath, previous, 0o750); err != nil {
		return Result{}, err
	}
	if err = s.host.WriteFileAtomic("/var/lib/aquaos/rollback/version", previousVersion, 0o640); err != nil {
		return Result{}, err
	}
	if err = s.host.WriteFileAtomic(binaryPath, request.Binary, 0o755); err != nil {
		return Result{}, err
	}
	_ = s.host.WriteFileAtomic(versionPath, []byte(request.Version+"\n"), 0o640)
	if err = s.host.Run(ctx, "systemctl", "restart", "aquaos.service"); err != nil {
		_ = s.host.WriteFileAtomic(binaryPath, previous, 0o755)
		_ = s.host.WriteFileAtomic(versionPath, previousVersion, 0o640)
		_ = s.host.Run(ctx, "systemctl", "restart", "aquaos.service")
		result.RolledBack = true
		return result, fmt.Errorf("upgrade failed and rollback attempted: %w", err)
	}
	result.Changed = true
	return result, nil
}

// Rollback activates the last staged binary.
func (s *Service) Rollback(ctx context.Context, actor Actor, dryRun bool) (Result, error) {
	if err := authorize(actor); err != nil {
		return Result{}, err
	}
	result := Result{Operation: "rollback", Actions: []string{"activate staged rollback", "restart AquaOS"}}
	if dryRun {
		return result, nil
	}
	binary, err := s.host.ReadFile("/var/lib/aquaos/rollback/aquaos")
	if err != nil {
		return Result{}, errors.New("no rollback artifact is available")
	}
	version, _ := s.host.ReadFile("/var/lib/aquaos/rollback/version")
	if err = s.host.WriteFileAtomic(binaryPath, binary, 0o755); err != nil {
		return Result{}, err
	}
	_ = s.host.WriteFileAtomic(versionPath, version, 0o640)
	if err = s.host.Run(ctx, "systemctl", "restart", "aquaos.service"); err != nil {
		return Result{}, err
	}
	result.Changed = true
	result.Version = strings.TrimSpace(string(version))
	return result, nil
}

// Backup creates a deterministic replacement-host recovery archive.
func (s *Service) Backup(ctx context.Context, actor Actor) ([]byte, error) {
	if err := authorize(actor); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files := []string{configPath, versionPath}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	manifest := make(map[string]string, len(files))
	for _, path := range files {
		payload, err := s.host.ReadFile(path)
		if err != nil {
			_ = archive.Close()
			return nil, err
		}
		name := strings.TrimPrefix(path, "/")
		entry, err := archive.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write(payload); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(payload)
		manifest[name] = hex.EncodeToString(sum[:])
	}
	encoded, _ := json.Marshal(manifest)
	entry, _ := archive.Create("manifest.json")
	_, _ = entry.Write(encoded)
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Restore validates a backup completely before atomically replacing configuration metadata.
func (s *Service) Restore(ctx context.Context, actor Actor, payload []byte, dryRun bool) (Result, error) {
	if err := authorize(actor); err != nil {
		return Result{}, err
	}
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return Result{}, errors.New("backup archive is invalid")
	}
	files := make(map[string][]byte)
	for _, entry := range reader.File {
		if entry.Name != "etc/aquaos/aquaos.yaml" && entry.Name != "var/lib/aquaos/current-version" && entry.Name != "manifest.json" {
			return Result{}, errors.New("backup contains an unsupported path")
		}
		stream, openErr := entry.Open()
		if openErr != nil {
			return Result{}, openErr
		}
		data, readErr := io.ReadAll(io.LimitReader(stream, 2*1024*1024))
		_ = stream.Close()
		if readErr != nil {
			return Result{}, readErr
		}
		files[entry.Name] = data
	}
	var manifest map[string]string
	if json.Unmarshal(files["manifest.json"], &manifest) != nil {
		return Result{}, errors.New("backup manifest is invalid")
	}
	for name, digest := range manifest {
		sum := sha256.Sum256(files[name])
		if hex.EncodeToString(sum[:]) != digest {
			return Result{}, errors.New("backup checksum mismatch")
		}
	}
	candidate := files["etc/aquaos/aquaos.yaml"]
	if _, err = config.DecodeCandidate(candidate); err != nil {
		return Result{}, fmt.Errorf("backup configuration: %w", err)
	}
	result := Result{Operation: "restore", Version: strings.TrimSpace(string(files["var/lib/aquaos/current-version"])), Actions: []string{"verify archive manifest", "validate candidate configuration", "activate configuration atomically", "restart AquaOS"}}
	if dryRun {
		return result, nil
	}
	if err = s.host.WriteFileAtomic(configPath, candidate, 0o640); err != nil {
		return Result{}, err
	}
	if err = s.host.WriteFileAtomic(versionPath, files["var/lib/aquaos/current-version"], 0o640); err != nil {
		return Result{}, err
	}
	if err = s.host.Run(ctx, "systemctl", "restart", "aquaos.service"); err != nil {
		return Result{}, err
	}
	result.Changed = true
	return result, nil
}

// Diagnostics returns a redacted verification snapshot even when checks fail.
func (s *Service) Diagnostics(ctx context.Context, actor Actor) (Diagnostics, error) {
	result, err := s.Verify(ctx, actor)
	return result, err
}

// Uninstall disables Core and removes only AquaOS-owned executable/unit files.
func (s *Service) Uninstall(ctx context.Context, actor Actor, preserveData, dryRun bool) (Result, error) {
	if err := authorize(actor); err != nil {
		return Result{}, err
	}
	actions := []string{"disable AquaOS service", "remove AquaOS binary and unit"}
	if preserveData {
		actions = append(actions, "preserve configuration and state")
	}
	result := Result{Operation: "uninstall", Actions: actions}
	if dryRun {
		return result, nil
	}
	_ = s.host.Run(ctx, "systemctl", "disable", "--now", "aquaos.service")
	for _, path := range []string{binaryPath, unitPath, sysusersPath, tmpfilesPath} {
		if err := s.host.Remove(path); err != nil {
			return Result{}, err
		}
	}
	if !preserveData {
		for _, path := range []string{configPath, versionPath} {
			if err := s.host.Remove(path); err != nil {
				return Result{}, err
			}
		}
	}
	_ = s.host.Run(ctx, "systemctl", "daemon-reload")
	result.Changed = true
	return result, nil
}

func verifyArtifact(binary []byte, expected string, signature []byte, key ed25519.PublicKey) error {
	if len(binary) == 0 || len(key) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return errors.New("signed artifact, signature, and trusted Ed25519 key are required")
	}
	sum := sha256.Sum256(binary)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
		return errors.New("artifact checksum mismatch")
	}
	if !ed25519.Verify(key, []byte(actual), signature) {
		return errors.New("artifact signature verification failed")
	}
	return nil
}

// VerifyReleaseArtifact validates one signed release file without changing the
// host. Installers use the same policy as installation and upgrades.
func VerifyReleaseArtifact(binary []byte, expected string, signature []byte, key ed25519.PublicKey) error {
	return verifyArtifact(binary, expected, signature, key)
}

// ManagedPaths returns the exact paths operations may modify.
func ManagedPaths() []string {
	values := []string{binaryPath, configPath, versionPath, unitPath, sysusersPath, tmpfilesPath, "/var/lib/aquaos/rollback/aquaos", "/var/lib/aquaos/rollback/version", "/var/lib/aquaos/rollback/aquaos.yaml"}
	sort.Strings(values)
	return values
}

const systemdUnit = `[Unit]
Description=AquaOS Core
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=aquaos
Group=aquaos
EnvironmentFile=-/etc/aquaos/aquaos.env
ExecStart=/opt/aquaos/bin/aquaos -config /etc/aquaos/aquaos.yaml
Restart=on-failure
RestartSec=5s
CPUWeight=1000
OOMScoreAdjust=-500
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/aquaos /var/log/aquaos
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
`
