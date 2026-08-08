package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// RegistryDevice is a physical or logical device already known to Home
// Assistant. Importing is read-only against Home Assistant; supported AquaOS
// adapters are configured separately through validated AquaOS configuration.
type RegistryDevice struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Manufacturer     string           `json:"manufacturer,omitempty"`
	Model            string           `json:"model,omitempty"`
	Firmware         string           `json:"firmware,omitempty"`
	AreaID           string           `json:"areaId,omitempty"`
	ConfigurationURL string           `json:"configurationUrl,omitempty"`
	Entities         []RegistryEntity `json:"entities"`
}

// RegistryEntity describes a Home Assistant entity attached to a device.
type RegistryEntity struct {
	EntityID    string `json:"entityId"`
	Name        string `json:"name"`
	Platform    string `json:"platform,omitempty"`
	DeviceClass string `json:"deviceClass,omitempty"`
	Unit        string `json:"unit,omitempty"`
	State       string `json:"state,omitempty"`
	Disabled    bool   `json:"disabled"`
}

// RegistryClient reads Home Assistant's registries over its authenticated
// WebSocket API. It never invokes services or changes Home Assistant state.
type RegistryClient struct {
	url     string
	token   string
	timeout time.Duration
	dialer  *websocket.Dialer
}

// NewRegistryClient constructs a bounded, read-only Home Assistant client.
func NewRegistryClient(url, token string, timeout time.Duration) (*RegistryClient, error) {
	if strings.TrimSpace(url) == "" || strings.TrimSpace(token) == "" || timeout <= 0 {
		return nil, errors.New("home assistant WebSocket URL, token, and timeout are required")
	}
	return &RegistryClient{url: url, token: token, timeout: timeout, dialer: &websocket.Dialer{HandshakeTimeout: timeout, Proxy: http.ProxyFromEnvironment}}, nil
}

// Devices returns Home Assistant devices with their registered entities.
func (c *RegistryClient) Devices(ctx context.Context) ([]RegistryDevice, error) {
	connection, response, err := c.dialer.DialContext(ctx, c.url, nil)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		return nil, fmt.Errorf("connect Home Assistant registry: %w", err)
	}
	defer func() { _ = connection.Close() }()
	connection.SetReadLimit(8 * 1024 * 1024)
	deadline := time.Now().Add(c.timeout)
	_ = connection.SetReadDeadline(deadline)
	_ = connection.SetWriteDeadline(deadline)

	var phase struct {
		Type string `json:"type"`
	}
	if err = connection.ReadJSON(&phase); err != nil || phase.Type != "auth_required" {
		return nil, errors.New("home assistant did not request WebSocket authentication")
	}
	if err = connection.WriteJSON(map[string]string{"type": "auth", "access_token": c.token}); err != nil {
		return nil, fmt.Errorf("authenticate Home Assistant registry: %w", err)
	}
	if err = connection.ReadJSON(&phase); err != nil || phase.Type != "auth_ok" {
		return nil, errors.New("home assistant rejected registry authentication")
	}

	var devices []registryDevice
	if err = c.command(connection, 1, "config/device_registry/list", &devices); err != nil {
		return nil, err
	}
	var entities []registryEntity
	if err = c.command(connection, 2, "config/entity_registry/list", &entities); err != nil {
		return nil, err
	}
	var states []registryState
	if err = c.command(connection, 3, "get_states", &states); err != nil {
		return nil, err
	}
	return mergeRegistry(devices, entities, states), nil
}

func (*RegistryClient) command(connection *websocket.Conn, id int, kind string, target any) error {
	if err := connection.WriteJSON(map[string]any{"id": id, "type": kind}); err != nil {
		return fmt.Errorf("request Home Assistant %s: %w", kind, err)
	}
	var response struct {
		ID      int             `json:"id"`
		Type    string          `json:"type"`
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := connection.ReadJSON(&response); err != nil {
		return fmt.Errorf("read Home Assistant %s: %w", kind, err)
	}
	if response.ID != id || response.Type != "result" || !response.Success {
		return fmt.Errorf("home assistant %s failed: %s", kind, response.Error.Message)
	}
	if err := json.Unmarshal(response.Result, target); err != nil {
		return fmt.Errorf("decode Home Assistant %s: %w", kind, err)
	}
	return nil
}

type registryDevice struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	NameByUser       string `json:"name_by_user"`
	Manufacturer     string `json:"manufacturer"`
	Model            string `json:"model"`
	SWVersion        string `json:"sw_version"`
	AreaID           string `json:"area_id"`
	ConfigurationURL string `json:"configuration_url"`
}

type registryEntity struct {
	EntityID     string `json:"entity_id"`
	DeviceID     string `json:"device_id"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	Platform     string `json:"platform"`
	DisabledBy   string `json:"disabled_by"`
}

type registryState struct {
	EntityID   string         `json:"entity_id"`
	State      string         `json:"state"`
	Attributes map[string]any `json:"attributes"`
}

func mergeRegistry(devices []registryDevice, entities []registryEntity, states []registryState) []RegistryDevice {
	stateByID := make(map[string]registryState, len(states))
	for _, state := range states {
		stateByID[state.EntityID] = state
	}
	entitiesByDevice := make(map[string][]RegistryEntity)
	for _, entity := range entities {
		if entity.DeviceID == "" {
			continue
		}
		state := stateByID[entity.EntityID]
		name := entity.Name
		if name == "" {
			name = entity.OriginalName
		}
		if friendly, ok := state.Attributes["friendly_name"].(string); ok && friendly != "" {
			name = friendly
		}
		deviceClass, _ := state.Attributes["device_class"].(string)
		unit, _ := state.Attributes["unit_of_measurement"].(string)
		entitiesByDevice[entity.DeviceID] = append(entitiesByDevice[entity.DeviceID], RegistryEntity{EntityID: entity.EntityID, Name: name, Platform: entity.Platform, DeviceClass: deviceClass, Unit: unit, State: state.State, Disabled: entity.DisabledBy != ""})
	}
	result := make([]RegistryDevice, 0, len(devices))
	for _, device := range devices {
		name := device.NameByUser
		if name == "" {
			name = device.Name
		}
		result = append(result, RegistryDevice{ID: device.ID, Name: name, Manufacturer: device.Manufacturer, Model: device.Model, Firmware: device.SWVersion, AreaID: device.AreaID, ConfigurationURL: device.ConfigurationURL, Entities: entitiesByDevice[device.ID]})
	}
	return result
}
