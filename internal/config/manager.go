package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

// Loader abstracts the configuration source so tests and files can use the same manager.
type Loader interface {
	Load(context.Context) (Config, error)
}

// Change describes one planned configuration difference.
type Change struct {
	Path       string `json:"path"`
	Reloadable bool   `json:"reloadable"`
	Reason     string `json:"reason"`
}

// ReloadPlan is the complete, pre-validated set of changes for one reload.
type ReloadPlan struct {
	Digest  string   `json:"digest"`
	Changes []Change `json:"changes"`
}

// ReloadRejectedError explains every unsafe configuration change.
type ReloadRejectedError struct {
	Changes []Change
}

// Error reports that a restart is required without exposing configuration values.
func (e *ReloadRejectedError) Error() string {
	return fmt.Sprintf("configuration reload rejected: %d change(s) require restart", len(e.Changes))
}

// ConfigurationManager owns the current immutable configuration snapshot.
type ConfigurationManager interface {
	health.Component
	Current() Config
	Digest() string
	Plan(Config) (ReloadPlan, error)
	Reload(context.Context) error
}

// LogLevelSetter applies the only currently supported harmless hot reload.
type LogLevelSetter interface {
	SetLogLevel(string)
}

// Manager atomically owns and reloads an immutable configuration snapshot.
type Manager struct {
	mu        sync.RWMutex
	reloadMu  sync.Mutex
	current   Config
	digest    string
	loader    Loader
	publisher events.Publisher
	logger    *slog.Logger
	started   bool
	now       func() time.Time
	levels    LogLevelSetter
}

// ManagerOption customizes deterministic manager dependencies.
type ManagerOption func(*Manager)

// WithClock injects the clock used for configuration health timestamps.
func WithClock(now func() time.Time) ManagerOption {
	return func(manager *Manager) {
		if now != nil {
			manager.now = now
		}
	}
}

// WithLogLevelSetter connects configuration activation to the live logger.
func WithLogLevelSetter(setter LogLevelSetter) ManagerOption {
	return func(manager *Manager) { manager.levels = setter }
}

// NewManager constructs a manager with an already validated initial snapshot.
func NewManager(initial Config, loader Loader, publisher events.Publisher, logger *slog.Logger, options ...ManagerOption) *Manager {
	digest, _ := initial.Digest()
	manager := &Manager{current: initial.Clone(), digest: digest, loader: loader, publisher: publisher, logger: logger, now: time.Now}
	for _, option := range options {
		option(manager)
	}
	return manager
}

// Name returns the lifecycle component name.
func (m *Manager) Name() string { return "configuration" }

// Start marks the configuration manager ready.
func (m *Manager) Start(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return nil
}

// Stop marks the configuration manager unavailable.
func (m *Manager) Stop(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	return nil
}

// Health reports configuration manager lifecycle health.
func (m *Manager) Health() health.Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state := health.StateUnhealthy
	if m.started {
		state = health.StateHealthy
	}
	return health.NewStatus(m.Name(), state, "", m.now().UTC())
}

// Current returns a deep copy of the current configuration snapshot.
func (m *Manager) Current() Config { m.mu.RLock(); defer m.mu.RUnlock(); return m.current.Clone() }

// Digest returns the active redacted configuration digest.
func (m *Manager) Digest() string { m.mu.RLock(); defer m.mu.RUnlock(); return m.digest }

// Plan validates a candidate and rejects every change except the explicitly
// harmless application log-level setting. Rejected plans contain no values.
func (m *Manager) Plan(next Config) (ReloadPlan, error) {
	if err := next.Validate(); err != nil {
		return ReloadPlan{}, err
	}
	digest, err := next.Digest()
	if err != nil {
		return ReloadPlan{}, err
	}
	current := m.Current()
	changes := changesBetween(current, next)
	unsafe := make([]Change, 0)
	for _, change := range changes {
		if !change.Reloadable {
			unsafe = append(unsafe, change)
		}
	}
	plan := ReloadPlan{Digest: digest, Changes: changes}
	if len(unsafe) != 0 {
		return plan, &ReloadRejectedError{Changes: unsafe}
	}
	return plan, nil
}

