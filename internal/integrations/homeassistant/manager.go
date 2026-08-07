// Package homeassistant publishes optional MQTT Discovery metadata and adapts
// Home Assistant switch requests into the authoritative AquaOS command path.
package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/tylerkirby004-droid/aquaos/internal/alarms"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
	"github.com/tylerkirby004-droid/aquaos/internal/mqtt"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
)

// Transport is the consumer-owned MQTT boundary used by discovery.
type Transport interface {
	Publish(context.Context, string, byte, bool, []byte) error
	Subscribe(context.Context, string, byte, paho.MessageHandler) error
}

// Inventory supplies stable device, sensor, and equipment identities.
type Inventory interface {
	Devices(context.Context) ([]domain.Device, error)
	Sensors(context.Context) ([]domain.Sensor, error)
	Equipment(context.Context) ([]domain.Equipment, error)
}

// AlarmReader supplies current alarm identities for diagnostic entities.
type AlarmReader interface {
	List(context.Context, alarms.Status) ([]alarms.Alarm, error)
}

// Commander is the authoritative equipment command application service.
type Commander interface {
	Submit(context.Context, output.Command) (output.Result, error)
}

// Tombstone explicitly removes a formerly published retained discovery entity.
type Tombstone struct {
	Component string `json:"component"`
	ObjectID  string `json:"objectId"`
}

// Config contains externally selected Home Assistant behavior.
type Config struct {
	SiteID              string
	CommandTTL          time.Duration
	MaximumPayload      int
	Tombstones          []Tombstone
	NotificationRuleIDs []domain.RuleID
}

type entity struct {
	Component string
	ObjectID  string
	Payload   []byte
}

// Manager owns Home Assistant discovery publication and command translation.
type Manager struct {
	transport Transport
	inventory Inventory
	alarms    AlarmReader
	commands  Commander
	registry  *mqtt.Registry
	codec     *mqtt.Codec
	logger    *slog.Logger
	cfg       Config
	now       func() time.Time
	mu        sync.RWMutex
	cancel    context.CancelFunc
	running   bool
	lastErr   error
	published map[string]struct{}
	allowed   map[string]domain.EquipmentID
}

// New constructs a Home Assistant integration with explicit optional boundaries.
func New(cfg Config, transport Transport, inventory Inventory, alarms AlarmReader, commands Commander, logger *slog.Logger) (*Manager, error) {
	if transport == nil || inventory == nil || commands == nil || logger == nil {
		return nil, errors.New("home assistant dependencies are required")
	}
	if cfg.CommandTTL <= 0 || cfg.CommandTTL > time.Minute {
		return nil, errors.New("home assistant command TTL must be positive and at most one minute")
	}
	registry, err := mqtt.NewRegistry(cfg.SiteID)
	if err != nil {
		return nil, err
	}
	codec, err := mqtt.NewCodec(cfg.SiteID, cfg.MaximumPayload, time.Now)
	if err != nil {
		return nil, err
	}
	return &Manager{transport: transport, inventory: inventory, alarms: alarms, commands: commands, registry: registry, codec: codec, logger: logger, cfg: cfg, now: time.Now, published: make(map[string]struct{}), allowed: make(map[string]domain.EquipmentID)}, nil
}

// Name returns the lifecycle component name.
func (*Manager) Name() string { return "home-assistant" }

// Start publishes deterministic retained discovery and installs one narrow command subscription.
func (m *Manager) Start(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancel = cancel
	m.running = true
	m.mu.Unlock()
	filter, qos, err := m.registry.SubscriptionFilter(mqtt.PurposeHACommand)
	if err != nil {
		return err
	}
	if err = m.transport.Subscribe(runCtx, filter, qos, m.receive); err != nil {
		m.setError(err)
		return nil
	}
	if err = m.Refresh(runCtx); err != nil {
		m.setError(err)
		m.logger.WarnContext(ctx, "Home Assistant discovery unavailable", "error", err)
	}
	return nil
}

// Stop cancels future command handling without deleting retained discovery.
func (m *Manager) Stop(context.Context) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.cancel = nil
	m.running = false
	m.mu.Unlock()
	return nil
}

