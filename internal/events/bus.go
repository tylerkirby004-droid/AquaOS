package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

var (
	// ErrClosed is returned after shutdown.
	ErrClosed = errors.New("event bus is closed")
	// ErrBackpressure means the configured concurrent-delivery bound was reached.
	ErrBackpressure = errors.New("event bus delivery capacity exhausted")
)

// Bus uses bounded, synchronous delivery: publishers receive handler failures directly,
// no goroutines are hidden, and overload is rejected instead of consuming unbounded memory.
type Bus struct {
	mu       sync.RWMutex
	handlers map[Type][]*subscription
	closed   bool
	slots    chan struct{}
}
type subscription struct {
	topic   Type
	handler Handler
	active  atomic.Bool
}

// NewBus constructs a bus allowing at most capacity concurrent publications.
func NewBus(capacities ...int) *Bus {
	capacity := 64
	if len(capacities) > 0 {
		capacity = capacities[0]
	}
	if capacity < 1 {
		capacity = 1
	}
	return &Bus{handlers: make(map[Type][]*subscription), slots: make(chan struct{}, capacity)}
}

// Name returns the lifecycle component name.
func (b *Bus) Name() string { return "events" }

// Start verifies that the bus has not already stopped.
func (b *Bus) Start(context.Context) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrClosed
	}
	return nil
}

// Stop permanently closes the bus and releases its registrations.
func (b *Bus) Stop(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.handlers = make(map[Type][]*subscription)
	return nil
}

// Health reports whether the bus can accept publications.
func (b *Bus) Health() health.Status {
	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	state := health.StateHealthy
	detail := ""
	if closed {
		state = health.StateUnhealthy
		detail = ErrClosed.Error()
	}
	return health.NewStatus(b.Name(), state, detail, time.Now().UTC())
}

// Subscribe registers a handler. Handlers run in stable registration order.
func (b *Bus) Subscribe(topic Type, handler Handler) (Subscription, error) {
	if topic == "" || handler == nil {
		return nil, errors.New("event type and handler are required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrClosed
	}
	s := &subscription{topic: topic, handler: handler}
	s.active.Store(true)
	b.handlers[topic] = append(b.handlers[topic], s)
	return s, nil
}

// Publish fails fast on overload. It invokes every active handler and joins their errors;
// there is deliberately no implicit retry because retry safety belongs to each consumer.
func (b *Bus) Publish(ctx context.Context, event Event) error {
	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return ErrClosed
	}
	if err := event.Validate(); err != nil {
		return err
	}
	select {
	case b.slots <- struct{}{}:
		defer func() { <-b.slots }()
	default:
		return ErrBackpressure
	}
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrClosed
	}
	handlers := append([]*subscription(nil), b.handlers[event.EventType]...)
	b.mu.RUnlock()
	var failures []error
	for _, s := range handlers {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		if !s.active.Load() {
			continue
		}
		delivered := event
		delivered.Payload = append([]byte(nil), event.Payload...)
		if err := s.handler(ctx, delivered); err != nil {
			failures = append(failures, fmt.Errorf("handler for %s: %w", s.topic, err))
		}
	}
	return errors.Join(failures...)
}
func (s *subscription) Unsubscribe() { s.active.Store(false) }
