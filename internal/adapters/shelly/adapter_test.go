package shelly

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
)

const (
	testEndpoint  = domain.EndpointID("11111111-1111-4111-8111-111111111111")
	testEquipment = domain.EquipmentID("22222222-2222-4222-8222-222222222222")
	testCommand   = domain.CommandID("33333333-3333-4333-8333-333333333333")
	testCorr      = domain.CorrelationID("44444444-4444-4444-8444-444444444444")
)

type fakeClient struct {
	mu       sync.Mutex
	status   SwitchStatus
	config   SwitchConfig
	getErr   error
	setErrs  []error
	setCalls int
}

func (f *fakeClient) GetSwitchConfig(context.Context, string, int) (SwitchConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.config, f.getErr
}

func (f *fakeClient) GetSwitchStatus(context.Context, string, int) (SwitchStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.getErr
}
func (f *fakeClient) SetSwitch(_ context.Context, _ string, _ int, on bool) (SetResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	index := f.setCalls
	f.setCalls++
	if index < len(f.setErrs) && f.setErrs[index] != nil {
		return SetResult{}, f.setErrs[index]
	}
	return SetResult{WasOn: !on}, nil
}

type fakeReports struct{ reports chan Report }

func (f *fakeReports) ReportShelly(_ context.Context, report Report) error {
	f.reports <- report
	return nil
}

type fakeTracker struct{ reconciled, expired chan domain.CommandID }

func (f *fakeTracker) Reconcile(_ context.Context, id domain.CommandID, _ bool, _ time.Time) (output.Result, error) {
	f.reconciled <- id
	return output.Result{}, nil
}
func (f *fakeTracker) ExpireAcknowledged(_ context.Context, id domain.CommandID, _ string, _ time.Time) (output.Result, error) {
	f.expired <- id
	return output.Result{}, nil
}

type failureRecord struct {
	active bool
	reason string
}
type fakeFailures struct{ records chan failureRecord }

func (f *fakeFailures) ShellyFailure(_ context.Context, _ Endpoint, active bool, reason string, _ time.Time) error {
	f.records <- failureRecord{active: active, reason: reason}
	return nil
}

func TestDispatchRetriesAndRequiresReportedReconciliation(t *testing.T) {
	client := &fakeClient{status: SwitchStatus{ID: 0, Output: true}, config: SwitchConfig{ID: 0, InitialState: "off"}, setErrs: []error{errors.New("temporary")}}
	reports := &fakeReports{reports: make(chan Report, 4)}
	tracker := &fakeTracker{reconciled: make(chan domain.CommandID, 1), expired: make(chan domain.CommandID, 1)}
	failures := &fakeFailures{records: make(chan failureRecord, 4)}
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	adapter := newTestAdapter(t, client, reports, tracker, failures, func() time.Time { return now })
	command := output.Command{ID: testCommand, CorrelationID: testCorr, EquipmentID: testEquipment, IdempotencyKey: "test", Requester: "test", IssuedAt: now, ExpiresAt: now.Add(time.Minute), On: true}
	ack, err := adapter.Dispatch(context.Background(), command)
	if err != nil || !ack.Accepted {
		t.Fatalf("dispatch: ack=%+v err=%v", ack, err)
	}
	if client.setCalls != 2 {
		t.Fatalf("set calls = %d, want 2", client.setCalls)
	}
	adapter.poll(context.Background(), adapter.endpoints[testEquipment])
	select {
	case id := <-tracker.reconciled:
		if id != testCommand {
			t.Fatalf("reconciled %s", id)
		}
	default:
		t.Fatal("expected reconciliation")
	}
}

func TestUnavailableEndpointExpiresPendingAndRaisesFailure(t *testing.T) {
	client := &fakeClient{status: SwitchStatus{ID: 0}, config: SwitchConfig{ID: 0, InitialState: "off"}, getErr: context.DeadlineExceeded}
	reports := &fakeReports{reports: make(chan Report, 2)}
	tracker := &fakeTracker{reconciled: make(chan domain.CommandID, 1), expired: make(chan domain.CommandID, 1)}
	failures := &fakeFailures{records: make(chan failureRecord, 4)}
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	adapter := newTestAdapter(t, client, reports, tracker, failures, func() time.Time { return now })
	adapter.pending[testEquipment] = pendingCommand{id: testCommand, expiresAt: now.Add(-time.Second)}
	adapter.poll(context.Background(), adapter.endpoints[testEquipment])
	select {
	case <-tracker.expired:
	default:
		t.Fatal("expected pending command expiry")
	}
	select {
	case record := <-failures.records:
		if !record.active || record.reason != "shelly.config_unavailable" {
			t.Fatalf("unexpected failure: %+v", record)
		}
	default:
		t.Fatal("expected availability failure")
	}
}

func TestStartStopJoinsReconciliationWorker(t *testing.T) {
	client := &fakeClient{status: SwitchStatus{ID: 0}, config: SwitchConfig{ID: 0, InitialState: "off"}}
	adapter := newTestAdapter(t, client, &fakeReports{reports: make(chan Report, 8)}, &fakeTracker{reconciled: make(chan domain.CommandID, 1), expired: make(chan domain.CommandID, 1)}, &fakeFailures{records: make(chan failureRecord, 8)}, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	if err := adapter.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := adapter.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func newTestAdapter(t *testing.T, client Client, reports ReportedSink, tracker CommandTracker, failures FailureSink, now func() time.Time) *Adapter {
	t.Helper()
	adapter, err := NewAdapter(client, reports, tracker, failures, slog.New(slog.NewTextHandler(io.Discard, nil)), now, Endpoint{ID: testEndpoint, EquipmentID: testEquipment, BaseURL: "http://shelly.local", Channel: 0, PollInterval: time.Second, RequestTimeout: 100 * time.Millisecond, Retries: 1, PowerReturnPolicy: PowerReturnOff})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}
