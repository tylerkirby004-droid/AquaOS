package safety

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/equipment"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

const (
	testEquipmentID domain.EquipmentID = "10000000-0000-4000-8000-000000000001"
	testSensorID    domain.EntityID    = "20000000-0000-4000-8000-000000000002"
	testEndpointID  domain.EndpointID  = "30000000-0000-4000-8000-000000000003"
	testOverrideID  domain.OverrideID  = "40000000-0000-4000-8000-000000000004"
)

type fakeStateReader struct {
	value state.Value
	err   error
}

func (r fakeStateReader) Get(context.Context, state.Key) (state.Value, error) { return r.value, r.err }

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }

func heaterProfile() equipment.Profile {
	required := true
	return equipment.Profile{EquipmentID: testEquipmentID, Kind: equipment.KindHeater, Hazardous: true, Capabilities: []domain.Capability{domain.CapabilitySwitch, domain.CapabilityCommandAcknowledgement, domain.CapabilityReportedState}, Limits: equipment.Limits{MaximumOn: time.Hour, MinimumOff: time.Minute}, RequiredInputs: []equipment.InputRequirement{{Key: state.Key{EntityKind: state.EntitySensor, EntityID: testSensorID, Plane: state.PlaneObservation, Attribute: "valid"}, RequireBoolean: &required}}}
}
func goodInput(at time.Time) state.Value {
	value := true
	return state.Value{Value: domain.NewBooleanValue(value), Quality: domain.QualityGood, ObservedAt: at, ReceivedAt: at, FreshFor: time.Minute, Source: testEndpointID}
}
func newEngine(t *testing.T, clock *testClock, reader StateReader) *Engine {
	t.Helper()
	engine, err := NewEngine(reader, slog.New(slog.NewTextHandler(io.Discard, nil)), clock.Now, heaterProfile())
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestHardMaximumOnCannotBeBypassedByModeOrOverride(t *testing.T) {
	for _, mode := range []Mode{ModeNormal, ModeMaintenance, ModeManual} {
		t.Run(string(mode), func(t *testing.T) {
			clock := &testClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			engine := newEngine(t, clock, fakeStateReader{value: goodInput(clock.now)})
			if err := engine.RecordReported(context.Background(), testEquipmentID, true, clock.now); err != nil {
				t.Fatal(err)
			}
			if mode != ModeNormal {
				if err := engine.SetMode(context.Background(), mode); err != nil {
					t.Fatal(err)
				}
				if err := engine.GrantOverride(context.Background(), Override{ID: testOverrideID, EquipmentID: testEquipmentID, Requester: "operator", Reason: "maintenance test", CreatedAt: clock.now, ExpiresAt: clock.now.Add(2 * time.Hour)}); err != nil {
					t.Fatal(err)
				}
			}
			clock.now = clock.now.Add(time.Hour)
			decision, err := engine.Evaluate(context.Background(), Intent{EquipmentID: testEquipmentID, On: true, IssuedAt: clock.now})
			if err != nil {
				t.Fatal(err)
			}
			if decision.Allowed || !decision.HardLimit || decision.Reason != ReasonMaximumOn {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}
}

func TestStaleAndInvalidInputsBlockHazardousCommands(t *testing.T) {
	for _, quality := range []domain.Quality{domain.QualityStale, domain.QualityInvalid, domain.QualitySuspect, domain.QualityUnavailable} {
		t.Run(string(quality), func(t *testing.T) {
			clock := &testClock{now: time.Now().UTC()}
			input := goodInput(clock.now)
			input.Quality = quality
			engine := newEngine(t, clock, fakeStateReader{value: input})
			decision, err := engine.Evaluate(context.Background(), Intent{EquipmentID: testEquipmentID, On: true, IssuedAt: clock.now})
			if err != nil {
				t.Fatal(err)
			}
			if decision.Allowed || !decision.HardLimit {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}
}

func TestMaximumOnWatchdog(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	engine := newEngine(t, clock, fakeStateReader{value: goodInput(clock.now)})
	if err := engine.RecordReported(context.Background(), testEquipmentID, true, clock.now); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Hour)
	actions, err := engine.CheckWatchdogs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Reason != ReasonMaximumOn || actions[0].On {
		t.Fatalf("actions=%+v", actions)
	}
	actions, err = engine.CheckWatchdogs(context.Background())
	if err != nil || len(actions) != 0 {
		t.Fatalf("duplicate watchdog actions=%+v err=%v", actions, err)
	}
}

func TestOverrideExpiryWatchdog(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	engine := newEngine(t, clock, fakeStateReader{value: goodInput(clock.now)})
	if err := engine.RecordReported(context.Background(), testEquipmentID, true, clock.now); err != nil {
		t.Fatal(err)
	}
	if err := engine.GrantOverride(context.Background(), Override{ID: testOverrideID, EquipmentID: testEquipmentID, Requester: "operator", Reason: "service", CreatedAt: clock.now, ExpiresAt: clock.now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Minute)
	actions, err := engine.CheckWatchdogs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Reason != ReasonOverrideExpired || actions[0].On {
		t.Fatalf("actions=%+v", actions)
	}
}
