// Package state owns revisioned canonical state; it is not historical storage.
package state

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

// ErrNotFound is returned when a canonical state key is absent.
var ErrNotFound = errors.New("state not found")

// EntityKind distinguishes heterogeneous canonical entities.
type EntityKind string

//nolint:revive // EntityKind documents the closed set in this block.
const (
	EntitySensor    EntityKind = "sensor"
	EntityEquipment EntityKind = "equipment"
)

// Plane separates observations, desired state, and reported state.
type Plane string

//nolint:revive // Plane documents the closed set in this block.
const (
	PlaneObservation Plane = "observation"
	PlaneDesired     Plane = "desired"
	PlaneReported    Plane = "reported"
)

// Key uniquely identifies one canonical state value.
type Key struct {
	EntityKind EntityKind      `json:"entityKind"`
	EntityID   domain.EntityID `json:"entityId"`
	Plane      Plane           `json:"plane"`
	Attribute  string          `json:"attribute"`
}

// Value is one immutable canonical state entry.
type Value struct {
	Key        Key               `json:"key"`
	Value      domain.Value      `json:"value"`
	Quality    domain.Quality    `json:"quality"`
	ObservedAt time.Time         `json:"observedAt"`
	ReceivedAt time.Time         `json:"receivedAt"`
	FreshFor   time.Duration     `json:"freshFor"`
	Source     domain.EndpointID `json:"source"`
	Revision   domain.Revision   `json:"revision"`
}

// Snapshot is an immutable, consistently revised view of canonical state.
type Snapshot struct {
	Revision domain.Revision `json:"revision"`
	Values   []Value         `json:"values"`
}

// Update is a bounded subscription delivery; Dropped counts superseded updates.
type Update struct {
	Snapshot Snapshot `json:"snapshot"`
	Dropped  uint64   `json:"dropped"`
}

// Subscription owns one bounded update stream.
type Subscription interface {
	Updates() <-chan Update
	Close()
}

// StateManager provides concurrency-safe canonical-state operations.
//
//nolint:revive // The governing architecture explicitly names this contract.
type StateManager interface {
	health.Component
	Set(context.Context, Value) (Value, error)
	Get(context.Context, Key) (Value, error)
	Snapshot(context.Context) (Snapshot, error)
	Delete(context.Context, Key) error
	Subscribe(context.Context, int) (Subscription, error)
}

type subscription struct {
	manager *Manager
	id      uint64
	updates chan Update
	once    sync.Once
}

func (s *subscription) Updates() <-chan Update { return s.updates }
func (s *subscription) Close()                 { s.once.Do(func() { s.manager.removeSubscription(s.id) }) }

// Manager is an in-memory canonical store with non-blocking bounded fan-out.
type Manager struct {
	mu             sync.RWMutex
	values         map[Key]Value
	publisher      events.Publisher
	started        bool
	revision       domain.Revision
	subscribers    map[uint64]*subscription
	nextSubscriber uint64
	now            func() time.Time
}

// Option customizes deterministic state dependencies.
type Option func(*Manager)

// WithClock injects freshness evaluation time.
func WithClock(now func() time.Time) Option {
	return func(manager *Manager) {
		if now != nil {
			manager.now = now
		}
	}
}

// NewManager constructs an empty canonical state manager.
func NewManager(publisher events.Publisher, options ...Option) *Manager {
	manager := &Manager{values: make(map[Key]Value), publisher: publisher, subscribers: make(map[uint64]*subscription), now: time.Now}
	for _, option := range options {
		option(manager)
	}
	return manager
}

// Name returns the lifecycle component name.
func (m *Manager) Name() string { return "state" }

// Start marks the canonical store ready.
func (m *Manager) Start(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return nil
}

// Stop marks the store unavailable and closes all subscription channels.
func (m *Manager) Stop(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	for id, subscriber := range m.subscribers {
		delete(m.subscribers, id)
		close(subscriber.updates)
	}
	return nil
}

// Health reports canonical-store lifecycle health.
func (m *Manager) Health() health.Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := health.StateUnhealthy
	if m.started {
		state = health.StateHealthy
	}
	return health.NewStatus(m.Name(), state, "", m.now().UTC())
}

// Set validates and atomically advances the global revision.
func (m *Manager) Set(ctx context.Context, value Value) (Value, error) {
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}
	if err := validate(value); err != nil {
		return Value{}, err
	}
	value = clone(value)
	m.mu.Lock()
	m.revision++
	value.Revision = m.revision
	m.values[value.Key] = value
	snapshot := m.snapshotLocked()
	m.deliverLocked(snapshot)
	m.mu.Unlock()
	if m.publisher != nil {
		event, err := events.New(m.Name(), events.StateChanged, events.SeverityInfo, value, events.CorrelationIDFromContext(ctx))
		if err != nil {
			return Value{}, err
		}
		if err := m.publisher.Publish(ctx, event); err != nil {
			return Value{}, err
		}
	}
	return clone(value), nil
}