// Reload validates, plans, audits, and commits a candidate as one serialized
// operation. Any failure leaves the previous snapshot and digest unchanged.
func (m *Manager) Reload(ctx context.Context) error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	if m.loader == nil {
		return errors.New("configuration reload is not available")
	}
	next, err := m.loader.Load(ctx)
	if err != nil {
		m.logger.Error("configuration reload failed", "error", err)
		return err
	}
	plan, err := m.Plan(next)
	if err != nil {
		m.logger.Warn("configuration reload rejected", "error", err)
		return err
	}
	if len(plan.Changes) != 0 && m.levels == nil {
		return errors.New("configuration reload rejected: live log-level setter is unavailable")
	}
	previous := m.Current()
	previousDigest := m.Digest()
	event, err := events.New(m.Name(), events.ConfigurationChanged, events.SeverityInfo, map[string]any{"digest": plan.Digest, "changes": plan.Changes}, events.CorrelationIDFromContext(ctx))
	if err != nil {
		return fmt.Errorf("create configuration reload event: %w", err)
	}
	if len(plan.Changes) != 0 {
		m.levels.SetLogLevel(next.Application.LogLevel)
	}
	m.mu.Lock()
	m.current = next.Clone()
	m.digest = plan.Digest
	m.mu.Unlock()
	if m.publisher != nil {
		if err := m.publisher.Publish(ctx, event); err != nil {
			if len(plan.Changes) != 0 {
				m.levels.SetLogLevel(previous.Application.LogLevel)
			}
			m.mu.Lock()
			m.current = previous
			m.digest = previousDigest
			m.mu.Unlock()
			m.logger.Error("configuration reload audit failed; activation rolled back", "error", err)
			return fmt.Errorf("publish configuration reload event: %w", err)
		}
	}
	m.logger.Info("configuration reloaded", "digest", plan.Digest, "change_count", len(plan.Changes))
	return nil
}

func changesBetween(current, next Config) []Change {
	changes := make([]Change, 0)
	add := func(path string, reloadable bool, reason string, before, after any) {
		if !reflect.DeepEqual(before, after) {
			changes = append(changes, Change{Path: path, Reloadable: reloadable, Reason: reason})
		}
	}
	add("schema_version", false, "schema changes require migration and restart", current.SchemaVersion, next.SchemaVersion)
	add("application.log_level", true, "logging verbosity is operationally harmless", current.Application.LogLevel, next.Application.LogLevel)
	add("application.startup_timeout", false, "lifecycle bounds are fixed at startup", current.Application.StartupTimeout, next.Application.StartupTimeout)
	add("application.shutdown_timeout", false, "lifecycle bounds are fixed at startup", current.Application.ShutdownTimeout, next.Application.ShutdownTimeout)
	add("application.component_timeout", false, "lifecycle bounds are fixed at startup", current.Application.ComponentTimeout, next.Application.ComponentTimeout)
	add("http", false, "HTTP listeners and timeouts require restart", current.HTTP, next.HTTP)
	add("mqtt", false, "MQTT connection changes require restart", current.MQTT.Redacted(), next.MQTT.Redacted())
	add("simulator", false, "adapter activation requires restart", current.Simulator, next.Simulator)
	add("adapters", false, "direct adapter identity, safety, and connection changes require restart", current.Adapters, next.Adapters)
	add("bench", false, "bench activation guard changes require restart", current.Bench, next.Bench)
	add("inventory", false, "inventory changes require validated registry activation", current.Inventory, next.Inventory)
	return changes
}

// Redacted returns MQTT configuration safe for comparisons and diagnostics.
func (m MQTT) Redacted() MQTT {
	if m.Password != "" {
		m.Password = redactedSecret
	}
	return m
}

// FileLoader reloads the same layered file-and-environment configuration used during bootstrap.
type FileLoader struct{ Path string }

// Load reads and validates FileLoader.Path unless ctx is cancelled.
func (l FileLoader) Load(ctx context.Context) (Config, error) {
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}
	return Load(l.Path)
}
