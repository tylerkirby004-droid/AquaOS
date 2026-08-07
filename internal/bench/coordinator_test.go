package bench

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/adapters/esp32"
	"github.com/tylerkirby004-droid/aquaos/internal/adapters/shelly"
	"github.com/tylerkirby004-droid/aquaos/internal/alarms"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

const (
	benchShellyEndpoint = domain.EndpointID("11111111-1111-4111-8111-111111111111")
	benchEquipment      = domain.EquipmentID("22222222-2222-4222-8222-222222222222")
	benchESP32Endpoint  = domain.EndpointID("33333333-3333-4333-8333-333333333333")
	benchDevice         = domain.DeviceID("44444444-4444-4444-8444-444444444444")
	benchProbeA         = domain.SensorID("55555555-5555-4555-8555-555555555555")
	benchProbeB         = domain.SensorID("66666666-6666-4666-8666-666666666666")
	benchShellyRule     = domain.RuleID("77777777-7777-4777-8777-777777777777")
	benchESPRule        = domain.RuleID("88888888-8888-4888-8888-888888888888")
)

type stateCollector struct{ values []state.Value }

func (s *stateCollector) Set(_ context.Context, value state.Value) (state.Value, error) {
	s.values = append(s.values, value)
	return value, nil
}

type alarmCollector struct {
	rules        []alarms.Rule
	observations []alarms.Observation
	observeErr   error
}

func (a *alarmCollector) RegisterRule(_ context.Context, rule alarms.Rule) error {
	a.rules = append(a.rules, rule)
	return nil
}
func (a *alarmCollector) Observe(_ context.Context, observation alarms.Observation) (alarms.Alarm, alarms.Transition, error) {
	a.observations = append(a.observations, observation)
	return alarms.Alarm{}, alarms.TransitionRaised, a.observeErr
}

func TestAlarmPublicationFailureDoesNotSuppressSafeResponse(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	states, alarmEngine, commands := &stateCollector{}, &alarmCollector{observeErr: errors.New("audit unavailable")}, &commandCollector{}
	coordinator := newCoordinator(t, states, alarmEngine, commands)
	endpoint := coordinator.shellyEndpoints[benchShellyEndpoint]
	if err := coordinator.ShellyFailure(context.Background(), endpoint, true, "shelly.unreachable", now); err == nil {
		t.Fatal("expected alarm publication error")
	}
	if len(commands.commands) != 1 || commands.commands[0].On {
		t.Fatalf("safe response was suppressed: %+v", commands.commands)
	}
}

func TestESP32FailureImmediatelyMarksBothSafetyInputsUnavailable(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	states, alarmEngine, commands := &stateCollector{}, &alarmCollector{}, &commandCollector{}
	coordinator := newCoordinator(t, states, alarmEngine, commands)
	endpoint := coordinator.espEndpoints[benchESP32Endpoint]
	if err := coordinator.ESP32Failure(context.Background(), endpoint, true, "esp32.unreachable", now); err != nil {
		t.Fatal(err)
	}
	if len(states.values) != 2 || states.values[0].Quality != domain.QualityUnavailable || states.values[1].Quality != domain.QualityUnavailable {
		t.Fatalf("unavailable probe states = %+v", states.values)
	}
}

func TestManualShellyDivergenceRequestsConfiguredSafeState(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	states, alarmEngine, commands := &stateCollector{}, &alarmCollector{}, &commandCollector{}
	coordinator := newCoordinator(t, states, alarmEngine, commands)
	desiredOn := true
	report := shelly.Report{EndpointID: benchShellyEndpoint, EquipmentID: benchEquipment, On: false, ObservedAt: now, DesiredOn: &desiredOn}
	if err := coordinator.ReportShelly(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	if len(commands.commands) != 1 || commands.commands[0].On {
		t.Fatalf("manual divergence did not request safe off: %+v", commands.commands)
	}
	if len(alarmEngine.observations) != 1 || alarmEngine.observations[0].Evidence.Code != "shelly.reported_divergence" {
		t.Fatalf("divergence alarm = %+v", alarmEngine.observations)
	}
}

type commandCollector struct{ commands []output.Command }

func (c *commandCollector) Submit(_ context.Context, command output.Command) (output.Result, error) {
	c.commands = append(c.commands, command)
	return output.Result{Command: command, Status: output.StatusFailed}, nil
}

func TestCoordinatorRecordsMeasurementsAndRequestsOneSafeResponse(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	states, alarmEngine, commands := &stateCollector{}, &alarmCollector{}, &commandCollector{}
	coordinator := newCoordinator(t, states, alarmEngine, commands)
	if err := coordinator.RegisterRules(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(alarmEngine.rules) != 2 {
		t.Fatalf("rules = %d", len(alarmEngine.rules))
	}
	report := shelly.Report{EndpointID: benchShellyEndpoint, EquipmentID: benchEquipment, On: true, APower: 40, Voltage: 120, Current: 0.33, ObservedAt: now}
	if err := coordinator.ReportShelly(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	if len(states.values) != 4 {
		t.Fatalf("Shelly state values = %d", len(states.values))
	}
	measurement := domain.Measurement{SensorID: benchProbeA, Value: mustTemperature(t, 25), Quality: domain.QualityGood, ObservedAt: now, ReceivedAt: now, FreshFor: 5 * time.Second}
	if err := coordinator.ReportESP32(context.Background(), benchESP32Endpoint, measurement); err != nil {
		t.Fatal(err)
	}
	if len(states.values) != 5 {
		t.Fatalf("all state values = %d", len(states.values))
	}
	endpoint := coordinator.shellyEndpoints[benchShellyEndpoint]
	if err := coordinator.ShellyFailure(context.Background(), endpoint, true, "shelly.unreachable", now); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.ShellyFailure(context.Background(), endpoint, true, "shelly.unreachable", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(commands.commands) != 1 || commands.commands[0].On {
		t.Fatalf("safe commands = %+v", commands.commands)
	}
}

func newCoordinator(t *testing.T, states StateWriter, alarmEngine AlarmEngine, commands Commander) *Coordinator {
	t.Helper()
	shellyEndpoint := shelly.Endpoint{ID: benchShellyEndpoint, EquipmentID: benchEquipment, BaseURL: "http://shelly.local", Channel: 0, PollInterval: time.Second, RequestTimeout: 100 * time.Millisecond, SafeOn: false, PowerReturnPolicy: shelly.PowerReturnOff}
	espEndpoint := esp32.Endpoint{ID: benchESP32Endpoint, DeviceID: benchDevice, BaseURL: "http://esp.local", ProbeIDs: [2]domain.SensorID{benchProbeA, benchProbeB}, PollInterval: time.Second, RequestTimeout: 100 * time.Millisecond, FreshFor: 5 * time.Second, MaximumClockSkew: time.Second, MaximumDifference: 0.5}
	coordinator, err := NewCoordinator(states, alarmEngine, commands, slog.New(slog.NewTextHandler(io.Discard, nil)), []ShellyPolicy{{Endpoint: shellyEndpoint, RuleID: benchShellyRule}}, []ESP32Policy{{Endpoint: espEndpoint, RuleID: benchESPRule}})
	if err != nil {
		t.Fatal(err)
	}
	return coordinator
}

func mustTemperature(t *testing.T, value float64) domain.Quantity {
	t.Helper()
	quantity, err := domain.NewQuantity(domain.QuantityTemperature, value, domain.UnitCelsius)
	if err != nil {
		t.Fatal(err)
	}
	return quantity
}
