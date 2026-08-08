// Package bench contains Prompt 8 application coordination for direct adapters.
// It translates adapter observations into canonical state and alarm/safe-command
// requests without putting domain policy in transport handlers.
package bench

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/adapters/esp32"
	"github.com/tylerkirby004-droid/aquaos/internal/adapters/shelly"
	"github.com/tylerkirby004-droid/aquaos/internal/alarms"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

// StateWriter is the canonical-state boundary used by adapter coordination.
type StateWriter interface {
	Set(context.Context, state.Value) (state.Value, error)
}

// AlarmEngine is the minimal alarm boundary owned by this application service.
type AlarmEngine interface {
	RegisterRule(context.Context, alarms.Rule) error
	Observe(context.Context, alarms.Observation) (alarms.Alarm, alarms.Transition, error)
}

// Commander submits safe-response requests through the authoritative output
// service and therefore through existing safety validation.
type Commander interface {
	Submit(context.Context, output.Command) (output.Result, error)
}

// ShellyPolicy binds adapter availability to a stable alarm rule.
type ShellyPolicy struct {
	Endpoint shelly.Endpoint
	RuleID   domain.RuleID
}

// ESP32Policy binds node/probe availability to a stable alarm rule.
type ESP32Policy struct {
	Endpoint esp32.Endpoint
	RuleID   domain.RuleID
}

// Coordinator is a synchronous application service. It starts no goroutines.
type Coordinator struct {
	mu              sync.Mutex
	state           StateWriter
	alarms          AlarmEngine
	commands        Commander
	logger          *slog.Logger
	shellyRules     map[domain.EndpointID]domain.RuleID
	espRules        map[domain.EndpointID]domain.RuleID
	shellyActive    map[domain.EndpointID]bool
	shellyEndpoints map[domain.EndpointID]shelly.Endpoint
	espEndpoints    map[domain.EndpointID]esp32.Endpoint
}

// NewCoordinator validates dependencies and external policies.
func NewCoordinator(stateWriter StateWriter, alarmEngine AlarmEngine, commander Commander, logger *slog.Logger, shellyPolicies []ShellyPolicy, espPolicies []ESP32Policy) (*Coordinator, error) {
	if stateWriter == nil || alarmEngine == nil || commander == nil || logger == nil {
		return nil, errors.New("all bench coordinator dependencies are required")
	}
	coordinator := &Coordinator{state: stateWriter, alarms: alarmEngine, commands: commander, logger: logger, shellyRules: make(map[domain.EndpointID]domain.RuleID), espRules: make(map[domain.EndpointID]domain.RuleID), shellyActive: make(map[domain.EndpointID]bool), shellyEndpoints: make(map[domain.EndpointID]shelly.Endpoint), espEndpoints: make(map[domain.EndpointID]esp32.Endpoint)}
	for _, policy := range shellyPolicies {
		if err := policy.RuleID.Validate(); err != nil {
			return nil, fmt.Errorf("shelly alarm rule ID: %w", err)
		}
		if _, exists := coordinator.shellyRules[policy.Endpoint.ID]; exists {
			return nil, errors.New("duplicate Shelly bench policy")
		}
		coordinator.shellyRules[policy.Endpoint.ID], coordinator.shellyEndpoints[policy.Endpoint.ID] = policy.RuleID, policy.Endpoint
	}
	for _, policy := range espPolicies {
		if err := policy.RuleID.Validate(); err != nil {
			return nil, fmt.Errorf("esp32 alarm rule ID: %w", err)
		}
		if _, exists := coordinator.espRules[policy.Endpoint.ID]; exists {
			return nil, errors.New("duplicate ESP32 bench policy")
		}
		coordinator.espRules[policy.Endpoint.ID], coordinator.espEndpoints[policy.Endpoint.ID] = policy.RuleID, policy.Endpoint
	}
	return coordinator, nil
}

// RegisterRules installs stable, latching availability rules. Acknowledgement
// cannot clear their underlying active condition.
func (c *Coordinator) RegisterRules(ctx context.Context) error {
	for id, ruleID := range c.shellyRules {
		endpoint := c.shellyEndpoints[id]
		rule := alarms.Rule{ID: ruleID, Code: "shelly.protection_or_connection_fault", Name: "Shelly protection or connection fault", Subject: alarms.Subject{Kind: "equipment", ID: domain.EntityID(endpoint.EquipmentID)}, Severity: events.SeverityCritical, Latching: true}
		if err := c.alarms.RegisterRule(ctx, rule); err != nil {
			return fmt.Errorf("register Shelly alarm: %w", err)
		}
	}
	for id, ruleID := range c.espRules {
		endpoint := c.espEndpoints[id]
		rule := alarms.Rule{ID: ruleID, Code: "esp32.node_unhealthy", Name: "ESP32 sensor node unhealthy", Subject: alarms.Subject{Kind: "device", ID: domain.EntityID(endpoint.DeviceID)}, Severity: events.SeverityCritical, Latching: true}
		if err := c.alarms.RegisterRule(ctx, rule); err != nil {
			return fmt.Errorf("register ESP32 alarm: %w", err)
		}
	}
	return nil
}

