package output

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/safety"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

const (
	testCommandID     domain.CommandID     = "10000000-0000-4000-8000-000000000001"
	testCorrelationID domain.CorrelationID = "20000000-0000-4000-8000-000000000002"
	testEquipmentID   domain.EquipmentID   = "30000000-0000-4000-8000-000000000003"
)

type fakePolicy struct {
	decision safety.Decision
	reported int
}

func (p *fakePolicy) Evaluate(context.Context, safety.Intent) (safety.Decision, error) {
	return p.decision, nil
}
func (p *fakePolicy) RecordReported(context.Context, domain.EquipmentID, bool, time.Time) error {
	p.reported++
	return nil
}
func (*fakePolicy) CheckWatchdogs(context.Context) ([]safety.WatchdogAction, error) { return nil, nil }

type fakeRevisions struct{ revision domain.Revision }

func (r fakeRevisions) Snapshot(context.Context) (state.Snapshot, error) {
	return state.Snapshot{Revision: r.revision}, nil
}

type fakeExecutor struct {
	calls           int
	acknowledgement Acknowledgement
}

func (e *fakeExecutor) Dispatch(context.Context, Command) (Acknowledgement, error) {
	e.calls++
	return e.acknowledgement, nil
}

type fixedClock struct{ at time.Time }

func (c *fixedClock) Now() time.Time { return c.at }

func newService(t *testing.T, clock *fixedClock, policy *fakePolicy, executor *fakeExecutor) *Service {
	t.Helper()
	factory, err := events.NewFactory(clock, func() (domain.CorrelationID, error) { return testCorrelationID, nil })
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewServiceWithDependencies(policy, fakeRevisions{revision: 7}, executor, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), clock.Now, func() (domain.CommandID, error) { return testCommandID, nil }, func() (domain.CorrelationID, error) { return testCorrelationID, nil }, factory)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
func command(at time.Time) Command {
	revision := domain.Revision(7)
	return Command{IdempotencyKey: "request-1", EquipmentID: testEquipmentID, Requester: "operator", IssuedAt: at, ExpiresAt: at.Add(time.Minute), ExpectedRevision: &revision, On: true}
}

func TestCommandRequiresAcknowledgementAndReconciliationForSuccess(t *testing.T) {
	clock := &fixedClock{at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	policy := &fakePolicy{decision: safety.Decision{Allowed: true, Reason: safety.ReasonAllowed}}
	executor := &fakeExecutor{acknowledgement: Acknowledgement{Accepted: true, AcknowledgedAt: clock.at.Add(time.Second)}}
	service := newService(t, clock, policy, executor)
	result, err := service.Submit(context.Background(), command(clock.at))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusAcknowledged || result.Status == StatusSucceeded {
		t.Fatalf("premature success: %+v", result)
	}
	clock.at = clock.at.Add(2 * time.Second)
	result, err = service.Reconcile(context.Background(), result.Command.ID, true, clock.at)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSucceeded || result.ReconciledAt == nil || policy.reported != 1 {
		t.Fatalf("result=%+v reported=%d", result, policy.reported)
	}
}

func TestExpiredCommandNeverDispatches(t *testing.T) {
	clock := &fixedClock{at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	policy := &fakePolicy{decision: safety.Decision{Allowed: true}}
	executor := &fakeExecutor{}
	service := newService(t, clock, policy, executor)
	expired := command(clock.at.Add(-2 * time.Minute))
	result, err := service.Submit(context.Background(), expired)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusExpired || executor.calls != 0 {
		t.Fatalf("result=%+v calls=%d", result, executor.calls)
	}
}

func TestIdempotentSubmissionDoesNotRedispatch(t *testing.T) {
	clock := &fixedClock{at: time.Now().UTC()}
	policy := &fakePolicy{decision: safety.Decision{Allowed: true}}
	executor := &fakeExecutor{acknowledgement: Acknowledgement{Accepted: true, AcknowledgedAt: clock.at}}
	service := newService(t, clock, policy, executor)
	first, err := service.Submit(context.Background(), command(clock.at))
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Submit(context.Background(), command(clock.at))
	if err != nil {
		t.Fatal(err)
	}
	if first.Command.ID != second.Command.ID || executor.calls != 1 {
		t.Fatalf("first=%+v second=%+v calls=%d", first, second, executor.calls)
	}
}

func TestExpireAcknowledgedRequiresDeadlineAndNeverSucceeds(t *testing.T) {
	clock := &fixedClock{at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	policy := &fakePolicy{decision: safety.Decision{Allowed: true}}
	executor := &fakeExecutor{acknowledgement: Acknowledgement{Accepted: true, AcknowledgedAt: clock.at}}
	service := newService(t, clock, policy, executor)
	result, err := service.Submit(context.Background(), command(clock.at))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ExpireAcknowledged(context.Background(), result.Command.ID, "shelly.reconciliation_expired", result.Command.ExpiresAt.Add(-time.Nanosecond)); err == nil {
		t.Fatal("expected early expiry rejection")
	}
	result, err = service.ExpireAcknowledged(context.Background(), result.Command.ID, "shelly.reconciliation_expired", result.Command.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusExpired || result.Reason != "shelly.reconciliation_expired" {
		t.Fatalf("unexpected expiry: %+v", result)
	}
}
