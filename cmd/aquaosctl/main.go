// Command aquaosctl provides headless installation and recovery operations.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/tylerkirby004-droid/aquaos/internal/operations"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("aquaosctl", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", string(filepath.Separator), "managed host root; use only a dedicated staging directory for tests")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	remaining := flags.Args()
	if len(remaining) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: aquaosctl [--root /] <install|status|verify|verify-artifact|configure|repair|backup|restore|upgrade|rollback|diagnostics|remove-role|uninstall>")
		return 2
	}
	host, err := operations.NewLocalHost(*root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	service, err := operations.New(host, slog.New(slog.NewJSONHandler(stderr, nil)))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	actor, err := localActor(*root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	ctx := context.Background()
	command := remaining[0]
	args := remaining[1:]
	var result any
	switch command {
	case "install":
		request, parseErr := parseInstall(args, actor)
		if parseErr != nil {
			err = parseErr
		} else {
			result, err = service.Install(ctx, request)
		}
	case "status":
		result, err = service.GetStatus(ctx, actor)
	case "verify":
		result, err = service.Verify(ctx, actor)
	case "diagnostics":
		result, err = service.Diagnostics(ctx, actor)
	case "repair":
		dryRun, parseErr := dryRunOnly("repair", args)
		if parseErr != nil {
			err = parseErr
		} else {
			result, err = service.Repair(ctx, actor, dryRun)
		}
	case "configure":
		result, err = configure(ctx, service, actor, args)
	case "rollback":
		dryRun, parseErr := dryRunOnly("rollback", args)
		if parseErr != nil {
			err = parseErr
		} else {
			result, err = service.Rollback(ctx, actor, dryRun)
		}
	case "upgrade":
		request, parseErr := parseUpgrade(args, actor)
		if parseErr != nil {
			err = parseErr
		} else {
			result, err = service.Upgrade(ctx, request)
		}
	case "verify-artifact":
		result, err = verifyReleaseArtifact(args)
	case "backup":
		result, err = backup(ctx, service, actor, args)
	case "restore":
		result, err = restore(ctx, service, actor, args)
	case "uninstall":
		result, err = uninstall(ctx, service, actor, args)
	case "remove-role":
		result, err = removeRole(args)
	default:
		err = fmt.Errorf("unknown command %q", command)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(result); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func verifyReleaseArtifact(arguments []string) (operations.Result, error) {
	flags := flag.NewFlagSet("verify-artifact", flag.ContinueOnError)
	binaryPath := flags.String("binary", "", "release artifact")
	checksum := flags.String("sha256", "", "expected SHA-256")
	signatureValue := flags.String("signature", "", "hex Ed25519 signature or file path")
	keyValue := flags.String("public-key", "", "hex trusted Ed25519 public key or file path")
	if err := flags.Parse(arguments); err != nil {
		return operations.Result{}, err
	}
	binary, err := os.ReadFile(*binaryPath)
	if err != nil {
		return operations.Result{}, err
	}
	signature, key, err := verification(*signatureValue, *keyValue)
	if err != nil {
		return operations.Result{}, err
	}
	if err = operations.VerifyReleaseArtifact(binary, *checksum, signature, key); err != nil {
		return operations.Result{}, err
	}
	return operations.Result{Operation: "verify-artifact", Actions: []string{"verify checksum and Ed25519 signature"}}, nil
}

func localActor(root string) (operations.Actor, error) {
	absolute, _ := filepath.Abs(root)
	filesystemRoot := filepath.Clean(string(filepath.Separator))
	if absolute != filesystemRoot {
		return operations.Actor{ID: "staging-operator", Administrator: true}, nil
	}
	current, err := user.Current()
	if err != nil {
		return operations.Actor{}, err
	}
	if current.Uid != "0" {
		return operations.Actor{}, errors.New("production host operations require root")
	}
	return operations.Actor{ID: "local-root", Administrator: true}, nil
}

func parseInstall(arguments []string, actor operations.Actor) (operations.InstallRequest, error) {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	binary := flags.String("binary", "", "signed AquaOS linux-amd64 binary")
	configuration := flags.String("config", "", "candidate AquaOS YAML")
	version := flags.String("version", "", "release version")
	checksum := flags.String("sha256", "", "expected SHA-256")
	signature := flags.String("signature", "", "hex Ed25519 signature or file path")
	publicKey := flags.String("public-key", "", "hex trusted Ed25519 public key or file path")
	ack := flags.Bool("ack-control-vm", false, "confirm this is a dedicated AquaOS control host, not a Proxmox host")
	dryRun := flags.Bool("dry-run", false, "validate and report without mutation")
	if err := flags.Parse(arguments); err != nil {
		return operations.InstallRequest{}, err
	}
	binaryData, err := os.ReadFile(*binary)
	if err != nil {
		return operations.InstallRequest{}, err
	}
	configData, err := os.ReadFile(*configuration)
	if err != nil {
		return operations.InstallRequest{}, err
	}
	signatureData, keyData, err := verification(*signature, *publicKey)
	if err != nil {
		return operations.InstallRequest{}, err
	}
	return operations.InstallRequest{Actor: actor, Version: *version, Binary: binaryData, SHA256: *checksum, Signature: signatureData, PublicKey: keyData, Configuration: configData, ControlVMAcknowledged: *ack, DryRun: *dryRun}, nil
}
func parseUpgrade(arguments []string, actor operations.Actor) (operations.UpgradeRequest, error) {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	binary := flags.String("binary", "", "signed AquaOS linux-amd64 binary")
	version := flags.String("version", "", "release version")
	checksum := flags.String("sha256", "", "expected SHA-256")
	signature := flags.String("signature", "", "hex Ed25519 signature or file path")
	publicKey := flags.String("public-key", "", "hex trusted Ed25519 public key or file path")
	dryRun := flags.Bool("dry-run", false, "validate and report without mutation")
	if err := flags.Parse(arguments); err != nil {
		return operations.UpgradeRequest{}, err
	}
	binaryData, err := os.ReadFile(*binary)
	if err != nil {
		return operations.UpgradeRequest{}, err
	}
	signatureData, keyData, err := verification(*signature, *publicKey)
	if err != nil {
		return operations.UpgradeRequest{}, err
	}
	return operations.UpgradeRequest{Actor: actor, Version: *version, Binary: binaryData, SHA256: *checksum, Signature: signatureData, PublicKey: keyData, DryRun: *dryRun}, nil
}
func verification(signatureValue, keyValue string) ([]byte, ed25519.PublicKey, error) {
	signature, err := readHexOrFile(signatureValue, ed25519.SignatureSize)
	if err != nil {
		return nil, nil, err
	}
	key, err := readHexOrFile(keyValue, ed25519.PublicKeySize)
	if err != nil {
		return nil, nil, err
	}
	return signature, ed25519.PublicKey(key), nil
}
func readHexOrFile(value string, size int) ([]byte, error) {
	raw := strings.TrimSpace(value)
	if payload, err := os.ReadFile(raw); err == nil {
		raw = strings.TrimSpace(string(payload))
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != size {
		return nil, fmt.Errorf("verification value must be %d-byte hexadecimal data", size)
	}
	return decoded, nil
}
func dryRunOnly(name string, arguments []string) (bool, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	dryRun := flags.Bool("dry-run", false, "report without mutation")
	err := flags.Parse(arguments)
	return *dryRun, err
}
func backup(ctx context.Context, service *operations.Service, actor operations.Actor, arguments []string) (any, error) {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	output := flags.String("out", "", "backup output path")
	if err := flags.Parse(arguments); err != nil {
		return nil, err
	}
	if *output == "" {
		return nil, errors.New("backup --out is required")
	}
	payload, err := service.Backup(ctx, actor)
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(*output, payload, 0o600); err != nil {
		return nil, err
	}
	return map[string]any{"operation": "backup", "path": *output, "bytes": len(payload)}, nil
}
func restore(ctx context.Context, service *operations.Service, actor operations.Actor, arguments []string) (any, error) {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	input := flags.String("file", "", "backup archive path")
	dryRun := flags.Bool("dry-run", false, "validate without mutation")
	if err := flags.Parse(arguments); err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(*input)
	if err != nil {
		return nil, err
	}
	return service.Restore(ctx, actor, payload, *dryRun)
}
func uninstall(ctx context.Context, service *operations.Service, actor operations.Actor, arguments []string) (any, error) {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	preserve := flags.Bool("preserve-data", true, "preserve configuration and state")
	dryRun := flags.Bool("dry-run", false, "report without mutation")
	if err := flags.Parse(arguments); err != nil {
		return nil, err
	}
	return service.Uninstall(ctx, actor, *preserve, *dryRun)
}
func removeRole(arguments []string) (any, error) {
	flags := flag.NewFlagSet("remove-role", flag.ContinueOnError)
	role := flags.String("role", "", "optional role name")
	dryRun := flags.Bool("dry-run", false, "report without mutation")
	if err := flags.Parse(arguments); err != nil {
		return nil, err
	}
	if *role == "" {
		return nil, errors.New("remove-role --role is required")
	}
	return map[string]any{"operation": "remove-role", "role": *role, "dryRun": *dryRun, "status": "planned; optional role installers are not yet active"}, nil
}

func configure(ctx context.Context, service *operations.Service, actor operations.Actor, arguments []string) (any, error) {
	flags := flag.NewFlagSet("configure", flag.ContinueOnError)
	candidate := flags.String("file", "", "validated candidate YAML path")
	dryRun := flags.Bool("dry-run", false, "validate without activation")
	if err := flags.Parse(arguments); err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(*candidate)
	if err != nil {
		return nil, err
	}
	return service.ApplyConfiguration(ctx, actor, payload, *dryRun)
}
