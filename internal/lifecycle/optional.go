package lifecycle

import (
	"context"
	"log/slog"
	"sync"

	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

// Optional prevents an external integration startup failure from rolling back
// authoritative components while preserving the integration's degraded health.
type Optional struct {
	component health.Component
	logger    *slog.Logger
	mu        sync.Mutex
	started   bool
}

// NewOptional wraps a noncritical lifecycle component.
func NewOptional(component health.Component, logger *slog.Logger) *Optional {
	return &Optional{component: component, logger: logger}
}

// Name returns the wrapped component name.
func (o *Optional) Name() string { return o.component.Name() }

// Start attempts startup and records degradation without failing core startup.
func (o *Optional) Start(ctx context.Context) error {
	err := o.component.Start(ctx)
	o.mu.Lock()
	o.started = err == nil
	o.mu.Unlock()
	if err != nil {
		o.logger.Warn("optional component unavailable", "component", o.Name(), "error", err)
	}
	return nil
}

// Stop stops the wrapped component only if it started successfully.
func (o *Optional) Stop(ctx context.Context) error {
	o.mu.Lock()
	started := o.started
	o.started = false
	o.mu.Unlock()
	if !started {
		return nil
	}
	return o.component.Stop(ctx)
}

// Health delegates the integration's actual health.
func (o *Optional) Health() health.Status { return o.component.Health() }
