// Command aquaos-admin runs the temporary authenticated recovery GUI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/adapters/esp32"
	"github.com/tylerkirby004-droid/aquaos/internal/adapters/shelly"
	"github.com/tylerkirby004-droid/aquaos/internal/admin"
	"github.com/tylerkirby004-droid/aquaos/internal/discovery"
	"github.com/tylerkirby004-droid/aquaos/internal/operations"
)

func main() { os.Exit(run()) }
func run() int {
	address := flag.String("address", "127.0.0.1:8090", "Admin GUI listen address")
	tokenFile := flag.String("token-file", "", "bearer token file")
	root := flag.String("root", "/", "managed Control VM root")
	authenticationRate := flag.Int("authentication-rate", 5, "authentication attempts per second per client")
	authenticationBurst := flag.Int("authentication-burst", 10, "authentication attempt burst per client")
	mutationRate := flag.Int("mutation-rate", 2, "mutations per second per client")
	mutationBurst := flag.Int("mutation-burst", 4, "mutation burst per client")
	flag.Parse()
	token, err := readToken(*tokenFile)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	host, err := operations.NewLocalHost(*root)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	service, err := operations.New(host, logger.With("component", "operations"))
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	httpClient := &http.Client{Transport: &http.Transport{Proxy: nil, MaxIdleConns: 16, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}}
	shellyClient, err := shelly.NewHTTPClient(httpClient)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	esp32Client, err := esp32.NewHTTPClient(httpClient)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	discoveryService, err := discovery.New(shellyClient, esp32Client, 4, 3*time.Second)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	server, err := admin.New(admin.Config{Address: *address, Token: token, MaximumRequestBytes: 32 * 1024 * 1024, ShutdownTimeout: 10 * time.Second, AuthenticationRate: *authenticationRate, AuthenticationBurst: *authenticationBurst, MutationRate: *mutationRate, MutationBurst: *mutationBurst}, service, logger.With("component", "admin"), admin.WithDiscovery(discoveryService))
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err = server.Start(ctx); err != nil {
		logger.Error("Admin GUI startup failed", "error", err)
		return 1
	}
	logger.Info("Admin GUI started", "address", *address)
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err = server.Stop(shutdownCtx); err != nil {
		logger.Error("Admin GUI shutdown failed", "error", err)
		return 1
	}
	return 0
}
func readToken(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("token-file is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	payload, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(payload))
	if len(payload) > 4096 || token == "" {
		return "", fmt.Errorf("token file must contain 1 to 4096 bytes")
	}
	return token, nil
}
