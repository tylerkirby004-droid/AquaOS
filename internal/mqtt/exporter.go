package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/alarms"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

// EventExporter mirrors canonical state and alarm transitions to the optional
// MQTT integration. Delivery failure is logged and never returned to critical
// in-process publishers.
type EventExporter struct {
	transport MQTTClient
	registry  *Registry
	codec     *Codec
	logger    *slog.Logger
}

// NewEventExporter constructs an optional transport adapter.
func NewEventExporter(siteID string, maximumPayload int, transport MQTTClient, logger *slog.Logger) (*EventExporter, error) {
	if transport == nil || logger == nil {
		return nil, errors.New("MQTT exporter dependencies are required")
	}
	registry, err := NewRegistry(siteID)
	if err != nil {
		return nil, err
	}
	codec, err := NewCodec(siteID, maximumPayload, func() time.Time { return time.Now().UTC() })
	if err != nil {
		return nil, err
	}
	return &EventExporter{transport: transport, registry: registry, codec: codec, logger: logger}, nil
}

// Attach subscribes the explicit externally published event set.
func (e *EventExporter) Attach(subscriber events.Subscriber) ([]events.Subscription, error) {
	types := []events.Type{events.StateChanged, events.AlarmRaised, events.AlarmAcknowledged, events.AlarmCleared, events.AlarmEscalated}
	result := make([]events.Subscription, 0, len(types))
	for _, eventType := range types {
		subscription, err := subscriber.Subscribe(eventType, e.Handle)
		if err != nil {
			for _, item := range result {
				item.Unsubscribe()
			}
			return nil, err
		}
		result = append(result, subscription)
	}
	return result, nil
}

// Handle encodes a retained v1 state message and isolates optional failures.
func (e *EventExporter) Handle(ctx context.Context, event events.Event) error {
	var topic string
	var policy Policy
	var data any
	var revision *domain.Revision
	switch event.EventType {
	case events.StateChanged:
		var value state.Value
		if json.Unmarshal(event.Payload, &value) != nil {
			return nil
		}
		purpose, resource := PurposeSensorState, "sensor-"+string(value.Key.EntityID)
		if value.Key.EntityKind == state.EntityEquipment {
			purpose, resource = PurposeEquipmentReported, "equipment-"+string(value.Key.EntityID)
		} else if value.Key.EntityKind != state.EntitySensor {
			return nil
		}
		topic, policy, _ = e.registry.Topic(purpose, resource)
		data, revision = value, &value.Revision
	case events.AlarmRaised, events.AlarmAcknowledged, events.AlarmCleared, events.AlarmEscalated:
		var alarm alarms.Alarm
		if json.Unmarshal(event.Payload, &alarm) != nil {
			return nil
		}
		topic, policy, _ = e.registry.Topic(PurposeAlarmState, string(alarm.ID))
		data = alarm
	default:
		return nil
	}
	payload, err := e.codec.Encode(string(event.CorrelationID), event.Source, event.CorrelationID, event.Timestamp, nil, revision, data)
	if err == nil {
		err = e.transport.Publish(ctx, topic, policy.QoS, policy.Retained, payload)
	}
	if err != nil {
		e.logger.WarnContext(ctx, "optional MQTT export failed", "event_type", event.EventType, "error", err)
	}
	return nil
}
