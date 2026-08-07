// Package equipment owns generic logical output identity, not control policy.
package equipment

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
	ErrNotFound = errors.New("equipment not found")
	ErrExists   = errors.New("equipment already exists")
)

// EndpointLookup is the consumer-owned ownership boundary used by the registry.
type EndpointLookup interface {
	Endpoint(context.Context, domain.EndpointID) (domain.Endpoint, error)
}

// EquipmentRegistry manages equipment definitions without issuing commands.
//
//nolint:revive // The governing architecture explicitly names this contract.
type EquipmentRegistry interface {
	health.Component
	Register(context.Context, domain.Equipment) (domain.Equipment, error)
	Update(context.Context, domain.Equipment) error
	Get(context.Context, domain.EquipmentID) (domain.Equipment, error)
	List(context.Context) ([]domain.Equipment, error)
	Remove(context.Context, domain.EquipmentID) error
}

// Update replaces existing equipment after repeating ownership validation.
func (r *Registry) Update(ctx context.Context, value domain.Equipment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.validate(ctx, value); err != nil {
		return err
	}
	value = clone(value)
	r.mu.Lock()
	if _, ok := r.equipment[value.ID]; !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	r.equipment[value.ID] = value
	r.mu.Unlock()
	return r.emit(ctx, events.EquipmentUpdated, value)
}

// EquipmentManager is the stable architecture name for EquipmentRegistry.
//
//nolint:revive // The governing architecture explicitly names this contract.
type EquipmentManager interface{ EquipmentRegistry }

// Registry is the concurrency-safe in-memory EquipmentRegistry implementation.
type Registry struct {
	mu        sync.RWMutex
	equipment map[domain.EquipmentID]domain.Equipment
	owners    EndpointLookup
	publisher events.Publisher
	started   bool
}

// NewRegistry constructs an equipment registry with explicit ownership lookup.
func NewRegistry(publisher events.Publisher, owners ...EndpointLookup) *Registry {
	var owner EndpointLookup
	if len(owners) > 0 {
		owner = owners[0]
	}
	return &Registry{equipment: make(map[domain.EquipmentID]domain.Equipment), owners: owner, publisher: publisher}
}

// Name returns the lifecycle component name.
func (r *Registry) Name() string { return "equipment" }

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

// Register validates identity, ownership, and capability constraints.
func (r *Registry) Register(ctx context.Context, value domain.Equipment) (domain.Equipment, error) {
	if err := ctx.Err(); err != nil {
		return domain.Equipment{}, err
	}
	if value.ID == "" {
		id, err := domain.NewEquipmentID()
		if err != nil {
			return domain.Equipment{}, err
		}
		value.ID = id
	}
	if err := r.validate(ctx, value); err != nil {
		return domain.Equipment{}, err
	}
	value = clone(value)
	r.mu.Lock()
	if _, ok := r.equipment[value.ID]; ok {
		r.mu.Unlock()
		return domain.Equipment{}, ErrExists
	}
	r.equipment[value.ID] = value
	r.mu.Unlock()
	return clone(value), r.emit(ctx, events.EquipmentRegistered, value)
}

// Get returns an immutable equipment snapshot.
func (r *Registry) Get(ctx context.Context, id domain.EquipmentID) (domain.Equipment, error) {
	if err := ctx.Err(); err != nil {
		return domain.Equipment{}, err
	}
	r.mu.RLock()
	value, ok := r.equipment[id]
	r.mu.RUnlock()
	if !ok {
		return domain.Equipment{}, ErrNotFound
	}
	return clone(value), nil
}

// List returns equipment in stable ID order.
func (r *Registry) List(ctx context.Context) ([]domain.Equipment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	result := make([]domain.Equipment, 0, len(r.equipment))
	for _, value := range r.equipment {
		result = append(result, clone(value))
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// Remove deletes one equipment definition without issuing a command.
func (r *Registry) Remove(ctx context.Context, id domain.EquipmentID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	if _, ok := r.equipment[id]; !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	delete(r.equipment, id)
	r.mu.Unlock()
	return r.emit(ctx, events.EquipmentRemoved, map[string]domain.EquipmentID{"id": id})
}
func (r *Registry) validate(ctx context.Context, value domain.Equipment) error {
	if err := value.ID.Validate(); err != nil {
		return fmt.Errorf("equipment ID: %w", err)
	}
	if value.Name == "" {
		return errors.New("equipment name is required")
	}
	if err := domain.ValidateCapabilities(value.Capabilities); err != nil {
		return err
	}
	if !domain.SupportsAll(value.Capabilities, []domain.Capability{domain.CapabilitySwitch}) && !domain.SupportsAll(value.Capabilities, []domain.Capability{domain.CapabilityVariableOutput}) {
		return errors.New("equipment requires switch or variable-output capability")
	}
	if r.owners == nil {
		return errors.New("equipment ownership lookup is required")
	}
	endpoint, err := r.owners.Endpoint(ctx, value.EndpointID)
	if err != nil {
		return fmt.Errorf("endpoint ownership: %w", err)
	}
	if endpoint.DeviceID != value.DeviceID {
		return errors.New("equipment device does not own endpoint")
	}
	if !domain.SupportsAll(endpoint.Capabilities, value.Capabilities) {
		return errors.New("equipment capabilities exceed endpoint capabilities")
	}
	return nil
}
func clone(value domain.Equipment) domain.Equipment {
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
