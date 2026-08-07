// Package sensors owns generic sensor identity and validated adapter ownership.
package sensors

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

// Registry errors are stable values callers may inspect with errors.Is.
var (
	ErrNotFound = errors.New("sensor not found")
	ErrExists   = errors.New("sensor already exists")
)

// EndpointLookup is the consumer-owned ownership boundary used by the registry.
type EndpointLookup interface {
	Endpoint(context.Context, domain.EndpointID) (domain.Endpoint, error)
}

// SensorRegistry manages generic sensor definitions.
type SensorRegistry interface {
	health.Component
	Register(context.Context, domain.Sensor) (domain.Sensor, error)
	Update(context.Context, domain.Sensor) error
	Get(context.Context, domain.SensorID) (domain.Sensor, error)
	List(context.Context) ([]domain.Sensor, error)
	Remove(context.Context, domain.SensorID) error
}

// Update replaces an existing sensor after repeating ownership validation.
func (r *Registry) Update(ctx context.Context, value domain.Sensor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.validate(ctx, value); err != nil {
		return err
	}
	value = clone(value)
	r.mu.Lock()
	if _, ok := r.sensors[value.ID]; !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	r.sensors[value.ID] = value
	r.mu.Unlock()
	return r.emit(ctx, events.SensorUpdated, value)
}

// SensorManager is the stable architecture name for SensorRegistry.
type SensorManager interface{ SensorRegistry }

// Registry is the concurrency-safe in-memory SensorRegistry implementation.
type Registry struct {
	mu        sync.RWMutex
	sensors   map[domain.SensorID]domain.Sensor
	owners    EndpointLookup
	publisher events.Publisher
	started   bool
}

// NewRegistry constructs a sensor registry with explicit ownership lookup.
func NewRegistry(publisher events.Publisher, owners ...EndpointLookup) *Registry {
	var owner EndpointLookup
	if len(owners) > 0 {
		owner = owners[0]
	}
	return &Registry{sensors: make(map[domain.SensorID]domain.Sensor), owners: owner, publisher: publisher}
}

// Name returns the lifecycle component name.
func (r *Registry) Name() string { return "sensors" }

// Start marks the registry ready.
func (r *Registry) Start(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = true
	return nil
}

// Stop marks the registry unavailable without discarding identity.
func (r *Registry) Stop(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = false
	return nil
}

// Health reports registry lifecycle health.
func (r *Registry) Health() health.Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := health.StateUnhealthy
	if r.started {
		state = health.StateHealthy
	}
	return health.NewStatus(r.Name(), state, "", time.Now().UTC())
}

// Register validates identity, ownership, unit, and capability constraints.
func (r *Registry) Register(ctx context.Context, value domain.Sensor) (domain.Sensor, error) {
	if err := ctx.Err(); err != nil {
		return domain.Sensor{}, err
	}
	if value.ID == "" {
		id, err := domain.NewSensorID()
		if err != nil {
			return domain.Sensor{}, err
		}
		value.ID = id
	}
	if err := r.validate(ctx, value); err != nil {
		return domain.Sensor{}, err
	}
	value = clone(value)
	r.mu.Lock()
	if _, ok := r.sensors[value.ID]; ok {
		r.mu.Unlock()
		return domain.Sensor{}, ErrExists
	}
	r.sensors[value.ID] = value
	r.mu.Unlock()
	return clone(value), r.emit(ctx, events.SensorRegistered, value)
}

// Get returns an immutable sensor snapshot.
func (r *Registry) Get(ctx context.Context, id domain.SensorID) (domain.Sensor, error) {
	if err := ctx.Err(); err != nil {
		return domain.Sensor{}, err
	}
	r.mu.RLock()
	value, ok := r.sensors[id]
	r.mu.RUnlock()
	if !ok {
		return domain.Sensor{}, ErrNotFound
	}
	return clone(value), nil
}

// List returns sensors in stable ID order.
func (r *Registry) List(ctx context.Context) ([]domain.Sensor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	result := make([]domain.Sensor, 0, len(r.sensors))
	for _, value := range r.sensors {
		result = append(result, clone(value))
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// Remove deletes one sensor definition.
func (r *Registry) Remove(ctx context.Context, id domain.SensorID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	if _, ok := r.sensors[id]; !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	delete(r.sensors, id)
	r.mu.Unlock()
	return r.emit(ctx, events.SensorRemoved, map[string]domain.SensorID{"id": id})
}

func (r *Registry) validate(ctx context.Context, value domain.Sensor) error {
	if err := value.ID.Validate(); err != nil {
		return fmt.Errorf("sensor ID: %w", err)
	}
	if value.Name == "" {
		return errors.New("sensor name is required")
	}
	if err := domain.ValidateCapabilities(value.Capabilities); err != nil {
		return err
	}
	if len(value.Capabilities) != 1 || value.Capabilities[0] != domain.CapabilityObserve {
		return errors.New("sensor supports only the observe capability")
	}
	quantity, err := domain.NewQuantity(value.Quantity, 0, value.Unit)
	if err != nil {
		return err
	}
	_ = quantity
	if r.owners == nil {
		return errors.New("sensor ownership lookup is required")
	}
	endpoint, err := r.owners.Endpoint(ctx, value.EndpointID)
	if err != nil {
		return fmt.Errorf("endpoint ownership: %w", err)
	}
	if endpoint.DeviceID != value.DeviceID {
		return errors.New("sensor device does not own endpoint")
	}
	if !domain.SupportsAll(endpoint.Capabilities, value.Capabilities) {
		return errors.New("sensor capabilities exceed endpoint capabilities")
	}
	return nil
}
func clone(value domain.Sensor) domain.Sensor {
	value.Capabilities = append([]domain.Capability(nil), value.Capabilities...)
	if value.Metadata != nil {
		metadata := make(map[string]string, len(value.Metadata))
		for key, item := range value.Metadata {
			metadata[key] = item
		}
		value.Metadata = metadata
	}
	return value
}
func (r *Registry) emit(ctx context.Context, eventType events.Type, payload any) error {
	if r.publisher == nil {
		return nil
	}
	event, err := events.New(r.Name(), eventType, events.SeverityInfo, payload, events.CorrelationIDFromContext(ctx))
	if err != nil {
		return err
	}
	return r.publisher.Publish(ctx, event)
}
