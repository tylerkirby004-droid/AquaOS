// Package simulator provides a hardware-incapable lifecycle adapter for safe
// foundation verification. The deterministic tank simulator belongs to a later
// milestone and is intentionally not implemented here.
package simulator

import (
	"context"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

// Adapter proves adapter lifecycle wiring without exposing hardware commands.
type Adapter struct {
	mu      sync.RWMutex
	started bool
}

// New constructs a stopped, hardware-incapable simulator adapter.
func New() *Adapter { return &Adapter{} }

// Name returns the lifecycle component name.
func (a *Adapter) Name() string { return "simulator" }

// Start marks the adapter healthy and starts no goroutines.
func (a *Adapter) Start(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.started = true
	return nil
}

// Stop marks the adapter stopped.
func (a *Adapter) Stop(context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.started = false
	return nil
}

// Health reports simulator lifecycle state.
func (a *Adapter) Health() health.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	state := health.StateUnhealthy
	if a.started {
		state = health.StateHealthy
	}
	return health.NewStatus(a.Name(), state, "", time.Now().UTC())
}
