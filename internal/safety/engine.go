// Package safety owns deterministic operating modes, interlocks, overrides, and watchdog policy.
package safety

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/equipment"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

// Mode describes system-wide command policy.
type Mode string

//nolint:revive // Mode values are documented collectively by Mode.
const (
	ModeNormal        Mode = "normal"
	ModeMaintenance   Mode = "maintenance"
	ModeManual        Mode = "manual"
	ModeEmergencyStop Mode = "emergency-stop"
)

// Stable decision reasons are suitable for APIs, events, and audit logs.
const (
	ReasonAllowed          = "safety.allowed"
	ReasonUnknownEquipment = "safety.unknown_equipment"
	ReasonEmergencyStop    = "safety.emergency_stop"
	ReasonOverrideRequired = "safety.override_required"
	ReasonOverrideExpired  = "safety.override_expired"
	ReasonInputMissing     = "safety.input_missing"
	ReasonInputInvalid     = "safety.input_invalid"
	ReasonInputStale       = "safety.input_stale"
	ReasonInputMismatch    = "safety.input_mismatch"
	ReasonMaximumOn        = "safety.maximum_on"
	ReasonMaximumDailyOn   = "safety.maximum_daily_on"
	ReasonMinimumOff       = "safety.minimum_off"
)

// StateReader is the consumer-owned canonical input boundary.
type StateReader interface {
	Get(context.Context, state.Key) (state.Value, error)
}

// Intent is the minimal command information needed by safety policy.
type Intent struct {
	EquipmentID domain.EquipmentID
	On          bool
	IssuedAt    time.Time
}

// Decision is an auditable policy result.
type Decision struct {
	Allowed    bool              `json:"allowed"`
	Reason     string            `json:"reason"`
	HardLimit  bool              `json:"hardLimit"`
	OverrideID domain.OverrideID `json:"overrideId,omitempty"`
}

// Override authorizes a narrowly scoped temporary bypass of mode restrictions.
// It never bypasses hard limits or hazardous-input validation.
type Override struct {
	ID          domain.OverrideID  `json:"id"`
	EquipmentID domain.EquipmentID `json:"equipmentId"`
	Requester   string             `json:"requester"`
	Reason      string             `json:"reason"`
	CreatedAt   time.Time          `json:"createdAt"`
	ExpiresAt   time.Time          `json:"expiresAt"`
}

// WatchdogAction requests restoration of an equipment profile's safe state.
type WatchdogAction struct {
	EquipmentID domain.EquipmentID `json:"equipmentId"`
	On          bool               `json:"on"`
	Reason      string             `json:"reason"`
	ObservedAt  time.Time          `json:"observedAt"`
}

type runtimeState struct {
	on             bool
	onSince        time.Time
	offSince       time.Time
	dailyOn        time.Duration
	dailyDate      string
	lastAccounted  time.Time
	watchdogReason string
}

// Engine evaluates commands without executing hardware operations. Watchdogs are
// explicitly polled by an owner; the engine starts no goroutines.
type Engine struct {
	mu        sync.RWMutex
	mode      Mode
	profiles  map[domain.EquipmentID]equipment.Profile
	overrides map[domain.EquipmentID]Override
	runtime   map[domain.EquipmentID]runtimeState
	state     StateReader
	logger    *slog.Logger
	now       func() time.Time
	started   bool
}

// NewEngine constructs a policy engine using externally supplied profiles.
func NewEngine(reader StateReader, logger *slog.Logger, now func() time.Time, profiles ...equipment.Profile) (*Engine, error) {
	if reader == nil || logger == nil || now == nil {
		return nil, errors.New("safety state reader, logger, and clock are required")
	}
	engine := &Engine{mode: ModeNormal, profiles: make(map[domain.EquipmentID]equipment.Profile), overrides: make(map[domain.EquipmentID]Override), runtime: make(map[domain.EquipmentID]runtimeState), state: reader, logger: logger, now: now}
	for _, profile := range profiles {
		if err := profile.Validate(); err != nil {
			return nil, fmt.Errorf("profile: %w", err)
		}
		if _, exists := engine.profiles[profile.EquipmentID]; exists {
			return nil, errors.New("duplicate equipment safety profile")
		}
		engine.profiles[profile.EquipmentID] = profile.Clone()
	}
	return engine, nil
}

// Name returns the lifecycle component name.
func (e *Engine) Name() string { return "safety" }

// Start marks the engine available.
func (e *Engine) Start(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.started = true
	return nil
}

// Stop marks the engine unavailable without discarding policy state.
func (e *Engine) Stop(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.started = false
	return nil
}

