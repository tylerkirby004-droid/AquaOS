// Command aquaos-admin runs the temporary authenticated recovery GUI.
package main

import (
	"context"
	"encoding/json"
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
	"github.com/tylerkirby004-droid/aquaos/internal/integrations/homeassistant"
	"github.com/tylerkirby004-droid/aquaos/internal/operations"
)

func main() { os.Exit(run()) }
func run() int {
	address := flag.String("address", "127.0.0.1:8090", "Admin GUI listen address")
	tokenFile := flag.String("token-file", "", "bearer token file")
	tlsCertificate := flag.String("tls-cert", "", "TLS certificate PEM file")
	tlsKey := flag.String("tls-key", "", "TLS private-key PEM file")
	trustedIngress := flag.Bool("trusted-ingress", false, "delegate browser authentication to a trusted local ingress proxy")
	trustedIngressCIDR := flag.String("trusted-ingress-cidr", "", "trusted ingress reverse-proxy CIDR")
	coreURL := flag.String("core-url", "http://localhost:8080", "local AquaOS Core API base URL")
	coreTokenFile := flag.String("core-token-file", "", "AquaOS Core API token file")
	homeAssistantWebSocket := flag.String("home-assistant-websocket", "", "Home Assistant WebSocket proxy URL")
	supervisorURL := flag.String("supervisor-url", "", "Home Assistant Supervisor API base URL")
	historyTokenFile := flag.String("history-token-file", "", "legacy generated InfluxDB token file")
	historyCoreTokenFile := flag.String("history-core-token-file", "", "legacy InfluxDB token path visible to AquaOS Core")
	root := flag.String("root", "/", "managed dedicated-appliance root")
	authenticationRate := flag.Int("authentication-rate", 5, "authentication attempts per second per client")
	authenticationBurst := flag.Int("authentication-burst", 10, "authentication attempt burst per client")
	mutationRate := flag.Int("mutation-rate", 2, "mutations per second per client")
	mutationBurst := flag.Int("mutation-burst", 4, "mutation burst per client")
	flag.Parse()
	var token string
	var err error
	if !*trustedIngress {
		token, err = readToken(*tokenFile)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			return 1
		}
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
	discoveryService, err := discovery.New(shellyClient, esp32Client, 4, 3*time.Second, readToken)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	options := []admin.Option{admin.WithDiscovery(discoveryService)}
	if *homeAssistantWebSocket != "" {
		homeAssistantClient, clientErr := homeassistant.NewRegistryClient(*homeAssistantWebSocket, os.Getenv("SUPERVISOR_TOKEN"), 15*time.Second)
		if clientErr != nil {
			_, _ = fmt.Fprintln(os.Stderr, clientErr)
			return 1
		}
		options = append(options, admin.WithHomeAssistantRegistry(homeAssistantClient))
	}
	if *supervisorURL != "" || *historyTokenFile != "" || *historyCoreTokenFile != "" {
		logger.Info("legacy history setup flags ignored; Home Assistant recorder is the supported history surface")
	}
	if *coreTokenFile != "" {
		coreToken, tokenErr := readToken(*coreTokenFile)
		if tokenErr != nil {
			_, _ = fmt.Fprintln(os.Stderr, tokenErr)
			return 1
		}
		options = append(options, admin.WithRuntimeHealth(&coreHealthClient{client: httpClient, baseURL: strings.TrimRight(*coreURL, "/"), token: coreToken}))
	}
	server, err := admin.New(admin.Config{Address: *address, Token: token, MaximumRequestBytes: 32 * 1024 * 1024, ShutdownTimeout: 10 * time.Second, AuthenticationRate: *authenticationRate, AuthenticationBurst: *authenticationBurst, MutationRate: *mutationRate, MutationBurst: *mutationBurst, TLSCertificateFile: *tlsCertificate, TLSKeyFile: *tlsKey, TrustedIngress: *trustedIngress, TrustedIngressCIDR: *trustedIngressCIDR}, service, logger.With("component", "admin"), options...)
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

type coreHealthClient struct {
	client  *http.Client
	baseURL string
	token   string
}

func (c *coreHealthClient) Report(ctx context.Context) (any, error) {
	healthReport, err := c.get(ctx, "/api/v1/health")
	if err != nil {
		return nil, err
	}
	stateReport, err := c.get(ctx, "/api/v1/state")
	if err != nil {
		return nil, err
	}
	activeAlarms, err := c.get(ctx, "/api/v1/alarms?status=active")
	if err != nil {
		return nil, err
	}
	return map[string]any{"health": healthReport, "canonicalState": stateReport, "activeAlarms": activeAlarms}, nil
}

func (c *coreHealthClient) get(ctx context.Context, path string) (any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("core health returned HTTP %d", response.StatusCode)
	}
	var report any
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1024*1024))
	if err = decoder.Decode(&report); err != nil {
		return nil, err
	}
	return report, nil
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
