package simulator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
)

type recordingDelay struct {
	duration time.Duration
	err      error
}

func (d *recordingDelay) Wait(context.Context, time.Duration) error {
	d.duration = time.Second
	return d.err
}

func TestFakeAdapterAcknowledgesAndReportsWithoutHardware(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	adapter, err := NewFakeAdapter(func() time.Time { return now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	command := simulatorCommand(true)
	ack, err := adapter.Dispatch(context.Background(), command)
	if err != nil || !ack.Accepted || !ack.AcknowledgedAt.Equal(now) {
		t.Fatalf("ack=%+v err=%v", ack, err)
	}
	if reported, exists := adapter.Reported(command.EquipmentID); !exists || !reported {
		t.Fatalf("reported=%v exists=%v", reported, exists)
	}
}

func TestFakeAdapterModelsLostAckAndStuckRelay(t *testing.T) {
	adapter, _ := NewFakeAdapter(time.Now, nil)
	if err := adapter.SetFaults(AdapterFaults{LoseAcknowledgement: true, RelayStuckOn: true}); err != nil {
		t.Fatal(err)
	}
	command := simulatorCommand(false)
	if _, err := adapter.Dispatch(context.Background(), command); !errors.Is(err, ErrAcknowledgementLost) {
		t.Fatalf("lost acknowledgement error=%v", err)
	}
	if reported, exists := adapter.Reported(command.EquipmentID); !exists || !reported {
		t.Fatal("stuck relay did not remain reported on")
	}
}

func TestFakeAdapterDelayIsInjectedAndCancellable(t *testing.T) {
	delay := &recordingDelay{err: context.DeadlineExceeded}
	adapter, _ := NewFakeAdapter(time.Now, delay)
	if err := adapter.SetFaults(AdapterFaults{AcknowledgementDelay: time.Second}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Dispatch(context.Background(), simulatorCommand(true)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("delay error=%v", err)
	}
	if delay.duration != time.Second {
		t.Fatalf("delay=%v", delay.duration)
	}
}

func simulatorCommand(on bool) output.Command {
	return output.Command{EquipmentID: domain.EquipmentID("30000000-0000-4000-8000-000000000003"), On: on}
}
