// Package devices owns protocol-neutral device and adapter-endpoint identity.
package devices

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
	ErrNotFound = errors.New("registry entry not found")
	ErrExists   = errors.New("registry ID already exists")
	ErrOwned    = errors.New("device still owns adapter endpoints")
)

// DeviceRegistry manages devices and their adapter-owned endpoints.
type DeviceRegistry interface {
	health.Component
	Register(context.Context, domain.Device) (domain.Device, error)
	Update(context.Context, domain.Device) error
	Get(context.Context, domain.DeviceID) (domain.Device, error)
	List(context.Context) ([]domain.Device, error)
	Remove(context.Context, domain.DeviceID) error
	RegisterEndpoint(context.Context, domain.Endpoint) (domain.Endpoint, error)
	Endpoint(context.Context, domain.EndpointID) (domain.Endpoint, error)
}

// Update replaces a device only when all owned endpoint capabilities remain valid.
func (r *Registry) Update(ctx context.Context, device domain.Device) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateDevice(device); err != nil {
		return err
	}
	device = cloneDevice(device)
	r.mu.Lock()
	if _, ok := r.devices[device.ID]; !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	for _, endpoint := range r.endpoints {
		if endpoint.DeviceID == device.ID && !domain.SupportsAll(device.Capabilities, endpoint.Capabilities) {
			r.mu.Unlock()
			return errors.New("device update would orphan endpoint capabilities")
		}
	}
	r.devices[device.ID] = device
	r.mu.Unlock()
	return r.emit(ctx, events.DeviceUpdated, device)
}

// Registry is a concurrency-safe in-memory device and endpoint registry.
type Registry struct {
	mu        sync.RWMutex
	devices   map[domain.DeviceID]domain.Device
	endpoints map[domain.EndpointID]domain.Endpoint
	publisher events.Publisher
	started   bool
}

// NewRegistry constructs an empty registry.
func NewRegistry(publisher events.Publisher) *Registry {
	return &Registry{devices: make(map[domain.DeviceID]domain.Device), endpoints: make(map[domain.EndpointID]domain.Endpoint), publisher: publisher}
}

// Name returns the lifecycle component name.
func (r *Registry) Name() string { return "devices" }

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

// Register validates and stores a defensive device copy.
func (r *Registry) Register(ctx context.Context, device domain.Device) (domain.Device, error) {
	if err := ctx.Err(); err != nil {
		return domain.Device{}, err
	}
	if device.ID == "" {
		id, err := domain.NewDeviceID()
		if err != nil {
			return domain.Device{}, err
		}
		device.ID = id
	}
	if err := validateDevice(device); err != nil {
		return domain.Device{}, err
	}
	device = cloneDevice(device)
	r.mu.Lock()
	if _, ok := r.devices[device.ID]; ok {
		r.mu.Unlock()
		return domain.Device{}, ErrExists
	}
	r.devices[device.ID] = device
	r.mu.Unlock()
	return cloneDevice(device), r.emit(ctx, events.DeviceRegistered, device)
}

// Get returns one immutable device snapshot.
func (r *Registry) Get(ctx context.Context, id domain.DeviceID) (domain.Device, error) {
	if err := ctx.Err(); err != nil {
		return domain.Device{}, err
	}
	r.mu.RLock()
	value, ok := r.devices[id]
	r.mu.RUnlock()
	if !ok {
		return domain.Device{}, ErrNotFound
	}
	return cloneDevice(value), nil
}

// List returns devices in stable ID order.
func (r *Registry) List(ctx context.Context) ([]domain.Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	result := make([]domain.Device, 0, len(r.devices))
	for _, value := range r.devices {
		result = append(result, cloneDevice(value))
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// Remove rejects deletion while adapter endpoints still reference the device.
func (r *Registry) Remove(ctx context.Context, id domain.DeviceID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	if _, ok := r.devices[id]; !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	for _, endpoint := range r.endpoints {
		if endpoint.DeviceID == id {
			r.mu.Unlock()
			return ErrOwned
		}
	}
	delete(r.devices, id)
	r.mu.Unlock()
	return r.emit(ctx, events.DeviceRemoved, map[string]domain.DeviceID{"id": id})
}

// RegisterEndpoint validates ownership and capability subsets atomically.
func (r *Registry) RegisterEndpoint(ctx context.Context, endpoint domain.Endpoint) (domain.Endpoint, error) {
	if err := ctx.Err(); err != nil {
		return domain.Endpoint{}, err
	}
	if endpoint.ID == "" {
		id, err := domain.NewEndpointID()
		if err != nil {
			return domain.Endpoint{}, err
		}
		endpoint.ID = id
	}
	if err := endpoint.ID.Validate(); err != nil {
		return domain.Endpoint{}, fmt.Errorf("endpoint ID: %w", err)
	}
	if endpoint.Name == "" {
		return domain.Endpoint{}, errors.New("endpoint name is required")
	}
	if err := domain.ValidateCapabilities(endpoint.Capabilities); err != nil {
		return domain.Endpoint{}, err
	}
	r.mu.Lock()
	device, owned := r.devices[endpoint.DeviceID]
	if !owned {
		r.mu.Unlock()
		return domain.Endpoint{}, fmt.Errorf("device ownership: %w", ErrNotFound)
	}
	if _, exists := r.endpoints[endpoint.ID]; exists {
		r.mu.Unlock()
		return domain.Endpoint{}, ErrExists
	}
	if !domain.SupportsAll(device.Capabilities, endpoint.Capabilities) {
		r.mu.Unlock()
		return domain.Endpoint{}, errors.New("endpoint capabilities exceed owning device capabilities")
	}
	endpoint = cloneEndpoint(endpoint)
	r.endpoints[endpoint.ID] = endpoint
	r.mu.Unlock()
	return cloneEndpoint(endpoint), nil
}

// Endpoint returns an immutable endpoint snapshot for ownership validation.
func (r *Registry) Endpoint(ctx context.Context, id domain.EndpointID) (domain.Endpoint, error) {
	if err := ctx.Err(); err != nil {
		return domain.Endpoint{}, err
	}
	r.mu.RLock()
	value, ok := r.endpoints[id]
	r.mu.RUnlock()
	if !ok {
		return domain.Endpoint{}, ErrNotFound
	}
	return cloneEndpoint(value), nil
}

func validateDevice(value domain.Device) error {
	if err := value.ID.Validate(); err != nil {
		return fmt.Errorf("device ID: %w", err)
	}
	if value.Name == "" {
		return errors.New("device name is required")
	}
	return domain.ValidateCapabilities(value.Capabilities)
}
func cloneDevice(value domain.Device) domain.Device {
	value.Capabilities = append([]domain.Capability(nil), value.Capabilities...)
	value.Metadata = cloneMetadata(value.Metadata)
	return value
}
func cloneEndpoint(value domain.Endpoint) domain.Endpoint {
	value.Capabilities = append([]domain.Capability(nil), value.Capabilities...)
	return value
}
func cloneMetadata(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
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
