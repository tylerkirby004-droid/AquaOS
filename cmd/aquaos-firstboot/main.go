// Command aquaos-firstboot provides authenticated one-time appliance onboarding.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/firstboot"
)

type installer struct{ script, payload, version, checksum, complete, adminToken string }

func (i installer) Install(ctx context.Context, request firstboot.InstallRequest) (firstboot.InstallResult, error) {
	if _, err := os.Stat(i.complete); err == nil {
		return firstboot.InstallResult{}, errors.New("appliance installation is already complete")
	} else if !errors.Is(err, os.ErrNotExist) {
		return firstboot.InstallResult{}, fmt.Errorf("inspect completion marker: %w", err)
	}
	args := []string{"--release", i.payload, "--repository", i.payload, "--version", i.version, "--sha256", i.checksum, "--site-id", request.SiteID, "--address", request.Address, "--timezone", request.Timezone, "--admin-token", i.adminToken, "--apply", "--ack-dedicated-appliance", "--ack-independent-safeguards"}
	if request.AdvancedHistory {
		args = append(args, "--advanced-history")
	}
	command := exec.CommandContext(ctx, i.script, args...)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return firstboot.InstallResult{}, fmt.Errorf("run signed installer: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(i.complete), 0o750); err != nil {
		return firstboot.InstallResult{}, fmt.Errorf("create state directory: %w", err)
	}
	if err := os.WriteFile(i.complete, []byte("complete\n"), 0o600); err != nil {
		return firstboot.InstallResult{}, fmt.Errorf("record completion: %w", err)
	}
	return firstboot.InstallResult{AdminAccessToken: i.adminToken}, nil
}

func main() {
	address := flag.String("address", ":8443", "temporary onboarding listen address")
	tlsCert := flag.String("tls-cert", "/run/aquaos-firstboot.crt", "temporary TLS certificate")
	tlsKey := flag.String("tls-key", "/run/aquaos-firstboot.key", "temporary TLS private key")
	payload := flag.String("payload", "/usr/share/aquaos-installer", "signed installer payload")
	version := flag.String("version", "", "embedded AquaOS version")
	checksum := flag.String("sha256", "", "embedded AquaOS Core SHA-256")
	complete := flag.String("complete-file", "/var/lib/aquaos/firstboot.complete", "completion marker")
	flag.Parse()
	if *version == "" || len(*checksum) != 64 {
		slog.Error("valid embedded version and checksum are required")
		os.Exit(2)
	}
	tokenBytes := make([]byte, 8)
	if _, err := rand.Read(tokenBytes); err != nil {
		slog.Error("generate setup code", "error", err)
		os.Exit(1)
	}
	token := hex.EncodeToString(tokenBytes)
	adminTokenBytes := make([]byte, 32)
	if _, err := rand.Read(adminTokenBytes); err != nil {
		slog.Error("generate Admin handoff", "error", err)
		os.Exit(1)
	}
	adminToken := hex.EncodeToString(adminTokenBytes)
	displayToken := strings.Join([]string{token[0:4], token[4:8], token[8:12], token[12:16]}, "-")
	if err := os.WriteFile("/run/aquaos-setup-code", []byte(displayToken+"\n"), 0o600); err != nil {
		slog.Error("write setup code", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	lanAddress := waitForPrivateIPv4(ctx, 90*time.Second)
	url := "https://aquaos.local:8443"
	if lanAddress != "" {
		url = "https://" + lanAddress + ":8443"
	}
	message := "\n\nAquaOS browser setup is ready.\n\nOpen: " + url + "\nSetup code: " + displayToken + "\n\nNo Linux login or terminal commands are required.\n"
	_ = os.WriteFile("/dev/tty1", []byte(message), 0o600)
	srv, err := firstboot.NewServer(ctx, token, lanAddress, installer{script: filepath.Join(*payload, "scripts/install-appliance.sh"), payload: *payload, version: *version, checksum: *checksum, complete: *complete, adminToken: adminToken})
	if err != nil {
		slog.Error("create onboarding server", "error", err)
		os.Exit(1)
	}
	httpServer := &http.Server{Addr: *address, Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Minute, IdleTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	slog.Info("first-boot onboarding ready", "url", url)
	if err := httpServer.ListenAndServeTLS(*tlsCert, *tlsKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve onboarding", "error", err)
		os.Exit(1)
	}
}

func waitForPrivateIPv4(ctx context.Context, timeout time.Duration) string {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if address := privateIPv4(); address != "" {
			return address
		}
		select {
		case <-ctx.Done():
			return ""
		case <-deadline.C:
			return ""
		case <-ticker.C:
		}
	}
}

func privateIPv4() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || strings.HasPrefix(iface.Name, "docker") || strings.HasPrefix(iface.Name, "veth") || strings.HasPrefix(iface.Name, "br-") || strings.HasPrefix(iface.Name, "virbr") {
			continue
		}
		addresses, addressErr := iface.Addrs()
		if addressErr != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, parseErr := net.ParseCIDR(address.String())
			if parseErr == nil && ip.To4() != nil && ip.IsPrivate() {
				return ip.String()
			}
		}
	}
	return ""
}
