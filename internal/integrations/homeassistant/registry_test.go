package homeassistant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRegistryClientReturnsMergedReadOnlyInventory(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = connection.Close() }()
		_ = connection.WriteJSON(map[string]string{"type": "auth_required"})
		var auth map[string]any
		_ = connection.ReadJSON(&auth)
		if auth["access_token"] != "test-token" {
			t.Errorf("token = %v", auth["access_token"])
			return
		}
		_ = connection.WriteJSON(map[string]string{"type": "auth_ok"})
		results := []any{
			[]map[string]any{{"id": "device-1", "name": "Shelly Plug", "manufacturer": "Shelly", "model": "Plug US Gen4", "sw_version": "1.2.3", "configuration_url": "http://shelly.local"}},
			[]map[string]any{{"entity_id": "switch.return_pump", "device_id": "device-1", "original_name": "Return pump", "platform": "shelly"}},
			[]map[string]any{{"entity_id": "switch.return_pump", "state": "off", "attributes": map[string]any{"friendly_name": "Main return pump"}}},
		}
		for index, result := range results {
			var command map[string]any
			_ = connection.ReadJSON(&command)
			_ = connection.WriteJSON(map[string]any{"id": index + 1, "type": "result", "success": true, "result": result})
		}
	}))
	defer server.Close()

	client, err := NewRegistryClient("ws"+strings.TrimPrefix(server.URL, "http"), "test-token", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := client.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Manufacturer != "Shelly" || devices[0].ConfigurationURL != "http://shelly.local" || len(devices[0].Entities) != 1 || devices[0].Entities[0].Name != "Main return pump" {
		t.Fatalf("unexpected registry: %+v", devices)
	}
}

func TestRegistryClientRejectsAuthenticationFailure(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = connection.Close() }()
		_ = connection.WriteJSON(map[string]string{"type": "auth_required"})
		var auth map[string]any
		_ = connection.ReadJSON(&auth)
		_ = connection.WriteJSON(map[string]string{"type": "auth_invalid"})
	}))
	defer server.Close()
	client, err := NewRegistryClient("ws"+strings.TrimPrefix(server.URL, "http"), "test-token", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Devices(context.Background()); err == nil {
		t.Fatal("authentication failure was accepted")
	}
}