// Health reports optional integration publication/subscription state.
func (m *Manager) Health() health.Status {
	m.mu.RLock()
	running, lastErr := m.running, m.lastErr
	m.mu.RUnlock()
	state := health.StateUnhealthy
	message := ""
	if running && lastErr == nil {
		state = health.StateHealthy
	}
	if lastErr != nil {
		message = lastErr.Error()
	}
	return health.NewStatus(m.Name(), state, message, m.now().UTC())
}

// Refresh republishes the current stable set and clears removed retained entities.
func (m *Manager) Refresh(ctx context.Context) error {
	entities, allowed, err := m.entities(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]struct{}, len(entities))
	for _, item := range entities {
		topic, policy, topicErr := m.registry.HADiscovery(item.Component, item.ObjectID)
		if topicErr != nil {
			return topicErr
		}
		if publishErr := m.transport.Publish(ctx, topic, policy.QoS, policy.Retained, item.Payload); publishErr != nil {
			return fmt.Errorf("publish Home Assistant discovery: %w", publishErr)
		}
		next[item.Component+"/"+item.ObjectID] = struct{}{}
	}
	m.mu.Lock()
	previous := m.published
	m.published = next
	m.allowed = allowed
	m.lastErr = nil
	m.mu.Unlock()
	for key := range previous {
		if _, ok := next[key]; ok {
			continue
		}
		parts := strings.SplitN(key, "/", 2)
		if err := m.clear(ctx, parts[0], parts[1]); err != nil {
			return err
		}
	}
	for _, item := range m.cfg.Tombstones {
		if _, active := next[item.Component+"/"+item.ObjectID]; active {
			return errors.New("home assistant tombstone conflicts with active entity")
		}
		if err := m.clear(ctx, item.Component, item.ObjectID); err != nil {
			return err
		}
	}
	if err := m.publishStatus(ctx); err != nil {
		return err
	}
	return nil
}

func (m *Manager) publishStatus(ctx context.Context) error {
	activeCount := 0
	notificationCount := 0
	if m.alarms != nil {
		active, err := m.alarms.List(ctx, alarms.StatusActive)
		if err != nil {
			return err
		}
		activeCount = len(active)
		notificationRules := make(map[domain.RuleID]struct{}, len(m.cfg.NotificationRuleIDs))
		for _, ruleID := range m.cfg.NotificationRuleIDs {
			notificationRules[ruleID] = struct{}{}
		}
		for _, alarm := range active {
			if _, enabled := notificationRules[alarm.RuleID]; enabled {
				notificationCount++
			}
		}
	}
	correlationID, err := domain.NewCorrelationID()
	if err != nil {
		return err
	}
	payload, err := m.codec.Encode("ha-status", "home-assistant", correlationID, m.now().UTC(), nil, nil, map[string]any{"status": "online", "activeAlarmCount": activeCount, "notificationAlarmCount": notificationCount})
	if err != nil {
		return err
	}
	topic, policy := m.registry.HAStatus()
	return m.transport.Publish(ctx, topic, policy.QoS, policy.Retained, payload)
}

// HandleAlarmEvent refreshes the optional diagnostic status without allowing
// Home Assistant or broker failure to fail the authoritative alarm transition.
func (m *Manager) HandleAlarmEvent(ctx context.Context, event events.Event) error {
	if err := m.publishStatus(ctx); err != nil {
		m.setError(err)
		m.logger.WarnContext(ctx, "Home Assistant alarm status unavailable", "event_type", event.EventType, "error", err)
	}
	return nil
}

func (m *Manager) clear(ctx context.Context, component, objectID string) error {
	topic, policy, err := m.registry.HADiscovery(component, objectID)
	if err != nil {
		return err
	}
	return m.transport.Publish(ctx, topic, policy.QoS, policy.Retained, []byte{})
}

