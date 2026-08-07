package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

func TestNewWiresCoreServices(t *testing.T) {
	cfg := config.Defaults()
	cfg.HTTP.Address = "localhost:0"
	container, err := New(cfg, "test.yaml", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if container.Devices == nil || container.Sensors == nil || container.Equipment == nil || container.Events == nil {
		t.Fatal("core service was not wired")
	}
}

func TestBenchAdaptersIngestDirectLANStateWithoutMQTT(t *testing.T) {
	var shellyOn atomic.Bool
	var espSequence atomic.Uint64
	shellyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/rpc/Switch.GetConfig":
			_, _ = writer.Write([]byte(`{"id":0,"initial_state":"off"}`))
		case "/rpc/Switch.GetStatus":
			_, _ = fmt.Fprintf(writer, `{"id":0,"source":"http","output":%t,"apower":0,"voltage":120,"current":0}`, shellyOn.Load())
		case "/rpc/Switch.Set":
			var body struct {
				On bool `json:"on"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				http.Error(writer, "invalid", http.StatusBadRequest)
				return
			}
			wasOn := shellyOn.Swap(body.On)
			_, _ = fmt.Fprintf(writer, `{"was_on":%t}`, wasOn)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer shellyServer.Close()
	espServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		sequence := espSequence.Add(1)
		observed := time.Now().UTC().Truncate(time.Millisecond)
		_, _ = fmt.Fprintf(writer, `{"schemaVersion":"1.0","nodeId":"44444444-4444-4444-8444-444444444444","firmware":"bench","bootId":"boot-a","sequence":%d,"observedAt":"%s","probes":[{"sensorId":"55555555-5555-4555-8555-555555555555","celsius":25.0,"valid":true},{"sensorId":"66666666-6666-4666-8666-666666666666","celsius":25.1,"valid":true}]}`, sequence, observed.Format(time.RFC3339Nano))
	}))
	defer espServer.Close()

	cfg := config.Defaults()
	cfg.HTTP.Address, cfg.Simulator.Enabled = "localhost:0", false
	cfg.Bench = config.Bench{Enabled: true, SafeLoadAcknowledged: true}
	cfg.Adapters.Shelly = config.ShellyAdapter{Enabled: true, Endpoints: []config.ShellyEndpoint{{ID: "11111111-1111-4111-8111-111111111111", EquipmentID: "22222222-2222-4222-8222-222222222222", AlarmRuleID: "33333333-3333-4333-8333-333333333333", BaseURL: shellyServer.URL, Channel: 0, PollInterval: 100 * time.Millisecond, RequestTimeout: 50 * time.Millisecond, Retries: 0, PowerReturnPolicy: "off", EquipmentKind: "heater", MaximumOn: time.Minute, RequiredProbeIDs: []string{"55555555-5555-4555-8555-555555555555", "66666666-6666-4666-8666-666666666666"}}}}
	cfg.Adapters.ESP32 = config.ESP32Adapter{Enabled: true, Endpoints: []config.ESP32Endpoint{{ID: "77777777-7777-4777-8777-777777777777", DeviceID: "44444444-4444-4444-8444-444444444444", AlarmRuleID: "88888888-8888-4888-8888-888888888888", BaseURL: espServer.URL, ProbeIDs: []string{"55555555-5555-4555-8555-555555555555", "66666666-6666-4666-8666-666666666666"}, PollInterval: 100 * time.Millisecond, RequestTimeout: 50 * time.Millisecond, FreshFor: time.Second, MaximumClockSkew: time.Second, MaximumDifference: 0.5}}}
	container, err := New(cfg, "test.yaml", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	registeredSensors, err := container.Sensors.List(context.Background())
	if err != nil || len(registeredSensors) != 2 {
		t.Fatalf("configured ESP32 probes were not registered: %v %+v", err, registeredSensors)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := container.Lifecycle.Start(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		if err := container.Lifecycle.Stop(stopCtx); err != nil {
			t.Error(err)
		}
	}()
	key := state.Key{EntityKind: state.EntitySensor, EntityID: domain.EntityID("55555555-5555-4555-8555-555555555555"), Plane: state.PlaneObservation, Attribute: "measurement"}
	deadline := time.Now().Add(time.Second)
	for {
		value, getErr := container.State.Get(context.Background(), key)
		if getErr == nil && value.Quality == domain.QualityGood {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("direct ESP32 state not ingested: %v", getErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if container.MQTT != nil {
		t.Fatal("direct adapter lifecycle unexpectedly created MQTT")
	}
	issuedAt := time.Now().UTC()
	commandResult, err := container.Output.Submit(context.Background(), output.Command{IdempotencyKey: "bench-lamp-on", EquipmentID: domain.EquipmentID("22222222-2222-4222-8222-222222222222"), Requester: "bench-test", IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Second), On: true})
	if err != nil {
		t.Fatal(err)
	}
	if commandResult.Status != output.StatusAcknowledged {
		t.Fatalf("command status = %s", commandResult.Status)
	}
	deadline = time.Now().Add(time.Second)
	for {
		commandResult, err = container.Output.Get(context.Background(), commandResult.Command.ID)
		if err == nil && commandResult.Status == output.StatusSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("direct Shelly command did not reconcile: %+v %v", commandResult, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNewComposesExplicitBenchAdaptersWithoutMQTT(t *testing.T) {
	cfg := config.Defaults()
	cfg.HTTP.Address = "localhost:0"
	cfg.Simulator.Enabled = false
	cfg.Bench = config.Bench{Enabled: true, SafeLoadAcknowledged: true}
	cfg.Adapters.Shelly = config.ShellyAdapter{Enabled: true, Endpoints: []config.ShellyEndpoint{{
		ID: "11111111-1111-4111-8111-111111111111", EquipmentID: "22222222-2222-4222-8222-222222222222", AlarmRuleID: "33333333-3333-4333-8333-333333333333", BaseURL: "http://shelly.local", Channel: 0, PollInterval: time.Second, RequestTimeout: 100 * time.Millisecond, Retries: 1, PowerReturnPolicy: "off", EquipmentKind: "outlet",
	}}}
	container, err := New(cfg, "test.yaml", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if container.Shelly == nil || container.Bench == nil || container.OutputRouter == nil {
		t.Fatal("bench adapter graph was not composed")
	}
	registeredDevices, listErr := container.Devices.List(context.Background())
	if listErr != nil || len(registeredDevices) != 1 {
		t.Fatalf("configured adapter devices were not registered: %v %+v", listErr, registeredDevices)
	}
	registeredEquipment, listErr := container.Equipment.List(context.Background())
	if listErr != nil || len(registeredEquipment) != 1 {
		t.Fatalf("configured adapter equipment was not registered: %v %+v", listErr, registeredEquipment)
	}
	if container.MQTT != nil {
		t.Fatal("bench adapter unexpectedly depends on MQTT")
	}
}

func TestNewRejectsMissingLogger(t *testing.T) {
	cfg := config.Defaults()
	if _, err := New(cfg, "test.yaml", nil); err == nil {
		t.Fatal("New() expected error")
	}
}

func TestNewSupportsBrokerFreeSimulator(t *testing.T) {
	cfg := config.Defaults()
	cfg.HTTP.Address = "localhost:0"
	container, err := New(cfg, "test.yaml", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if container.MQTT != nil {
		t.Fatal("broker-free composition unexpectedly created MQTT client")
	}
	if container.Simulator == nil {
		t.Fatal("simulator was not composed")
	}
}
