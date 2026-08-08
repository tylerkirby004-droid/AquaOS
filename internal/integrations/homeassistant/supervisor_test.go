package homeassistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSupervisorClientInstallsHistoryWithoutExposingCredentials(t *testing.T) {
	var mu sync.Mutex
	installed := false
	configured := false
	started := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer supervisor-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "POST /store/reload":
			writeSupervisorResponse(w, nil)
		case "GET /addons/self/info":
			writeSupervisorResponse(w, map[string]any{"slug": "265912a1_aquaos"})
		case "GET /addons/265912a1_aquaos_history/info":
			if !installed {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			state := "stopped"
			if started {
				state = "started"
			}
			writeSupervisorResponse(w, map[string]any{"version": "0.1.0-history", "state": state})
		case "POST /addons/265912a1_aquaos_history/install":
			installed = true
			writeSupervisorResponse(w, nil)
		case "POST /addons/265912a1_aquaos_history/options":
			var payload struct {
				Options map[string]any `json:"options"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			token, tokenOK := payload.Options["influx_token"].(string)
			password, passwordOK := payload.Options["influx_admin_password"].(string)
			_, envOK := payload.Options["env_vars"].([]any)
			configured = tokenOK && passwordOK && envOK && len(token) >= 48 && len(password) >= 24
			writeSupervisorResponse(w, nil)
		case "POST /addons/265912a1_aquaos_history/start":
			started = true
			writeSupervisorResponse(w, nil)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	secretPath := filepath.Join(t.TempDir(), "influxdb.token")
	client, err := NewSupervisorClient(server.URL, "supervisor-token", secretPath, "/config/influxdb.token", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	status, err := client.SetupHistory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !configured || !started || !status.Running || status.PanelPath != "/265912a1_aquaos_history" || status.InfluxURL != "http://265912a1-aquaos-history:8086" || status.TokenFile != "/config/influxdb.token" {
		t.Fatalf("unexpected setup status: %+v", status)
	}
	payload, err := os.ReadFile(secretPath)
	if err != nil || len(strings.TrimSpace(string(payload))) < 48 {
		t.Fatal("generated history credential was not stored")
	}
	info, err := os.Stat(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode = %v", info.Mode().Perm())
	}
}

func writeSupervisorResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"result": "ok", "data": data})
}
