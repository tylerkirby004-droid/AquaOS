// Package lifecycle starts components in dependency order and stops them in
// reverse order with explicit bounds. Failed startup is rolled back so partial
// resources and workers do not survive bootstrap failure.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

// Timeouts bounds aggregate startup/shutdown and each component operation.
type Timeouts struct {
	Startup   time.Duration
	Shutdown  time.Duration
	Component time.Duration
}

// Manager coordinates ordered component startup and reverse-order shutdown.
type Manager struct {
	logger     *slog.Logger
	components []health.Component
	timeouts   Timeouts
	mu         sync.Mutex
	started    []startedComponent
}

type startedComponent struct {
	component health.Component
	cancel    context.CancelFunc
}

// New constructs a manager with conservative default lifecycle bounds.
func New(logger *slog.Logger, components ...health.Component) *Manager {
	return NewConfigured(logger, Timeouts{Startup: 30 * time.Second, Shutdown: 15 * time.Second, Component: 5 * time.Second}, components...)
}

// NewConfigured constructs a manager with externally configured bounds.
func NewConfigured(logger *slog.Logger, timeouts Timeouts, components ...health.Component) *Manager {
	return &Manager{logger: logger, components: append([]health.Component(nil), components...), timeouts: timeouts}
}

// Start starts components in order and rolls back a partial startup within the
// configured shutdown bound.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.started) != 0 {
		return errors.New("lifecycle already started")
	}
	startupDeadline := time.NewTimer(m.timeouts.Startup)
	defer startupDeadline.Stop()
	for _, component := range m.components {
		m.logger.Info("starting component", "component", component.Name())
		componentCtx, cancelComponent := context.WithCancel(ctx)
		result := make(chan error, 1)
		// The start goroutine is bounded by the component context and joined by
		// receiving result on every select path; it never outlives Start.
		go func() { result <- component.Start(componentCtx) }()
		componentDeadline := time.NewTimer(m.timeouts.Component)
		var err error
		select {
		case err = <-result:
		case <-ctx.Done():
			cancelComponent()
			err = errors.Join(ctx.Err(), <-result)
		case <-startupDeadline.C:
			cancelComponent()
			err = errors.Join(context.DeadlineExceeded, <-result)
		case <-componentDeadline.C:
			cancelComponent()
			err = errors.Join(context.DeadlineExceeded, <-result)
		}
		componentDeadline.Stop()
		if err != nil {
			cancelComponent()
			rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), m.timeouts.Shutdown)
			rollbackErr := m.stopLocked(rollbackCtx)
			cancelRollback()
			return errors.Join(fmt.Errorf("start %s: %w", component.Name(), err), rollbackErr)
		}
		m.started = append(m.started, startedComponent{component: component, cancel: cancelComponent})
	}
	return nil
}

// Stop stops started components in reverse order within configured bounds.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	shutdownCtx, cancelShutdown := context.WithTimeout(ctx, m.timeouts.Shutdown)
	defer cancelShutdown()
	return m.stopLocked(shutdownCtx)
}

func (m *Manager) stopLocked(ctx context.Context) error {
	var errs []error
	for i := len(m.started) - 1; i >= 0; i-- {
		started := m.started[i]
		component := started.component
		m.logger.Info("stopping component", "component", component.Name())
		started.cancel()
		componentCtx, cancelComponent := context.WithTimeout(ctx, m.timeouts.Component)
		err := component.Stop(componentCtx)
		cancelComponent()
		if err != nil {
			errs = append(errs, fmt.Errorf("stop %s: %w", component.Name(), err))
		}
	}
	m.started = nil
	return errors.Join(errs...)
}
