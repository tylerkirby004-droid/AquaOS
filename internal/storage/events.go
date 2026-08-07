package storage

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"

	"github.com/tylerkirby004-droid/aquaos/internal/alarms"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

// EventSubscriber is the consumer-owned event registration boundary.
type EventSubscriber interface {
	Subscribe(events.Type, events.Handler) (events.Subscription, error)
}

// EventSink converts stable domain events into versioned storage points.
type EventSink struct {
	storage Storage
	logger  *slog.Logger
}

// NewEventSink constructs a non-blocking historical event consumer.
func NewEventSink(target Storage, logger *slog.Logger) (*EventSink, error) {
	if target == nil || logger == nil {
		return nil, errors.New("event storage and logger are required")
	}
	return &EventSink{storage: target, logger: logger}, nil
}

// Attach registers the explicit event set persisted by this schema version.
func (s *EventSink) Attach(subscriber EventSubscriber) ([]events.Subscription, error) {
	types := []events.Type{events.StateChanged, events.CommandValidated, events.CommandRejected, events.CommandDispatched, events.CommandAcknowledged, events.CommandReconciled, events.CommandExpired, events.AlarmRaised, events.AlarmAcknowledged, events.AlarmCleared, events.AlarmEscalated}
	subscriptions := make([]events.Subscription, 0, len(types))
	for _, eventType := range types {
		subscription, err := subscriber.Subscribe(eventType, s.Handle)
		if err != nil {
			for _, item := range subscriptions {
				item.Unsubscribe()
			}
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions, nil
}

// Handle performs bounded conversion and never propagates optional-storage failure.
func (s *EventSink) Handle(ctx context.Context, event events.Event) error {
	point, ok := pointFromEvent(event)
	if !ok {
		return nil
	}
	if err := s.storage.Enqueue(point); err != nil {
		s.logger.WarnContext(ctx, "optional storage point dropped", "event_type", event.EventType, "error", err)
	}
	return nil
}

func pointFromEvent(event events.Event) (Point, bool) {
	base := Point{Timestamp: event.Timestamp, Tags: map[string]string{"event_type": string(event.EventType), "severity": string(event.Severity), "source": event.Source}, Fields: map[string]Field{"correlation_id": StringField(string(event.CorrelationID))}}
	switch event.EventType {
	case events.StateChanged:
		var value state.Value
		if json.Unmarshal(event.Payload, &value) != nil {
			return Point{}, false
		}
		base.Tags["entity_kind"] = string(value.Key.EntityKind)
		base.Tags["entity_id"] = string(value.Key.EntityID)
		base.Tags["attribute"] = value.Key.Attribute
		base.Fields["quality"] = StringField(string(value.Quality))
		base.Fields["revision"] = StringField(strconv.FormatUint(uint64(value.Revision), 10))
		if value.Value.Quantity != nil {
			base.Measurement = "aquaos_measurements_v1"
			base.Tags["quantity"] = string(value.Value.Quantity.Kind)
			base.Tags["unit"] = string(value.Value.Quantity.Unit)
			base.Fields["value"] = FloatField(value.Value.Quantity.Value)
		} else {
			base.Measurement = "aquaos_equipment_state_v1"
			if value.Value.Boolean != nil {
				base.Fields["value"] = BooleanField(*value.Value.Boolean)
			} else if value.Value.Text != nil {
				base.Fields["value"] = StringField(*value.Value.Text)
			}
		}
	case events.CommandValidated, events.CommandRejected, events.CommandDispatched, events.CommandAcknowledged, events.CommandReconciled, events.CommandExpired:
		var result output.Result
		if json.Unmarshal(event.Payload, &result) != nil {
			return Point{}, false
		}
		base.Measurement = "aquaos_command_outcomes_v1"
		base.Tags["equipment_id"] = string(result.Command.EquipmentID)
		base.Tags["status"] = string(result.Status)
		base.Tags["reason"] = result.Reason
		base.Fields["command_id"] = StringField(string(result.Command.ID))
		base.Fields["on"] = BooleanField(result.Command.On)
	case events.AlarmRaised, events.AlarmAcknowledged, events.AlarmCleared, events.AlarmEscalated:
		var alarm alarms.Alarm
		if json.Unmarshal(event.Payload, &alarm) != nil {
			return Point{}, false
		}
		base.Measurement = "aquaos_alarms_v1"
		base.Tags["alarm_code"] = alarm.Code
		base.Tags["status"] = string(alarm.Status)
		base.Tags["subject_kind"] = alarm.Subject.Kind
		base.Fields["alarm_id"] = StringField(string(alarm.ID))
		base.Fields["subject_id"] = StringField(string(alarm.Subject.ID))
		base.Fields["condition_active"] = BooleanField(alarm.ConditionActive)
	default:
		return Point{}, false
	}
	return base, true
}
