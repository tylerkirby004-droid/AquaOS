package events

import (
	"context"
	"errors"
	"testing"
)

func TestPublishDeliversInOrderAndReturnsHandlerFailure(t *testing.T) {
	bus := NewBus(1)
	var calls []int
	expected := errors.New("failed")
	_, _ = bus.Subscribe(StateChanged, func(context.Context, Event) error { calls = append(calls, 1); return expected })
	_, _ = bus.Subscribe(StateChanged, func(context.Context, Event) error { calls = append(calls, 2); return nil })
	event, err := New("test", StateChanged, SeverityInfo, map[string]bool{"changed": true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = bus.Publish(context.Background(), event); !errors.Is(err, expected) {
		t.Fatalf("Publish() error = %v", err)
	}
	if len(calls) != 2 || calls[0] != 1 || calls[1] != 2 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestBackpressureFailsFast(t *testing.T) {
	bus := NewBus(1)
	entered := make(chan struct{})
	release := make(chan struct{})
	_, _ = bus.Subscribe(StateChanged, func(context.Context, Event) error { close(entered); <-release; return nil })
	event, _ := New("test", StateChanged, SeverityInfo, map[string]bool{"changed": true}, "")
	done := make(chan error, 1)
	go func() { done <- bus.Publish(context.Background(), event) }()
	<-entered
	if err := bus.Publish(context.Background(), event); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("error = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestStoppedBusRejectsWork(t *testing.T) {
	bus := NewBus()
	_ = bus.Stop(context.Background())
	if err := bus.Publish(context.Background(), Event{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("error = %v", err)
	}
}
