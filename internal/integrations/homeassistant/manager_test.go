package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/tylerkirby004-droid/aquaos/internal/alarms"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
)

const (
	testDeviceID    = domain.DeviceID("11111111-1111-4111-8111-111111111111")
	testSensorID    = domain.SensorID("22222222-2222-4222-8222-222222222222")
	testEquipmentID = domain.EquipmentID("33333333-3333-4333-8333-333333333333")
)

type publication struct {
	topic    string
	qos      byte
	retained bool
	payload  []byte
}
type fakeTransport struct {
	mu           sync.Mutex
	publications []publication
	subscribed   string
	publishErr   error
}

func (f *fakeTransport) Publish(_ context.Context, topic string, qos byte, retained bool, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.publishErr != nil {
		return f.publishErr
	}
	f.publications = append(f.publications, publication{topic, qos, retained, append([]byte(nil), payload...)})
	return nil
}
func (f *fakeTransport) Subscribe(_ context.Context, topic string, _ byte, _ paho.MessageHandler) error {
	f.subscribed = topic
	return nil
}

type fakeInventory struct {
	mu        sync.RWMutex
	devices   []domain.Device
	sensors   []domain.Sensor
	equipment []domain.Equipment
}

func (f *fakeInventory) Devices(context.Context) ([]domain.Device, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return append([]domain.Device(nil), f.devices...), nil
}
func (f *fakeInventory) Sensors(context.Context) ([]domain.Sensor, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return append([]domain.Sensor(nil), f.sensors...), nil
}
func (f *fakeInventory) Equipment(context.Context) ([]domain.Equipment, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return append([]domain.Equipment(nil), f.equipment...), nil
}

type fakeAlarmReader struct{}

func (fakeAlarmReader) List(context.Context, alarms.Status) ([]alarms.Alarm, error) { return nil, nil }

type fakeCommander struct{ commands []output.Command }

func (f *fakeCommander) Submit(_ context.Context, command output.Command) (output.Result, error) {
	f.commands = append(f.commands, command)
	return output.Result{Command: command}, nil
}

func fixtureInventory() *fakeInventory {
	return &fakeInventory{
		devices:   []domain.Device{{ID: testDeviceID, Name: "Fish room node", Capabilities: []domain.Capability{domain.CapabilityObserve, domain.CapabilitySwitch}}},
		sensors:   []domain.Sensor{{ID: testSensorID, DeviceID: testDeviceID, Name: "Water temperature", Quantity: domain.QuantityTemperature, Unit: domain.UnitCelsius, Capabilities: []domain.Capability{domain.CapabilityObserve}}},
		equipment: []domain.Equipment{{ID: testEquipmentID, DeviceID: testDeviceID, Name: "Lamp", Capabilities: []domain.Capability{domain.CapabilitySwitch}}},
	}
}
func newTestManager(t *testing.T, transport *fakeTransport, inventory *fakeInventory, commander *fakeCommander) *Manager {
	t.Helper()
	manager, err := New(Config{SiteID: "test-reef", CommandTTL: 10 * time.Second, MaximumPayload: 65536}, transport, inventory, fakeAlarmReader{}, commander, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return time.Unix(100, 0).UTC() }
	return manager
}

func TestDiscoveryFixtureIsStableGroupedAndRetained(t *testing.T) {
	transport := &fakeTransport{}
	manager := newTestManager(t, transport, fixtureInventory(), &fakeCommander{})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if transport.subscribed != "aquaos/test-reef/v1/home-assistant/+/set" {
		t.Fatalf("subscription = %q", transport.subscribed)
	}
	if len(transport.publications) != 6 {
		t.Fatalf("publication count = %d", len(transport.publications))
	}
	seen := map[string]discoveryPayload{}
	for _, item := range transport.publications {
		if !item.retained || item.qos != 1 {
			t.Fatalf("invalid policy for %s", item.topic)
		}
		var payload discoveryPayload
		if err := json.Unmarshal(item.payload, &payload); err != nil {
			t.Fatal(err)
		}
		seen[item.topic] = payload
	}
	sensor := seen["homeassistant/sensor/sensor-22222222-2222-4222-8222-222222222222/config"]
	if sensor.UniqueID != "aquaos_sensor_22222222-2222-4222-8222-222222222222" || sensor.UnitOfMeasurement != "°C" || sensor.DeviceClass != "temperature" || sensor.Device.Identifiers[0] != "aquaos-device-11111111-1111-4111-8111-111111111111" {
		t.Fatalf("sensor = %+v", sensor)
	}
	switchEntity := seen["homeassistant/switch/equipment-33333333-3333-4333-8333-333333333333/config"]
	if switchEntity.CommandTopic != "aquaos/test-reef/v1/home-assistant/equipment-33333333-3333-4333-8333-333333333333/set" || switchEntity.UniqueID == "" {
		t.Fatalf("switch = %+v", switchEntity)
	}
}

func TestRestartRepublishesIdenticalEntities(t *testing.T) {
	transport := &fakeTransport{}
	manager := newTestManager(t, transport, fixtureInventory(), &fakeCommander{})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := append([]publication(nil), transport.publications...)
	_ = manager.Stop(context.Background())
	transport.publications = nil
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(first) != len(transport.publications) {
		t.Fatal("entity count changed")
	}
	for i := range first {
		if first[i].topic != transport.publications[i].topic {
			t.Fatalf("entity %d changed", i)
		}
		if strings.HasPrefix(first[i].topic, "homeassistant/") && string(first[i].payload) != string(transport.publications[i].payload) {
			t.Fatalf("discovery payload %d changed", i)
		}
	}
}

func TestRefreshClearsRemovedRetainedEntity(t *testing.T) {
	inventory := fixtureInventory()
	transport := &fakeTransport{}
	manager := newTestManager(t, transport, inventory, &fakeCommander{})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	inventory.mu.Lock()
	inventory.equipment = nil
	inventory.mu.Unlock()
	transport.publications = nil
	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, item := range transport.publications {
		if item.topic == "homeassistant/switch/equipment-33333333-3333-4333-8333-333333333333/config" && item.retained && len(item.payload) == 0 {
			return
		}
	}
	t.Fatal("removed entity was not cleared")
}

func TestCommandBridgeUsesAuthoritativeService(t *testing.T) {
	commander := &fakeCommander{}
	manager := newTestManager(t, &fakeTransport{}, fixtureInventory(), commander)
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	topic := "aquaos/test-reef/v1/home-assistant/equipment-33333333-3333-4333-8333-333333333333/set"
	if err := manager.HandleCommand(context.Background(), topic, []byte("ON"), 42); err != nil {
		t.Fatal(err)
	}
	if len(commander.commands) != 1 || commander.commands[0].EquipmentID != testEquipmentID || !commander.commands[0].On || commander.commands[0].Requester != "home-assistant" {
		t.Fatalf("commands = %+v", commander.commands)
	}
	if err := manager.HandleCommand(context.Background(), topic, []byte("TOGGLE"), 43); err == nil {
		t.Fatal("invalid payload accepted")
	}
}

func TestHomeAssistantOutageDoesNotFailStart(t *testing.T) {
	manager := newTestManager(t, &fakeTransport{publishErr: errors.New("broker unavailable")}, fixtureInventory(), &fakeCommander{})
	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("optional outage failed start: %v", err)
	}
	if manager.Health().State == health.StateHealthy {
		t.Fatal("outage hidden from health")
	}
}
