// Package events defines AquaOS's transport-neutral, typed event contract.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

// Type is a stable, machine-readable event code. Existing values are never repurposed.
type Type string

//nolint:revive // Event codes are documented collectively by Type.
const (
	DeviceRegistered      Type = "device.registered"
	DeviceUpdated         Type = "device.updated"
	DeviceRemoved         Type = "device.removed"
	SensorRegistered      Type = "sensor.registered"
	SensorUpdated         Type = "sensor.updated"
	SensorRemoved         Type = "sensor.removed"
	EquipmentRegistered   Type = "equipment.registered"
	EquipmentUpdated      Type = "equipment.updated"
	EquipmentRemoved      Type = "equipment.removed"
	StateChanged          Type = "state.changed"
	AlarmRaised           Type = "alarm.raised"
	AlarmAcknowledged     Type = "alarm.acknowledged"
	AlarmCleared          Type = "alarm.cleared"
	AlarmEscalated        Type = "alarm.escalated"
	CommandValidated      Type = "command.validated"
	CommandRejected       Type = "command.rejected"
	CommandDispatched     Type = "command.dispatched"
	CommandAcknowledged   Type = "command.acknowledged"
	CommandReconciled     Type = "command.reconciled"
	CommandExpired        Type = "command.expired"
	SafetyModeChanged     Type = "safety.mode_changed"
	SafetyOverrideChanged Type = "safety.override_changed"
	SafetyWatchdogTripped Type = "safety.watchdog_tripped"
	ConfigurationChanged  Type = "configuration.changed"
)

// Severity describes operational importance in increasing order.
type Severity string

//nolint:revive // Severity values are documented collectively by Severity.
const (
	SeverityDebug    Severity = "debug"
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Rank returns the ordering used for deterministic severity escalation.
func (s Severity) Rank() int {
	switch s {
	case SeverityDebug:
		return 1
	case SeverityInfo:
		return 2
	case SeverityWarning:
		return 3
	case SeverityCritical:
		return 4
	default:
		return 0
	}
}

// Event is an immutable envelope. Payload remains JSON so this contract can cross MQTT later.
type Event struct {
	Timestamp     time.Time            `json:"timestamp"`
	Source        string               `json:"source"`
	EventType     Type                 `json:"eventType"`
	Severity      Severity             `json:"severity"`
	Payload       json.RawMessage      `json:"payload"`
	CorrelationID domain.CorrelationID `json:"correlationId"`
}

// Clock supplies time without coupling tests or policy engines to wall-clock time.
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Factory creates validated events with injected time and ID generation.
type Factory struct {
	clock            Clock
	newCorrelationID func() (domain.CorrelationID, error)
}

// NewFactory constructs an event factory. Nil dependencies are rejected.
func NewFactory(clock Clock, newID func() (domain.CorrelationID, error)) (*Factory, error) {
	if clock == nil || newID == nil {
		return nil, errors.New("event clock and correlation ID generator are required")
	}
	return &Factory{clock: clock, newCorrelationID: newID}, nil
}

// New constructs an event using production-safe dependencies. Services needing deterministic
// time should inject and retain a Factory instead.
func New(source string, eventType Type, severity Severity, payload any, correlationID domain.CorrelationID) (Event, error) {
	factory, _ := NewFactory(systemClock{}, domain.NewCorrelationID)
	return factory.New(source, eventType, severity, payload, correlationID)
}

// New constructs and validates an event.
func (f *Factory) New(source string, eventType Type, severity Severity, payload any, correlationID domain.CorrelationID) (Event, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("encode event payload: %w", err)
	}
	if correlationID == "" {
		correlationID, err = f.newCorrelationID()
		if err != nil {
			return Event{}, fmt.Errorf("create correlation ID: %w", err)
		}
	}
	event := Event{Timestamp: f.clock.Now().UTC(), Source: source, EventType: eventType, Severity: severity, Payload: encoded, CorrelationID: correlationID}
	return event, event.Validate()
}

// Validate checks required envelope fields, known severity, identity, and payload encoding.
func (e Event) Validate() error {
	if e.Timestamp.IsZero() || e.Source == "" || e.EventType == "" {
		return errors.New("event timestamp, source, and type are required")
	}
	if e.Severity.Rank() == 0 {
		return errors.New("event severity is invalid")
	}
	if err := e.CorrelationID.Validate(); err != nil {
		return fmt.Errorf("invalid correlation ID: %w", err)
	}
	if !json.Valid(e.Payload) {
		return errors.New("event payload must be valid JSON")
	}
	return nil
}

// Handler consumes one immutable delivery and reports processing failure.
type Handler func(context.Context, Event) error

// Publisher emits events without exposing a concrete transport.
type Publisher interface {
	Publish(context.Context, Event) error
}

// Subscription controls a handler registration.
type Subscription interface{ Unsubscribe() }

// Subscriber registers handlers for one stable event type.
type Subscriber interface {
	Subscribe(Type, Handler) (Subscription, error)
}

type correlationKey struct{}

// WithCorrelationID attaches a validated causal identity to a context.
func WithCorrelationID(ctx context.Context, id domain.CorrelationID) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

// CorrelationIDFromContext returns the attached identity, if present.
func CorrelationIDFromContext(ctx context.Context) domain.CorrelationID {
	id, _ := ctx.Value(correlationKey{}).(domain.CorrelationID)
	return id
}
