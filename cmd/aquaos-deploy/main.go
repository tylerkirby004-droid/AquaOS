// Command aquaos-deploy provides dry-run-first whole-system VM orchestration.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/tylerkirby004-droid/aquaos/internal/deployment"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(arguments []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: aquaos-deploy <init|plan|apply> [options]")
		return 2
	}
	switch arguments[0] {
	case "init":
		return initialize(arguments[1:], stdin, stdout, stderr)
	case "plan", "apply":
		return execute(arguments[0], arguments[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n", arguments[0])
		return 2
	}
}

func initialize(arguments []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "aquaos-deployment.json", "deployment configuration path")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	reader := bufio.NewReader(stdin)
	ask := func(label, fallback string) string {
		if fallback == "" {
			_, _ = fmt.Fprintf(stdout, "%s: ", label)
		} else {
			_, _ = fmt.Fprintf(stdout, "%s [%s]: ", label, fallback)
		}
		value, _ := reader.ReadString('\n')
		value = strings.TrimSpace(value)
		if value == "" {
			return fallback
		}
		return value
	}
	integer := func(label string, fallback int) int {
		value, err := strconv.Atoi(ask(label, strconv.Itoa(fallback)))
		if err != nil {
			return 0
		}
		return value
	}
	_, _ = fmt.Fprintln(stdout, "AquaOS whole-system setup\nFind storage, bridge, template IDs, and unused VM IDs in the Proxmox web interface before continuing.")
	proxmox := deployment.Proxmox{Host: ask("Proxmox host name or address", ""), User: ask("Proxmox SSH administrator", "root"), Node: ask("Proxmox node name", ""), Storage: ask("VM storage", "local-lvm"), Bridge: ask("LAN bridge", "vmbr0"), IdentityFile: ask("Workstation SSH private-key file", ""), PublicKeyFile: ask("Public-key file path visible on Proxmox", ""), DebianTemplateID: integer("Approved Debian cloud template VM ID", 9000), HAOSTemplateID: integer("Approved Home Assistant OS template VM ID", 9001)}
	guest := func(role, name string, id int, cores, memory, disk int) deployment.Guest {
		address := ask(role+" VM reserved IP address", "")
		return deployment.Guest{VMID: integer(role+" unused VM ID", id), Name: ask(role+" VM name", name), Address: address, CIDR: ask(role+" VM address with prefix", address+"/24"), Gateway: ask(role+" VM gateway", ""), Cores: integer(role+" CPU cores", cores), MemoryMiB: integer(role+" memory MiB", memory), DiskGiB: integer(role+" disk GiB", disk)}
	}
	cfg := deployment.Config{Proxmox: proxmox, Control: guest("Control", "aquaos-control", 200, 2, 4096, 32), Services: guest("Services", "aquaos-services", 201, 4, 8192, 80), HomeAssistant: guest("Home Assistant", "home-assistant", 202, 2, 4096, 32), Release: deployment.Release{Directory: ask("Signed AquaOS release directory", "dist"), Version: ask("AquaOS release version", ""), SHA256: ask("AquaOS Core SHA-256", "")}, RepositoryDirectory: ask("AquaOS repository directory", ".")}
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if err = os.WriteFile(*output, append(payload, '\n'), 0o600); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "Saved %s. Run aquaos-deploy plan --config %s and review every action.\n", *output, *output)
	return 0
}

func execute(command string, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "aquaos-deployment.json", "deployment configuration")
	ackVMs := flags.Bool("ack-create-vms", false, "acknowledge creation of the three declared VMs")
	ackBackups := flags.Bool("ack-independent-backups", false, "acknowledge Proxmox/VM backups and physical safeguards")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	payload, err := os.ReadFile(*path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	var cfg deployment.Config
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	actions, err := deployment.Plan(cfg)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if command == "plan" {
		_ = encoder.Encode(actions)
		return 0
	}
	if !*ackVMs || !*ackBackups {
		_, _ = fmt.Fprintln(stderr, "apply requires --ack-create-vms and --ack-independent-backups")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	orchestrator, _ := deployment.New(deployment.CommandRunner{})
	if err = orchestrator.Apply(ctx, cfg); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	_ = encoder.Encode(map[string]any{"status": "provisioned", "actions": len(actions), "homeAssistantOnboarding": "http://" + cfg.HomeAssistant.Address + ":8123"})
	return 0
}
