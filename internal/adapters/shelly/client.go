package shelly

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

const maximumResponseBytes = 64 * 1024

// SwitchStatus is the supported boundary subset of Switch.GetStatus. Unknown
// additive firmware fields are deliberately ignored.
type SwitchStatus struct {
	ID      int     `json:"id"`
	Source  string  `json:"source"`
	Output  bool    `json:"output"`
	APower  float64 `json:"apower"`
	Voltage float64 `json:"voltage"`
	Current float64 `json:"current"`
	Uptime  uint64  `json:"-"`
}

// SetResult is the supported Switch.Set response.
type SetResult struct {
	WasOn bool `json:"was_on"`
}

// SwitchConfig is the supported boundary subset of Switch.GetConfig.
type SwitchConfig struct {
	ID           int    `json:"id"`
	InitialState string `json:"initial_state"`
}

// Client is the consumer-owned Shelly RPC boundary.
type Client interface {
	GetSwitchStatus(context.Context, string, int) (SwitchStatus, error)
	GetSwitchConfig(context.Context, string, int) (SwitchConfig, error)
	SetSwitch(context.Context, string, int, bool) (SetResult, error)
}

// HTTPClient calls Shelly's local HTTP RPC endpoints with bounded bodies.
type HTTPClient struct {
	client *http.Client
}

// GetSwitchConfig invokes Switch.GetConfig for power-return verification.
func (c *HTTPClient) GetSwitchConfig(ctx context.Context, baseURL string, channel int) (SwitchConfig, error) {
	var config SwitchConfig
	if err := c.call(ctx, baseURL, "Switch.GetConfig", struct {
		ID int `json:"id"`
	}{ID: channel}, &config); err != nil {
		return SwitchConfig{}, err
	}
	if config.ID != channel {
		return SwitchConfig{}, fmt.Errorf("shelly config channel %d does not match requested channel %d", config.ID, channel)
	}
	return config, nil
}

// NewHTTPClient constructs a client. Request deadlines are supplied by the
// adapter through context; the transport client must still be non-nil.
func NewHTTPClient(client *http.Client) (*HTTPClient, error) {
	if client == nil {
		return nil, errors.New("shelly HTTP client is required")
	}
	return &HTTPClient{client: client}, nil
}

// GetSwitchStatus invokes Switch.GetStatus for one channel.
func (c *HTTPClient) GetSwitchStatus(ctx context.Context, baseURL string, channel int) (SwitchStatus, error) {
	var status SwitchStatus
	if err := c.call(ctx, baseURL, "Switch.GetStatus", struct {
		ID int `json:"id"`
	}{ID: channel}, &status); err != nil {
		return SwitchStatus{}, err
	}
	if status.ID != channel {
		return SwitchStatus{}, fmt.Errorf("shelly response channel %d does not match requested channel %d", status.ID, channel)
	}
	return status, nil
}

// SetSwitch invokes Switch.Set for one channel.
func (c *HTTPClient) SetSwitch(ctx context.Context, baseURL string, channel int, on bool) (SetResult, error) {
	var result SetResult
	err := c.call(ctx, baseURL, "Switch.Set", struct {
		ID int  `json:"id"`
		On bool `json:"on"`
	}{ID: channel, On: on}, &result)
	return result, err
}

func (c *HTTPClient) call(ctx context.Context, baseURL, method string, params, result any) error {
	endpoint, err := rpcURL(baseURL, method)
	if err != nil {
		return err
	}
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encode Shelly RPC request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Shelly RPC request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("call Shelly RPC: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	limited := io.LimitReader(response.Body, maximumResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read Shelly RPC response: %w", err)
	}
	if len(payload) > maximumResponseBytes {
		return errors.New("shelly RPC response exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("shelly RPC returned HTTP status %d", response.StatusCode)
	}
	if err := json.Unmarshal(payload, result); err != nil {
		return fmt.Errorf("decode Shelly RPC response: %w", err)
	}
	return nil
}

func rpcURL(baseURL, method string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("shelly base URL must be an unauthenticated http URL with a host")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("shelly base URL must not contain a query or fragment")
	}
	parsed.Path = "/rpc/" + method
	return parsed.String(), nil
}

// ChannelString returns a stable diagnostic representation of a channel.
func ChannelString(channel int) string { return strconv.Itoa(channel) }
