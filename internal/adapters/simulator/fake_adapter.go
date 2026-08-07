package simulator

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
)

var (
	// ErrAcknowledgementLost indicates that a simulated command may have been
	// applied but its acknowledgement was intentionally discarded.
	ErrAcknowledgementLost = errors.New("simulated acknowledgement lost")
)

// Delay waits for simulated acknowledgement latency and must honor context.
type Delay interface {
	Wait(context.Context, time.Duration) error
}

type timerDelay struct{}

func (timerDelay) Wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// AdapterFaults configures deterministic fake-adapter failures.
type AdapterFaults struct {
	AcknowledgementDelay time.Duration
	LoseAcknowledgement  bool
	RelayStuckOn         bool
}

// FakeAdapter is an in-memory output.Executor. It has no network, GPIO, file,
// process, or vendor-client dependency and therefore cannot reach hardware.
type FakeAdapter struct {
	mu       sync.RWMutex
	now      func() time.Time
	delay    Delay
	faults   AdapterFaults
	reported map[domain.EquipmentID]bool
}

// NewFakeAdapter constructs a hardware-incapable adapter with injected time.
func NewFakeAdapter(now func() time.Time, delay Delay) (*FakeAdapter, error) {
	if now == nil {
		return nil, errors.New("fake adapter clock is required")
	}
	if delay == nil {
		delay = timerDelay{}
	}
	return &FakeAdapter{now: now, delay: delay, reported: make(map[domain.EquipmentID]bool)}, nil
}

// SetFaults atomically replaces fault injection for subsequent commands.
func (a *FakeAdapter) SetFaults(faults AdapterFaults) error {
	if faults.AcknowledgementDelay < 0 || faults.AcknowledgementDelay > time.Hour {
		return errors.New("fake acknowledgement delay must be between zero and one hour")
	}
	a.mu.Lock()
	a.faults = faults
	a.mu.Unlock()
	return nil
}

// Dispatch applies a command to memory and returns an explicit acknowledgement.
func (a *FakeAdapter) Dispatch(ctx context.Context, command output.Command) (output.Acknowledgement, error) {
	if err := ctx.Err(); err != nil {
		return output.Acknowledgement{}, err
	}
	a.mu.RLock()
	faults := a.faults
	a.mu.RUnlock()
	if faults.AcknowledgementDelay > 0 {
		if err := a.delay.Wait(ctx, faults.AcknowledgementDelay); err != nil {
			return output.Acknowledgement{}, err
		}
	}
	a.mu.Lock()
	reported := command.On
	if faults.RelayStuckOn && !command.On {
		reported = true
	}
	a.reported[command.EquipmentID] = reported
	a.mu.Unlock()
	if faults.LoseAcknowledgement {
		return output.Acknowledgement{}, ErrAcknowledgementLost
	}
	return output.Acknowledgement{Accepted: true, Reason: output.ReasonAcknowledged, AcknowledgedAt: a.now().UTC()}, nil
}

// Reported returns the fake physical state for reconciliation.
func (a *FakeAdapter) Reported(equipmentID domain.EquipmentID) (bool, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	value, exists := a.reported[equipmentID]
	return value, exists
}

var _ output.Executor = (*FakeAdapter)(nil)