// ReportShelly records direct reported state and bounded electrical telemetry.
func (c *Coordinator) ReportShelly(ctx context.Context, report shelly.Report) error {
	endpoint, exists := c.shellyEndpoints[report.EndpointID]
	if !exists || endpoint.EquipmentID != report.EquipmentID {
		return errors.New("unknown Shelly report endpoint")
	}
	freshFor := endpoint.PollInterval * 2
	values := []struct {
		attribute string
		value     domain.Value
	}{
		{"on", domain.NewBooleanValue(report.On)},
	}
	for _, item := range []struct {
		attribute string
		kind      domain.QuantityKind
		unit      domain.Unit
		value     float64
	}{{"power", domain.QuantityPower, domain.UnitWatts, report.APower}, {"voltage", domain.QuantityVoltage, domain.UnitVolts, report.Voltage}, {"current", domain.QuantityCurrent, domain.UnitAmperes, report.Current}} {
		quantity, err := domain.NewQuantity(item.kind, item.value, item.unit)
		if err != nil {
			return err
		}
		values = append(values, struct {
			attribute string
			value     domain.Value
		}{item.attribute, domain.NewQuantityValue(quantity)})
	}
	for _, item := range values {
		_, err := c.state.Set(ctx, state.Value{Key: state.Key{EntityKind: state.EntityEquipment, EntityID: domain.EntityID(report.EquipmentID), Plane: state.PlaneReported, Attribute: item.attribute}, Value: item.value, Quality: domain.QualityGood, ObservedAt: report.ObservedAt, ReceivedAt: report.ObservedAt, FreshFor: freshFor, Source: report.EndpointID})
		if err != nil {
			return err
		}
	}
	if len(report.Errors) > 0 {
		return c.ShellyFailure(ctx, endpoint, true, "shelly.protection."+strings.Join(report.Errors, "+"), report.ObservedAt)
	}
	desired := endpoint.SafeOn
	if report.DesiredOn != nil {
		desired = *report.DesiredOn
	}
	if !report.Pending && report.On != desired {
		return c.ShellyFailure(ctx, endpoint, true, "shelly.reported_divergence", report.ObservedAt)
	}
	if report.On == desired {
		return c.ShellyFailure(ctx, endpoint, false, "shelly.reported_converged", report.ObservedAt)
	}
	return nil
}

// ReportESP32 records one validated generic probe measurement.
func (c *Coordinator) ReportESP32(ctx context.Context, endpointID domain.EndpointID, measurement domain.Measurement) error {
	if _, exists := c.espEndpoints[endpointID]; !exists {
		return errors.New("unknown ESP32 report endpoint")
	}
	_, err := c.state.Set(ctx, state.Value{Key: state.Key{EntityKind: state.EntitySensor, EntityID: domain.EntityID(measurement.SensorID), Plane: state.PlaneObservation, Attribute: "measurement"}, Value: domain.NewQuantityValue(measurement.Value), Quality: measurement.Quality, ObservedAt: measurement.ObservedAt, ReceivedAt: measurement.ReceivedAt, FreshFor: measurement.FreshFor, Source: endpointID})
	return err
}

// ShellyFailure observes availability and requests the configured safe state on
// the first transition into failure. Repeated polls do not flood commands.
func (c *Coordinator) ShellyFailure(ctx context.Context, endpoint shelly.Endpoint, active bool, reason string, observedAt time.Time) error {
	ruleID, exists := c.shellyRules[endpoint.ID]
	if !exists {
		return errors.New("unknown Shelly failure endpoint")
	}
	observation := alarms.Observation{RuleID: ruleID, Active: active, ObservedAt: observedAt, Evidence: alarms.Evidence{Code: reason, Message: "Shelly reported a protection or connection fault"}}
	_, _, alarmErr := c.alarms.Observe(ctx, observation)
	c.mu.Lock()
	wasActive := c.shellyActive[endpoint.ID]
	c.shellyActive[endpoint.ID] = active
	c.mu.Unlock()
	if !active || wasActive {
		return alarmErr
	}
	command := output.Command{IdempotencyKey: fmt.Sprintf("adapter-failure/%s/%d", endpoint.ID, observedAt.UnixNano()), EquipmentID: endpoint.EquipmentID, Requester: "adapter-failure-policy", IssuedAt: observedAt, ExpiresAt: observedAt.Add(2 * endpoint.RequestTimeout), On: endpoint.SafeOn}
	result, err := c.commands.Submit(ctx, command)
	c.logger.WarnContext(ctx, "Shelly safe response requested", "code", reason, "endpoint_id", endpoint.ID, "equipment_id", endpoint.EquipmentID, "status", result.Status, "error", err)
	return errors.Join(alarmErr, err)
}

// ESP32Failure observes node/probe health. Existing safety policy blocks
// hazardous commands when the last canonical measurements become stale.
func (c *Coordinator) ESP32Failure(ctx context.Context, endpoint esp32.Endpoint, active bool, reason string, observedAt time.Time) error {
	ruleID, exists := c.espRules[endpoint.ID]
	if !exists {
		return errors.New("unknown ESP32 failure endpoint")
	}
	var stateErrors []error
	if active {
		quantity, quantityErr := domain.NewQuantity(domain.QuantityTemperature, 0, domain.UnitCelsius)
		if quantityErr != nil {
			stateErrors = append(stateErrors, quantityErr)
		} else {
			for _, probeID := range endpoint.ProbeIDs {
				_, setErr := c.state.Set(ctx, state.Value{Key: state.Key{EntityKind: state.EntitySensor, EntityID: domain.EntityID(probeID), Plane: state.PlaneObservation, Attribute: "measurement"}, Value: domain.NewQuantityValue(quantity), Quality: domain.QualityUnavailable, ObservedAt: observedAt, ReceivedAt: observedAt, FreshFor: endpoint.FreshFor, Source: endpoint.ID})
				stateErrors = append(stateErrors, setErr)
			}
		}
	}
	_, _, alarmErr := c.alarms.Observe(ctx, alarms.Observation{RuleID: ruleID, Active: active, ObservedAt: observedAt, Evidence: alarms.Evidence{Code: reason, Message: "ESP32 node or probe condition"}})
	stateErrors = append(stateErrors, alarmErr)
	return errors.Join(stateErrors...)
}
