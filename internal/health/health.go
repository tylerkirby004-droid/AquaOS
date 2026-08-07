// Package health defines component and aggregate operational health.
package health

import (
	"context"
	"sync"
	"time"
)

// State is the severity of a component or aggregate health assessment.
type State string

const (
	// StateHealthy means the component is operating normally.
	StateHealthy State = "healthy"
	// StateDegraded means useful service continues with reduced capability.
	StateDegraded State = "degraded"
	// StateUnhealthy means the component cannot provide its required capability.
	StateUnhealthy State = "unhealthy"
)

// Status is the health state of a component at a point in time. Healthy is
// retained as a compatibility field and always reflects StateHealthy.
type Status struct {
	Name      string    `json:"name"`
	State     State     `json:"state"`
	Healthy   bool      `json:"healthy"`
	Message   string    `json:"message,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

// NewStatus constructs a consistent component status.
func NewStatus(name string, state State, message string, checkedAt time.Time) Status {
	return Status{Name: name, State: state, Healthy: state == StateHealthy, Message: message, CheckedAt: checkedAt}
}

// Component is the lifecycle contract implemented by every long-running subsystem.
type Component interface {
	Name() string
	Start(context.Context) error
	Stop(context.Context) error
	Health() Status
}

type registration struct {
	component Component
	required  bool
}

// Report distinguishes process liveness, required-component readiness, and
// aggregate degradation in one stable snapshot.
type Report struct {
	Live       bool     `json:"live"`
	Ready      bool     `json:"ready"`
	State      State    `json:"state"`
	Components []Status `json:"components"`
}

// Manager aggregates component health without coupling subsystems together.
type Manager struct {
	mu         sync.RWMutex
	components []registration
	started    bool
	now        func() time.Time
}

// Option customizes deterministic health-manager dependencies.
type Option func(*Manager)

// WithClock injects the clock used by manager-owned health timestamps.
func WithClock(now func() time.Time) Option {
	return func(manager *Manager) {
		if now != nil {
			manager.now = now
		}
	}
}

// NewManager constructs an empty health aggregator.
func NewManager(options ...Option) *Manager {
	manager := &Manager{now: time.Now}
	for _, option := range options {
		option(manager)
	}
	return manager
}

// Name returns the lifecycle component name.
func (m *Manager) Name() string { return "health" }

// Register adds required components to future health snapshots.
func (m *Manager) Register(components ...Component) {
	for _, component := range components {
		m.RegisterComponent(component, true)
	}
}

// RegisterComponent adds a component and declares whether its failure blocks readiness.
func (m *Manager) RegisterComponent(component Component, required bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.components = append(m.components, registration{component: component, required: required})
}

// Start enables readiness reporting.
func (m *Manager) Start(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return nil
}

// Stop disables readiness reporting.
func (m *Manager) Stop(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	return nil
}

// Health reports the health manager's lifecycle state.
func (m *Manager) Health() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := StateUnhealthy
	if m.started {
		state = StateHealthy
	}
	return NewStatus(m.Name(), state, "", m.now().UTC())
}

// Snapshot returns a stable component list suitable for serialization.
func (m *Manager) Snapshot() []Status { return m.Report().Components }

// Report returns a consistent aggregate assessment. Optional component failure
// degrades the service but does not make required local control unready.
func (m *Manager) Report() Report {
	m.mu.RLock()
	components := append([]registration(nil), m.components...)
	started := m.started
	m.mu.RUnlock()
	statuses := make([]Status, 0, len(components)+1)
	statuses = append(statuses, m.Health())
	state := StateHealthy
	ready := started
	for _, registered := range components {
		status := normalize(registered.component.Health())
		statuses = append(statuses, status)
		if status.State != StateHealthy {
			if registered.required {
				ready = false
				state = StateUnhealthy
			} else if state == StateHealthy {
				state = StateDegraded
			}
		}
	}
	if !started {
		state = StateUnhealthy
	}
	return Report{Live: true, Ready: ready, State: state, Components: statuses}
}

// Healthy reports whether all required components are ready.
func (m *Manager) Healthy() bool { return m.Report().Ready }

func normalize(status Status) Status {
	if status.State == "" {
		if status.Healthy {
			status.State = StateHealthy
		} else {
			status.State = StateUnhealthy
		}
	}
	status.Healthy = status.State == StateHealthy
	return status
}