// Get returns a value with freshness evaluated at the injected current time.
func (m *Manager) Get(ctx context.Context, key Key) (Value, error) {
	if err := ctx.Err(); err != nil {
		return Value{}, err
	}
	m.mu.RLock()
	value, ok := m.values[key]
	now := m.now()
	m.mu.RUnlock()
	if !ok {
		return Value{}, ErrNotFound
	}
	value = clone(value)
	if !now.Before(value.ObservedAt.Add(value.FreshFor)) {
		value.Quality = domain.QualityStale
	}
	return value, nil
}

// Snapshot returns an immutable stable-order view at one revision.
func (m *Manager) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshotLocked(), nil
}

// Delete removes a value and advances the global revision.
func (m *Manager) Delete(ctx context.Context, key Key) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	if _, ok := m.values[key]; !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	delete(m.values, key)
	m.revision++
	snapshot := m.snapshotLocked()
	m.deliverLocked(snapshot)
	m.mu.Unlock()
	return nil
}

// Subscribe creates a bounded stream and immediately offers the current snapshot.
func (m *Manager) Subscribe(ctx context.Context, capacity int) (Subscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if capacity < 1 || capacity > 1024 {
		return nil, errors.New("subscription capacity must be between 1 and 1024")
	}
	m.mu.Lock()
	m.nextSubscriber++
	subscriber := &subscription{manager: m, id: m.nextSubscriber, updates: make(chan Update, capacity)}
	m.subscribers[subscriber.id] = subscriber
	subscriber.updates <- Update{Snapshot: m.snapshotLocked()}
	m.mu.Unlock()
	return subscriber, nil
}

func (m *Manager) snapshotLocked() Snapshot {
	values := make([]Value, 0, len(m.values))
	now := m.now()
	for _, value := range m.values {
		item := clone(value)
		if !now.Before(item.ObservedAt.Add(item.FreshFor)) {
			item.Quality = domain.QualityStale
		}
		values = append(values, item)
	}
	sort.Slice(values, func(i, j int) bool { return keyString(values[i].Key) < keyString(values[j].Key) })
	return Snapshot{Revision: m.revision, Values: values}
}
func (m *Manager) deliverLocked(snapshot Snapshot) {
	for _, subscriber := range m.subscribers {
		update := Update{Snapshot: cloneSnapshot(snapshot)}
		select {
		case subscriber.updates <- update:
		default:
			select {
			case previous := <-subscriber.updates:
				update.Dropped = previous.Dropped + 1
			default:
			}
			subscriber.updates <- update
		}
	}
}
func (m *Manager) removeSubscription(id uint64) {
	m.mu.Lock()
	subscriber, ok := m.subscribers[id]
	if ok {
		delete(m.subscribers, id)
		close(subscriber.updates)
	}
	m.mu.Unlock()
}
func validate(value Value) error {
	if value.Key.EntityKind != EntitySensor && value.Key.EntityKind != EntityEquipment {
		return errors.New("unsupported entity kind")
	}
	if value.Key.Plane != PlaneObservation && value.Key.Plane != PlaneDesired && value.Key.Plane != PlaneReported {
		return errors.New("unsupported state plane")
	}
	if value.Key.EntityKind == EntitySensor && value.Key.Plane != PlaneObservation {
		return errors.New("sensor state must use observation plane")
	}
	if value.Key.EntityKind == EntityEquipment && value.Key.Plane == PlaneObservation {
		return errors.New("equipment state must use desired or reported plane")
	}
	if err := value.Key.EntityID.Validate(); err != nil {
		return fmt.Errorf("entity ID: %w", err)
	}
	if value.Key.Attribute == "" {
		return errors.New("state attribute is required")
	}
	if err := value.Value.Validate(); err != nil {
		return err
	}
	if !validQuality(value.Quality) {
		return errors.New("unsupported state quality")
	}
	if value.ObservedAt.IsZero() || value.ReceivedAt.IsZero() || value.ObservedAt.After(value.ReceivedAt) {
		return errors.New("invalid state timestamps")
	}
	if value.FreshFor <= 0 {
		return errors.New("freshness duration must be positive")
	}
	if err := value.Source.Validate(); err != nil {
		return fmt.Errorf("source endpoint: %w", err)
	}
	return nil
}
func validQuality(value domain.Quality) bool {
	return value == domain.QualityGood || value == domain.QualitySuspect || value == domain.QualityStale || value == domain.QualityInvalid || value == domain.QualityUnavailable
}
func clone(value Value) Value { value.Value = value.Value.Clone(); return value }
func cloneSnapshot(value Snapshot) Snapshot {
	result := Snapshot{Revision: value.Revision, Values: make([]Value, len(value.Values))}
	for index := range value.Values {
		result.Values[index] = clone(value.Values[index])
	}
	return result
}
func keyString(key Key) string {
	return string(key.EntityKind) + "/" + string(key.EntityID) + "/" + string(key.Plane) + "/" + key.Attribute
}
