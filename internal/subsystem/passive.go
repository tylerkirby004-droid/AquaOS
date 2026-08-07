// Package subsystem supplies a minimal lifecycle implementation for architectural
// placeholders. It can be replaced by domain managers without changing bootstrap.
package subsystem

import (
	"context"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

// Passive is a lifecycle-only placeholder for a future subsystem.
type Passive struct {
	name    string
	mu      sync.RWMutex
	started bool
}

// NewPassive constructs a stopped placeholder with the supplied name.
func NewPassive(name string) *Passive { return &Passive{name: name} }

// Name returns the configured component name.
func (p *Passive) Name() string { return p.name }

// Start marks the placeholder healthy.
func (p *Passive) Start(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = true
	return nil
}

// Stop marks the placeholder unhealthy.
func (p *Passive) Stop(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.started = false
	return nil
}

// Health reports placeholder lifecycle health.
func (p *Passive) Health() health.Status {
	p.mu.RLock()
	defer p.mu.RUnlock()
	state := health.StateUnhealthy
	if p.started {
		state = health.StateHealthy
	}
	return health.NewStatus(p.name, state, "", time.Now().UTC())
}
