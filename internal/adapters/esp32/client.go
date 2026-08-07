package esp32

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const maximumSnapshotBytes = 64 * 1024

// ProbeDTO is one wire-level DS18B20 observation.
type ProbeDTO struct {
	SensorID  string   `json:"sensorId"`
	Celsius   *float64 `json:"celsius"`
	Valid     bool     `json:"valid"`
	ErrorCode string   `json:"errorCode,omitempty"`
}

// SnapshotDTO is the versioned ESP32 node response. BootID distinguishes a
// device reboot from an out-of-order sequence.
type SnapshotDTO struct {
	SchemaVersion string     `json:"schemaVersion"`
	NodeID        string     `json:"nodeId"`
	Firmware      string     `json:"firmware"`
	BootID        string     `json:"bootId"`
	Sequence      uint64     `json:"sequence"`
	ObservedAt    time.Time  `json:"observedAt"`
	Probes        []ProbeDTO `json:"probes"`
}

// Client is the consumer-owned ESP32 transport boundary.
type Client interface {
	Snapshot(context.Context, string, string) (SnapshotDTO, error)
}

// HTTPClient fetches bounded versioned snapshots from an AquaOS ESP32 node.
type HTTPClient struct{ client *http.Client }

// NewHTTPClient constructs an ESP32 HTTP client.
func NewHTTPClient(client *http.Client) (*HTTPClient, error) {
	if client == nil {
		return nil, errors.New("esp32 HTTP client is required")
	}
	return &HTTPClient{client: client}, nil
}

// Snapshot fetches `/aquaos/v1/snapshot`; bearerToken is never placed in URLs.
func (c *HTTPClient) Snapshot(ctx context.Context, baseURL, bearerToken string) (SnapshotDTO, error) {
	endpoint, err := snapshotURL(baseURL)
	if err != nil {
		return SnapshotDTO{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return SnapshotDTO{}, fmt.Errorf("create ESP32 snapshot request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return SnapshotDTO{}, fmt.Errorf("fetch ESP32 snapshot: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maximumSnapshotBytes+1))
	if err != nil {
		return SnapshotDTO{}, fmt.Errorf("read ESP32 snapshot: %w", err)
	}
	if len(payload) > maximumSnapshotBytes {
		return SnapshotDTO{}, errors.New("esp32 snapshot exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return SnapshotDTO{}, fmt.Errorf("esp32 snapshot returned HTTP status %d", response.StatusCode)
	}
	var snapshot SnapshotDTO
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return SnapshotDTO{}, fmt.Errorf("decode ESP32 snapshot: %w", err)
	}
	return snapshot, nil
}

func snapshotURL(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("esp32 base URL must be an http or https URL with a host and no credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("esp32 base URL must not contain a query or fragment")
	}
	parsed.Path = "/aquaos/v1/snapshot"
	return parsed.String(), nil
}