func (m *Manager) entities(ctx context.Context) ([]entity, map[string]domain.EquipmentID, error) {
	devices, err := m.inventory.Devices(ctx)
	if err != nil {
		return nil, nil, err
	}
	sensors, err := m.inventory.Sensors(ctx)
	if err != nil {
		return nil, nil, err
	}
	equipment, err := m.inventory.Equipment(ctx)
	if err != nil {
		return nil, nil, err
	}
	names := make(map[domain.DeviceID]string, len(devices))
	for _, device := range devices {
		names[device.ID] = device.Name
	}
	availability, _, err := m.registry.Topic(mqtt.PurposeAvailability, "core")
	if err != nil {
		return nil, nil, err
	}
	items := make([]entity, 0, len(sensors)+len(equipment)+2)
	allowed := make(map[string]domain.EquipmentID, len(equipment))
	for _, sensor := range sensors {
		resource := "sensor-" + string(sensor.ID)
		stateTopic, _, topicErr := m.registry.Topic(mqtt.PurposeSensorState, resource)
		if topicErr != nil {
			return nil, nil, topicErr
		}
		payload := discoveryPayload{Name: sensor.Name, UniqueID: "aquaos_sensor_" + string(sensor.ID), SuggestedObjectID: entityObjectID("sensor", string(sensor.ID)), StateTopic: stateTopic, AvailabilityTopic: availability, ValueTemplate: "{{ value_json.data.value.quantity.value }}", UnitOfMeasurement: haUnit(sensor.Unit), DeviceClass: deviceClass(sensor.Quantity), StateClass: "measurement", Device: deviceInfo(sensor.DeviceID, names[sensor.DeviceID])}
		encoded, _ := json.Marshal(payload)
		items = append(items, entity{Component: "sensor", ObjectID: resource, Payload: encoded})
	}
	for _, item := range equipment {
		resource := "equipment-" + string(item.ID)
		stateTopic, _, topicErr := m.registry.Topic(mqtt.PurposeEquipmentReported, resource)
		if topicErr != nil {
			return nil, nil, topicErr
		}
		commandTopic, _, topicErr := m.registry.HACommand(resource)
		if topicErr != nil {
			return nil, nil, topicErr
		}
		payload := discoveryPayload{Name: item.Name, UniqueID: "aquaos_equipment_" + string(item.ID), SuggestedObjectID: entityObjectID("equipment", string(item.ID)), StateTopic: stateTopic, CommandTopic: commandTopic, AvailabilityTopic: availability, ValueTemplate: "{{ 'ON' if value_json.data.value.boolean else 'OFF' }}", PayloadOn: "ON", PayloadOff: "OFF", Device: deviceInfo(item.DeviceID, names[item.DeviceID])}
		encoded, _ := json.Marshal(payload)
		items = append(items, entity{Component: "switch", ObjectID: resource, Payload: encoded})
		allowed[resource] = item.ID
	}
	statusTopic, _ := m.registry.HAStatus()
	status, _ := json.Marshal(discoveryPayload{Name: "AquaOS Core", UniqueID: "aquaos_" + m.cfg.SiteID + "_core", SuggestedObjectID: entityObjectID(m.cfg.SiteID, "core"), StateTopic: statusTopic, AvailabilityTopic: availability, ValueTemplate: "{{ value_json.data.status }}", Device: deviceInfo("", "AquaOS Core")})
	items = append(items, entity{Component: "sensor", ObjectID: "aquaos-" + m.cfg.SiteID + "-core", Payload: status})
	alarmPayload, _ := json.Marshal(discoveryPayload{Name: "Active alarm", UniqueID: "aquaos_" + m.cfg.SiteID + "_alarm", SuggestedObjectID: entityObjectID(m.cfg.SiteID, "alarm"), StateTopic: statusTopic, AvailabilityTopic: availability, ValueTemplate: "{{ 'ON' if value_json.data.activeAlarmCount > 0 else 'OFF' }}", PayloadOn: "ON", PayloadOff: "OFF", DeviceClass: "problem", Device: deviceInfo("", "AquaOS Core")})
	items = append(items, entity{Component: "binary_sensor", ObjectID: "aquaos-" + m.cfg.SiteID + "-alarm", Payload: alarmPayload})
	notificationPayload, _ := json.Marshal(discoveryPayload{Name: "Notifiable alarm", UniqueID: "aquaos_" + m.cfg.SiteID + "_notification", SuggestedObjectID: entityObjectID(m.cfg.SiteID, "notification"), StateTopic: statusTopic, AvailabilityTopic: availability, ValueTemplate: "{{ 'ON' if value_json.data.notificationAlarmCount > 0 else 'OFF' }}", PayloadOn: "ON", PayloadOff: "OFF", DeviceClass: "problem", Device: deviceInfo("", "AquaOS Core")})
	items = append(items, entity{Component: "binary_sensor", ObjectID: "aquaos-" + m.cfg.SiteID + "-notification", Payload: notificationPayload})
	return items, allowed, nil
}