// Health reports policy-engine lifecycle state.
func (e *Engine) Health() health.Status {
	e.mu.RLock()
	started := e.started
	e.mu.RUnlock()
	condition := health.StateUnhealthy
	if started {
		condition = health.StateHealthy
	}
	return health.NewStatus(e.Name(), condition, "", e.now().UTC())
}

// SetMode changes system policy. Mode transitions never modify hard limits.
func (e *Engine) SetMode(ctx context.Context, mode Mode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if mode != ModeNormal && mode != ModeMaintenance && mode != ModeManual && mode != ModeEmergencyStop {
		return errors.New("unsupported safety mode")
	}
	e.mu.Lock()
	e.mode = mode
	e.mu.Unlock()
	e.logger.InfoContext(ctx, "safety mode changed", "code", "safety.mode_changed", "mode", mode, "correlation_id", events.CorrelationIDFromContext(ctx))
	return nil
}

// Mode returns the current system mode.
func (e *Engine) Mode() Mode { e.mu.RLock(); defer e.mu.RUnlock(); return e.mode }

// GrantOverride validates and stores a temporary, reasoned override.
func (e *Engine) GrantOverride(ctx context.Context, override Override) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := override.ID.Validate(); err != nil {
		return err
	}
	if override.Reason == "" || override.Requester == "" {
		return errors.New("override requester and reason are required")
	}
	if override.CreatedAt.IsZero() {
		override.CreatedAt = e.now().UTC()
	}
	if !override.ExpiresAt.After(override.CreatedAt) {
		return errors.New("override expiry must follow creation")
	}
	e.mu.Lock()
	if _, ok := e.profiles[override.EquipmentID]; !ok {
		e.mu.Unlock()
		return errors.New(ReasonUnknownEquipment)
	}
	e.overrides[override.EquipmentID] = override
	e.mu.Unlock()
	e.logger.WarnContext(ctx, "safety override granted", "code", "safety.override_granted", "override_id", override.ID, "equipment_id", override.EquipmentID, "requester", override.Requester, "reason", override.Reason, "expires_at", override.ExpiresAt, "correlation_id", events.CorrelationIDFromContext(ctx))
	return nil
}

// RevokeOverride removes a scoped override.
func (e *Engine) RevokeOverride(ctx context.Context, equipmentID domain.EquipmentID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	delete(e.overrides, equipmentID)
	e.mu.Unlock()
	e.logger.WarnContext(ctx, "safety override revoked", "code", "safety.override_revoked", "equipment_id", equipmentID, "correlation_id", events.CorrelationIDFromContext(ctx))
	return nil
}

// Evaluate applies mode restrictions, hard limits, and hazardous-input checks.
func (e *Engine) Evaluate(ctx context.Context, intent Intent) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	now := intent.IssuedAt
	if now.IsZero() {
		now = e.now().UTC()
	}
	e.mu.Lock()
	profile, ok := e.profiles[intent.EquipmentID]
	if !ok {
		e.mu.Unlock()
		return Decision{Reason: ReasonUnknownEquipment}, nil
	}
	runtime := e.accountLocked(intent.EquipmentID, now)
	mode := e.mode
	override, hasOverride := e.overrides[intent.EquipmentID]
	if hasOverride && !now.Before(override.ExpiresAt) {
		delete(e.overrides, intent.EquipmentID)
		hasOverride = false
	}
	e.mu.Unlock()
	if !intent.On {
		return Decision{Allowed: true, Reason: ReasonAllowed}, nil
	}
	if mode == ModeEmergencyStop {
		return Decision{Reason: ReasonEmergencyStop, HardLimit: true}, nil
	}
	if runtime.on && profile.Limits.MaximumOn > 0 && now.Sub(runtime.onSince) >= profile.Limits.MaximumOn {
		return Decision{Reason: ReasonMaximumOn, HardLimit: true}, nil
	}
	if profile.Limits.MaximumDailyOn > 0 && runtime.dailyOn >= profile.Limits.MaximumDailyOn {
		return Decision{Reason: ReasonMaximumDailyOn, HardLimit: true}, nil
	}
	if !runtime.offSince.IsZero() && profile.Limits.MinimumOff > 0 && now.Sub(runtime.offSince) < profile.Limits.MinimumOff {
		return Decision{Reason: ReasonMinimumOff, HardLimit: true}, nil
	}
	if profile.Hazardous {
		if decision, err := e.validateInputs(ctx, profile); err != nil || !decision.Allowed {
			return decision, err
		}
	}
	if (mode == ModeMaintenance || mode == ModeManual) && !hasOverride {
		return Decision{Reason: ReasonOverrideRequired}, nil
	}
	decision := Decision{Allowed: true, Reason: ReasonAllowed}
	if hasOverride {
		decision.OverrideID = override.ID
	}
	return decision, nil
}

