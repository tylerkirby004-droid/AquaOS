package homeassistant

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HistoryStatus reports the optional AquaOS history app lifecycle and the
// connection values needed by AquaOS Core.
type HistoryStatus struct {
	Installed       bool   `json:"installed"`
	Running         bool   `json:"running"`
	Version         string `json:"version,omitempty"`
	PanelPath       string `json:"panelPath,omitempty"`
	InfluxURL       string `json:"influxUrl,omitempty"`
	Organization    string `json:"organization,omitempty"`
	Bucket          string `json:"bucket,omitempty"`
	TokenFile       string `json:"tokenFile,omitempty"`
	RestartRequired bool   `json:"restartRequired"`
}

// SupervisorClient performs the narrowly scoped companion-app operations
// explicitly requested from the AquaOS panel.
type SupervisorClient struct {
	baseURL        string
	token          string
	secretPath     string
	coreSecretPath string
	client         *http.Client
}

// NewSupervisorClient constructs a Home Assistant Supervisor client.
func NewSupervisorClient(baseURL, token, secretPath, coreSecretPath string, timeout time.Duration) (*SupervisorClient, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(token) == "" || strings.TrimSpace(secretPath) == "" || strings.TrimSpace(coreSecretPath) == "" || timeout <= 0 {
		return nil, errors.New("supervisor URL, token, secret paths, and timeout are required")
	}
	return &SupervisorClient{baseURL: strings.TrimRight(baseURL, "/"), token: token, secretPath: secretPath, coreSecretPath: coreSecretPath, client: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: timeout}}, nil
}

// History returns the companion-app status without changing Supervisor state.
func (c *SupervisorClient) History(ctx context.Context) (HistoryStatus, error) {
	historySlug, err := c.historySlug(ctx)
	if err != nil {
		return HistoryStatus{}, err
	}
	var info struct {
		Version string `json:"version"`
		State   string `json:"state"`
	}
	status, err := c.request(ctx, http.MethodGet, "/addons/"+historySlug+"/info", nil, &info)
	if err != nil {
		return HistoryStatus{}, err
	}
	if status == http.StatusNotFound {
		return c.status(historySlug, false, false, ""), nil
	}
	return c.status(historySlug, info.Version != "", info.State == "started", info.Version), nil
}

// SetupHistory installs, configures, and starts the optional history app. It
// generates credentials internally and never returns them to the browser.
func (c *SupervisorClient) SetupHistory(ctx context.Context) (HistoryStatus, error) {
	current, err := c.History(ctx)
	if err != nil {
		return HistoryStatus{}, err
	}
	historySlug, err := c.historySlug(ctx)
	if err != nil {
		return HistoryStatus{}, err
	}
	if !current.Installed {
		// A newly published companion app may not yet exist in Supervisor's
		// cached store. Reloading here keeps installation genuinely one-click.
		if _, reloadErr := c.request(ctx, http.MethodPost, "/store/reload", map[string]any{}, nil); reloadErr != nil {
			return HistoryStatus{}, fmt.Errorf("refresh Home Assistant app store: %w", reloadErr)
		}
		status, installErr := c.request(ctx, http.MethodPost, "/addons/"+historySlug+"/install", map[string]any{}, nil)
		if installErr != nil {
			return HistoryStatus{}, fmt.Errorf("install AquaOS Advanced Trends: %w", installErr)
		}
		if status < 200 || status >= 300 {
			return HistoryStatus{}, fmt.Errorf("install AquaOS Advanced Trends returned HTTP %d", status)
		}
	}
	token, err := randomSecret(48)
	if err != nil {
		return HistoryStatus{}, err
	}
	password, err := randomSecret(24)
	if err != nil {
		return HistoryStatus{}, err
	}
	options := map[string]any{"options": map[string]string{"influx_token": token, "influx_admin_password": password}}
	status, err := c.request(ctx, http.MethodPost, "/addons/"+historySlug+"/options", options, nil)
	if err != nil {
		return HistoryStatus{}, fmt.Errorf("configure AquaOS Advanced Trends: %w", err)
	}
	if status < 200 || status >= 300 {
		return HistoryStatus{}, fmt.Errorf("configure AquaOS Advanced Trends returned HTTP %d", status)
	}
	if err = writeSecretAtomic(c.secretPath, []byte(token+"\n")); err != nil {
		return HistoryStatus{}, fmt.Errorf("store InfluxDB credential: %w", err)
	}
	status, err = c.request(ctx, http.MethodPost, "/addons/"+historySlug+"/start", map[string]any{}, nil)
	if err != nil {
		return HistoryStatus{}, fmt.Errorf("start AquaOS Advanced Trends: %w", err)
	}
	if status < 200 || status >= 300 {
		return HistoryStatus{}, fmt.Errorf("start AquaOS Advanced Trends returned HTTP %d", status)
	}
	result := c.status(historySlug, true, true, "")
	result.RestartRequired = true
	return result, nil
}

func (c *SupervisorClient) status(historySlug string, installed, running bool, version string) HistoryStatus {
	host := strings.ReplaceAll(historySlug, "_", "-")
	return HistoryStatus{Installed: installed, Running: running, Version: version, PanelPath: "/" + historySlug, InfluxURL: "http://" + host + ":8086", Organization: "aquaos", Bucket: "aquaos", TokenFile: c.coreSecretPath}
}

func (c *SupervisorClient) historySlug(ctx context.Context) (string, error) {
	var info struct {
		Slug string `json:"slug"`
	}
	status, err := c.request(ctx, http.MethodGet, "/addons/self/info", nil, &info)
	if err != nil {
		return "", fmt.Errorf("resolve AquaOS companion app identifier: %w", err)
	}
	if status != http.StatusOK || !strings.HasSuffix(info.Slug, "_aquaos") {
		return "", errors.New("supervisor returned an invalid AquaOS app identifier")
	}
	return strings.TrimSuffix(info.Slug, "_aquaos") + "_aquaos_history", nil
}

func (c *SupervisorClient) request(ctx context.Context, method, path string, payload any, target any) (int, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return response.StatusCode, nil
	}
	var envelope struct {
		Data    json.RawMessage `json:"data"`
		Result  string          `json:"result"`
		Message string          `json:"message"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(&envelope); err != nil && response.StatusCode >= 400 {
		return response.StatusCode, fmt.Errorf("supervisor returned HTTP %d", response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Result == "error" {
		return response.StatusCode, fmt.Errorf("supervisor returned HTTP %d: %s", response.StatusCode, envelope.Message)
	}
	if target != nil && len(envelope.Data) > 0 {
		if err = json.Unmarshal(envelope.Data, target); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}

func randomSecret(size int) (string, error) {
	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func writeSecretAtomic(path string, payload []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".aquaos-history-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err = file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err = file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