type discoveryPayload struct {
	Name              string   `json:"name"`
	UniqueID          string   `json:"unique_id"`
	SuggestedObjectID string   `json:"object_id"`
	StateTopic        string   `json:"state_topic"`
	CommandTopic      string   `json:"command_topic,omitempty"`
	AvailabilityTopic string   `json:"availability_topic"`
	ValueTemplate     string   `json:"value_template,omitempty"`
	PayloadOn         string   `json:"payload_on,omitempty"`
	PayloadOff        string   `json:"payload_off,omitempty"`
	UnitOfMeasurement string   `json:"unit_of_measurement,omitempty"`
	DeviceClass       string   `json:"device_class,omitempty"`
	StateClass        string   `json:"state_class,omitempty"`
	Device            haDevice `json:"device"`
}

func entityObjectID(kind, id string) string {
	value := strings.ToLower("aquaos_" + kind + "_" + id)
	return strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' {
			return character
		}
		return '_'
	}, value)
}

type haDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
}

func deviceInfo(id domain.DeviceID, name string) haDevice {
	identifier := "aquaos-core"
	if id != "" {
		identifier = "aquaos-device-" + string(id)
	}
	if name == "" {
		name = "AquaOS Device"
	}
	return haDevice{Identifiers: []string{identifier}, Name: name, Manufacturer: "AquaOS", Model: "AquaOS managed device"}
}
func haUnit(unit domain.Unit) string {
	switch unit {
	case domain.UnitCelsius:
		return "°C"
	case domain.UnitPPT:
		return "ppt"
	case domain.UnitLitersPerHour:
		return "L/h"
	case domain.UnitPercent:
		return "%"
	case domain.UnitWatts:
		return "W"
	case domain.UnitVolts:
		return "V"
	case domain.UnitAmperes:
		return "A"
	default:
		return string(unit)
	}
}
func deviceClass(kind domain.QuantityKind) string {
	switch kind {
	case domain.QuantityTemperature:
		return "temperature"
	case domain.QuantityPower:
		return "power"
	case domain.QuantityVoltage:
		return "voltage"
	case domain.QuantityCurrent:
		return "current"
	default:
		return ""
	}
}

func (m *Manager) receive(_ paho.Client, message paho.Message) {
	if message.Retained() {
		m.logger.Warn("rejected retained Home Assistant command", "topic", message.Topic())
		return
	}
	m.mu.RLock()
	cancel := m.cancel
	m.mu.RUnlock()
	if cancel == nil {
		return
	}
	ctx, timeout := context.WithTimeout(context.Background(), m.cfg.CommandTTL)
	defer timeout()
	if err := m.HandleCommand(ctx, message.Topic(), message.Payload(), message.MessageID()); err != nil {
		m.setError(err)
		m.logger.WarnContext(ctx, "Home Assistant command rejected", "error", err, "topic", message.Topic())
	}
}

// HandleCommand translates a bounded switch payload and always calls the output service.
func (m *Manager) HandleCommand(ctx context.Context, topic string, payload []byte, messageID uint16) error {
	parts := strings.Split(topic, "/")
	if len(parts) != 6 || parts[0] != "aquaos" || parts[1] != m.cfg.SiteID || parts[2] != "v1" || parts[3] != "home-assistant" || parts[5] != "set" {
		return errors.New("home assistant command topic is outside the configured namespace")
	}
	m.mu.RLock()
	equipmentID, ok := m.allowed[parts[4]]
	m.mu.RUnlock()
	if !ok {
		return errors.New("home assistant command targets unknown equipment")
	}
	raw := strings.TrimSpace(string(payload))
	on := false
	switch raw {
	case "ON":
		on = true
	case "OFF":
	default:
		return errors.New("home assistant command payload must be ON or OFF")
	}
	now := m.now().UTC()
	correlationID, err := domain.NewCorrelationID()
	if err != nil {
		return err
	}
	key := "ha/" + parts[4] + "/" + strconv.Itoa(int(messageID)) + "/" + strings.ToLower(raw)
	_, err = m.commands.Submit(ctx, output.Command{IdempotencyKey: key, CorrelationID: correlationID, EquipmentID: equipmentID, Requester: "home-assistant", IssuedAt: now, ExpiresAt: now.Add(m.cfg.CommandTTL), On: on})
	return err
}
func (m *Manager) setError(err error) { m.mu.Lock(); m.lastErr = err; m.mu.Unlock() }