func (e *Engine) validateInputs(ctx context.Context, profile equipment.Profile) (Decision, error) {
	for _, requirement := range profile.RequiredInputs {
		value, err := e.state.Get(ctx, requirement.Key)
		if err != nil {
			if errors.Is(err, state.ErrNotFound) {
				return Decision{Reason: ReasonInputMissing, HardLimit: true}, nil
			}
			return Decision{}, err
		}
		switch value.Quality {
		case domain.QualityStale:
			return Decision{Reason: ReasonInputStale, HardLimit: true}, nil
		case domain.QualityGood:
		default:
			return Decision{Reason: ReasonInputInvalid, HardLimit: true}, nil
		}
		if requirement.RequireBoolean != nil && (value.Value.Boolean == nil || *value.Value.Boolean != *requirement.RequireBoolean) {
			return Decision{Reason: ReasonInputMismatch, HardLimit: true}, nil
		}
	}
	return Decision{Allowed: true, Reason: ReasonAllowed}, nil
}

// RecordReported records reconciled physical state for hard-limit accounting.
func (e *Engine) RecordReported(ctx context.Context, equipmentID domain.EquipmentID, on bool, observedAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if observedAt.IsZero() {
		return errors.New("reported-state timestamp is required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.profiles[equipmentID]; !ok {
		return errors.New(ReasonUnknownEquipment)
	}
	runtime := e.accountLocked(equipmentID, observedAt)
	if runtime.on != on {
		runtime.on = on
		if on {
			runtime.onSince = observedAt
			runtime.lastAccounted = observedAt
		} else {
			runtime.offSince = observedAt
			runtime.onSince = time.Time{}
			runtime.lastAccounted = time.Time{}
		}
		runtime.watchdogReason = ""
	}
	e.runtime[equipmentID] = runtime
	return nil
}

// CheckWatchdogs returns deterministic safe-state actions and expires overrides.
func (e *Engine) CheckWatchdogs(ctx context.Context) ([]WatchdogAction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := e.now().UTC()
	e.mu.Lock()
	actions := make([]WatchdogAction, 0)
	for id, override := range e.overrides {
		if !now.Before(override.ExpiresAt) {
			delete(e.overrides, id)
			runtime := e.accountLocked(id, now)
			if runtime.on {
				profile := e.profiles[id]
				if runtime.watchdogReason == "" {
					runtime.watchdogReason = ReasonOverrideExpired
					e.runtime[id] = runtime
					actions = append(actions, WatchdogAction{EquipmentID: id, On: profile.FailSafeOn, Reason: ReasonOverrideExpired, ObservedAt: now})
				}
			}
		}
	}
	for id, profile := range e.profiles {
		runtime := e.accountLocked(id, now)
		if runtime.watchdogReason != "" {
			continue
		}
		if runtime.on && profile.Limits.MaximumOn > 0 && now.Sub(runtime.onSince) >= profile.Limits.MaximumOn {
			runtime.watchdogReason = ReasonMaximumOn
			e.runtime[id] = runtime
			actions = append(actions, WatchdogAction{EquipmentID: id, On: profile.FailSafeOn, Reason: ReasonMaximumOn, ObservedAt: now})
		} else if runtime.on && profile.Limits.MaximumDailyOn > 0 && runtime.dailyOn >= profile.Limits.MaximumDailyOn {
			runtime.watchdogReason = ReasonMaximumDailyOn
			e.runtime[id] = runtime
			actions = append(actions, WatchdogAction{EquipmentID: id, On: profile.FailSafeOn, Reason: ReasonMaximumDailyOn, ObservedAt: now})
		}
	}
	e.mu.Unlock()
	for _, action := range actions {
		e.logger.ErrorContext(ctx, "safety watchdog tripped", "code", action.Reason, "equipment_id", action.EquipmentID, "safe_on", action.On, "correlation_id", events.CorrelationIDFromContext(ctx))
	}
	return actions, nil
}

func (e *Engine) accountLocked(id domain.EquipmentID, at time.Time) runtimeState {
	runtime := e.runtime[id]
	date := at.UTC().Format("2006-01-02")
	if runtime.dailyDate != date {
		runtime.dailyDate = date
		runtime.dailyOn = 0
		if runtime.on {
			runtime.lastAccounted = at
		}
	}
	if runtime.on && !runtime.lastAccounted.IsZero() && at.After(runtime.lastAccounted) {
		runtime.dailyOn += at.Sub(runtime.lastAccounted)
		runtime.lastAccounted = at
	}
	e.runtime[id] = runtime
	return runtime
}
