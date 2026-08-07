package alarms

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
)

const (
	testRuleID        domain.RuleID        = "10000000-0000-4000-8000-000000000001"
	testSubjectID     domain.EntityID      = "20000000-0000-4000-8000-000000000002"
	testAlarmID       domain.AlarmID       = "30000000-0000-4000-8000-000000000003"
	testCorrelationID domain.CorrelationID = "40000000-0000-4000-8000-000000000004"
)

type fixedClock struct{ at time.Time }

func (c *fixedClock) Now() time.Time { return c.at }

type recordingPublisher struct{ event events.Event }

func (p *recordingPublisher) Publish(_ context.Context, event events.Event) error {
	p.event = event
	return nil
}

func newTestManager(t *testing.T, rule Rule) (*Manager, *fixedClock) {
	t.Helper()
	clock := &fixedClock{at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	factory, err := events.NewFactory(clock, func() (domain.CorrelationID, error) { return testCorrelationID, nil })
	if err != nil {
		t.Fatal(err)
	}
	m := NewManagerWithDependencies(events.NewBus(), slog.New(slog.NewTextHandler(io.Discard, nil)), clock.Now, func() (domain.AlarmID, error) { return testAlarmID, nil }, factory, 3)
	if err = m.RegisterRule(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
	return m, clock
}
func baseRule() Rule {
	return Rule{ID: testRuleID, Code: "water.temperature.high", Name: "high temperature", Subject: Subject{Kind: "sensor", ID: testSubjectID}, Severity: events.SeverityWarning, Debounce: time.Minute, Hysteresis: 30 * time.Second}
}
func observe(t *testing.T, m *Manager, at time.Time, active bool, severity events.Severity) (Alarm, Transition) {
	t.Helper()
	a, tr, err := m.Observe(context.Background(), Observation{RuleID: testRuleID, Active: active, Severity: severity, ObservedAt: at, Evidence: Evidence{Code: "sensor.reading", Message: "sample"}, CorrelationID: testCorrelationID})
	if err != nil {
		t.Fatal(err)
	}
	return a, tr
}

func TestAlarmLifecycleTransitions(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		offset    time.Duration
		active    bool
		severity  events.Severity
		want      Transition
		status    Status
		condition bool
	}{
		{"begin debounce", 0, true, "", TransitionDebouncing, "", false}, {"raise", time.Minute, true, "", TransitionRaised, StatusActive, true}, {"repeat", 2 * time.Minute, true, "", TransitionNone, StatusActive, true}, {"escalate", 3 * time.Minute, true, events.SeverityCritical, TransitionEscalated, StatusActive, true}, {"begin hysteresis", 4 * time.Minute, false, "", TransitionClearPending, StatusActive, false}, {"clear", 4*time.Minute + 30*time.Second, false, "", TransitionCleared, StatusCleared, false}}
	m, _ := newTestManager(t, baseRule())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, tr := observe(t, m, start.Add(tt.offset), tt.active, tt.severity)
			if tr != tt.want {
				t.Fatalf("transition=%s want %s", tr, tt.want)
			}
			if tt.status != "" && (a.Status != tt.status || a.ConditionActive != tt.condition) {
				t.Fatalf("alarm=%+v", a)
			}
		})
	}
}

func TestDuplicateAndOutOfOrderObservationsAreIgnored(t *testing.T) {
	m, _ := newTestManager(t, func() Rule { r := baseRule(); r.Debounce = 0; return r }())
	at := time.Now().UTC()
	a, _ := observe(t, m, at, true, "")
	duplicate, tr := observe(t, m, at, true, events.SeverityCritical)
	if tr != TransitionIgnoredStale || duplicate.OccurrenceCount != a.OccurrenceCount || duplicate.Severity != a.Severity {
		t.Fatalf("duplicate mutated alarm: %+v", duplicate)
	}
	older, tr := observe(t, m, at.Add(-time.Second), false, "")
	if tr != TransitionIgnoredStale || !older.ConditionActive {
		t.Fatalf("stale observation mutated alarm: %+v", older)
	}
}

func TestAcknowledgementDoesNotClearCondition(t *testing.T) {
	m, clock := newTestManager(t, func() Rule { r := baseRule(); r.Debounce = 0; return r }())
	a, _ := observe(t, m, clock.at, true, "")
	clock.at = clock.at.Add(time.Second)
	ack, err := m.Acknowledge(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != StatusAcknowledged || !ack.ConditionActive || ack.ClearedAt != nil {
		t.Fatalf("acknowledgement cleared condition: %+v", ack)
	}
	if _, err = m.Clear(context.Background(), a.ID); !errors.Is(err, ErrConditionActive) {
		t.Fatalf("Clear() error=%v", err)
	}
}

func TestLatchingAlarmRequiresExplicitClear(t *testing.T) {
	rule := baseRule()
	rule.Debounce = 0
	rule.Hysteresis = 0
	rule.Latching = true
	m, _ := newTestManager(t, rule)
	at := time.Now().UTC()
	_, _ = observe(t, m, at, true, "")
	a, tr := observe(t, m, at.Add(time.Second), false, "")
	if tr != TransitionClearPending || a.Status == StatusCleared {
		t.Fatalf("latching alarm cleared: %+v", a)
	}
	cleared, err := m.Clear(context.Background(), a.ID)
	if err != nil || cleared.Status != StatusCleared {
		t.Fatalf("clear=%+v err=%v", cleared, err)
	}
}

func TestTransitionEventHasStableCodeAndCorrelationID(t *testing.T) {
	clock := &fixedClock{at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	factory, err := events.NewFactory(clock, func() (domain.CorrelationID, error) { return testCorrelationID, nil })
	if err != nil {
		t.Fatal(err)
	}
	publisher := &recordingPublisher{}
	manager := NewManagerWithDependencies(publisher, slog.New(slog.NewTextHandler(io.Discard, nil)), clock.Now, func() (domain.AlarmID, error) { return testAlarmID, nil }, factory, 3)
	rule := baseRule()
	rule.Debounce = 0
	if err = manager.RegisterRule(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
	_, transition := observe(t, manager, clock.at, true, "")
	if transition != TransitionRaised || publisher.event.EventType != events.AlarmRaised || publisher.event.CorrelationID != testCorrelationID {
		t.Fatalf("event = %+v, transition = %s", publisher.event, transition)
	}
}
