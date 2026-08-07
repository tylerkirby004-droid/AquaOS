// Package alarms implements deterministic alarm lifecycle policy without aquarium-specific rules.
package alarms

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

// Status describes one alarm instance.
type Status string

//nolint:revive // Status values are documented collectively by Status.
const (
	StatusActive       Status = "active"
	StatusAcknowledged Status = "acknowledged"
	StatusCleared      Status = "cleared"
)

// Subject identifies the generic entity affected by a rule.
type Subject struct {
	Kind string          `json:"kind"`
	ID   domain.EntityID `json:"id"`
}

// Evidence is an immutable observation explanation suitable for audit output.
type Evidence struct {
	Code     string            `json:"code"`
	Message  string            `json:"message"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Rule configures generic temporal alarm behavior. Hysteresis is the continuous
// healthy interval required before clear; it avoids threshold semantics in this core package.
type Rule struct {
	ID         domain.RuleID   `json:"id"`
	Code       string          `json:"code"`
	Name       string          `json:"name"`
	Subject    Subject         `json:"subject"`
	Severity   events.Severity `json:"severity"`
	Debounce   time.Duration   `json:"debounce"`
	Hysteresis time.Duration   `json:"hysteresis"`
	Latching   bool            `json:"latching"`
}

// Observation reports whether a rule's underlying condition exists at a specific time.
type Observation struct {
	RuleID        domain.RuleID        `json:"ruleId"`
	Active        bool                 `json:"active"`
	Severity      events.Severity      `json:"severity,omitempty"`
	ObservedAt    time.Time            `json:"observedAt"`
	Evidence      Evidence             `json:"evidence"`
	CorrelationID domain.CorrelationID `json:"correlationId"`
}

// Alarm is the auditable state for one condition episode.
type Alarm struct {
	ID              domain.AlarmID       `json:"id"`
	RuleID          domain.RuleID        `json:"ruleId"`
	Code            string               `json:"code"`
	Subject         Subject              `json:"subject"`
	Severity        events.Severity      `json:"severity"`
	Status          Status               `json:"status"`
	ConditionActive bool                 `json:"conditionActive"`
	Evidence        []Evidence           `json:"evidence"`
	FirstObserved   time.Time            `json:"firstObserved"`
	LastObserved    time.Time            `json:"lastObserved"`
	OccurrenceCount uint64               `json:"occurrenceCount"`
	AcknowledgedAt  *time.Time           `json:"acknowledgedAt,omitempty"`
	ClearedAt       *time.Time           `json:"clearedAt,omitempty"`
	CorrelationID   domain.CorrelationID `json:"correlationId"`
}

// Transition is a stable lifecycle result code.
type Transition string

//nolint:revive // Transition codes are documented collectively by Transition.
const (
	TransitionNone         Transition = "alarm.no_change"
	TransitionDebouncing   Transition = "alarm.debouncing"
	TransitionRaised       Transition = "alarm.raised"
	TransitionEscalated    Transition = "alarm.escalated"
	TransitionClearPending Transition = "alarm.clear_pending"
	TransitionCleared      Transition = "alarm.cleared"
	TransitionAcknowledged Transition = "alarm.acknowledged"
	TransitionIgnoredStale Transition = "alarm.ignored_stale"
)

// Stable alarm errors allow callers to use errors.Is without parsing text.
var (
	ErrNotFound          = errors.New("alarm or rule not found")
	ErrDuplicate         = errors.New("alarm or rule already exists")
	ErrInvalidTransition = errors.New("invalid alarm transition")
	ErrConditionActive   = errors.New("underlying alarm condition remains active")
)

// AlarmManager exposes generic rule evaluation and lifecycle operations.
type AlarmManager interface {
	health.Component
	RegisterRule(context.Context, Rule) error
	Observe(context.Context, Observation) (Alarm, Transition, error)
	Acknowledge(context.Context, domain.AlarmID) (Alarm, error)
	Clear(context.Context, domain.AlarmID) (Alarm, error)
	Get(context.Context, domain.AlarmID) (Alarm, error)
	List(context.Context, Status) ([]Alarm, error)
}

type ruleState struct {
	rule            Rule
	pendingActive   *time.Time
	pendingClear    *time.Time
	lastObservation time.Time
	current         domain.AlarmID
}

// Manager is a concurrency-safe single-node alarm engine. State is deliberately
// keyed by stable IDs so a durable implementation can replace it for clustering later.
type Manager struct {
	mu            sync.RWMutex
	rules         map[domain.RuleID]*ruleState
	alarms        map[domain.AlarmID]Alarm
	publisher     events.Publisher
	factory       *events.Factory
	logger        *slog.Logger
	now           func() time.Time
	newAlarmID    func() (domain.AlarmID, error)
	evidenceLimit int
	started       bool
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

// NewManager constructs an engine with production dependencies and bounded evidence history.
func NewManager(p events.Publisher, logger *slog.Logger) *Manager {
	clock := wallClock{}
	factory, _ := events.NewFactory(clock, domain.NewCorrelationID)
	return NewManagerWithDependencies(p, logger, clock.Now, domain.NewAlarmID, factory, 16)
}

// NewManagerWithDependencies constructs a deterministic engine for tests and adapters.
func NewManagerWithDependencies(p events.Publisher, logger *slog.Logger, now func() time.Time, newAlarmID func() (domain.AlarmID, error), factory *events.Factory, evidenceLimit int) *Manager {
	if evidenceLimit < 1 {
		evidenceLimit = 1
	}
	return &Manager{rules: make(map[domain.RuleID]*ruleState), alarms: make(map[domain.AlarmID]Alarm), publisher: p, logger: logger, now: now, newAlarmID: newAlarmID, factory: factory, evidenceLimit: evidenceLimit}
}

// Name returns the lifecycle component name.
func (m *Manager) Name() string { return "alarms" }

// Start marks the manager ready for health reporting.
func (m *Manager) Start(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return nil
}

// Stop marks the manager unavailable without discarding alarm history.
func (m *Manager) Stop(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	return nil
}

// Health reports manager lifecycle state using the injected clock.
func (m *Manager) Health() health.Status {
	m.mu.RLock()
	started := m.started
	m.mu.RUnlock()
	state := health.StateUnhealthy
	if started {
		state = health.StateHealthy
	}
	return health.NewStatus(m.Name(), state, "", m.now().UTC())
}

// RegisterRule validates and stores an immutable rule.
func (m *Manager) RegisterRule(ctx context.Context, rule Rule) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRule(rule); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.rules[rule.ID]; exists {
		return ErrDuplicate
	}
	m.rules[rule.ID] = &ruleState{rule: rule}
	return nil
}

// Observe applies one ordered observation. Equal or older timestamps are ignored,
// making duplicate delivery safe. Transitions depend only on observation time.
func (m *Manager) Observe(ctx context.Context, observation Observation) (Alarm, Transition, error) {
	if err := ctx.Err(); err != nil {
		return Alarm{}, TransitionNone, err
	}
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = m.now().UTC()
	}
	if observation.CorrelationID == "" {
		id, err := domain.NewCorrelationID()
		if err != nil {
			return Alarm{}, TransitionNone, err
		}
		observation.CorrelationID = id
	}
	m.mu.Lock()
	state, ok := m.rules[observation.RuleID]
	if !ok {
		m.mu.Unlock()
		return Alarm{}, TransitionNone, ErrNotFound
	}
	if !state.lastObservation.IsZero() && !observation.ObservedAt.After(state.lastObservation) {
		alarm := cloneAlarm(m.alarms[state.current])
		m.mu.Unlock()
		return alarm, TransitionIgnoredStale, nil
	}
	state.lastObservation = observation.ObservedAt
	alarm, transition, err := m.applyObservation(state, observation)
	if alarm.ID != "" {
		m.alarms[alarm.ID] = cloneAlarm(alarm)
	}
	m.mu.Unlock()
	if err != nil || transition == TransitionNone || transition == TransitionDebouncing || transition == TransitionClearPending {
		return cloneAlarm(alarm), transition, err
	}
	if emitErr := m.emit(ctx, transition, alarm); emitErr != nil {
		return cloneAlarm(alarm), transition, emitErr
	}
	return cloneAlarm(alarm), transition, nil
}

func (m *Manager) applyObservation(state *ruleState, o Observation) (Alarm, Transition, error) {
	if o.Active {
		state.pendingClear = nil
		if state.current == "" || m.alarms[state.current].Status == StatusCleared {
			if state.pendingActive == nil {
				at := o.ObservedAt
				state.pendingActive = &at
			}
			if o.ObservedAt.Sub(*state.pendingActive) < state.rule.Debounce {
				return Alarm{}, TransitionDebouncing, nil
			}
			id, err := m.newAlarmID()
			if err != nil {
				return Alarm{}, TransitionNone, err
			}
			severity := state.rule.Severity
			if o.Severity.Rank() > severity.Rank() {
				severity = o.Severity
			}
			alarm := Alarm{ID: id, RuleID: state.rule.ID, Code: state.rule.Code, Subject: state.rule.Subject, Severity: severity, Status: StatusActive, ConditionActive: true, Evidence: appendEvidence(nil, o.Evidence, m.evidenceLimit), FirstObserved: *state.pendingActive, LastObserved: o.ObservedAt, OccurrenceCount: 1, CorrelationID: o.CorrelationID}
			state.current = id
			state.pendingActive = nil
			return alarm, TransitionRaised, nil
		}
		alarm := m.alarms[state.current]
		alarm.ConditionActive = true
		alarm.LastObserved = o.ObservedAt
		alarm.OccurrenceCount++
		alarm.Evidence = appendEvidence(alarm.Evidence, o.Evidence, m.evidenceLimit)
		alarm.CorrelationID = o.CorrelationID
		if o.Severity.Rank() > alarm.Severity.Rank() {
			alarm.Severity = o.Severity
			return alarm, TransitionEscalated, nil
		}
		return alarm, TransitionNone, nil
	}
	state.pendingActive = nil
	if state.current == "" {
		return Alarm{}, TransitionNone, nil
	}
	alarm := m.alarms[state.current]
	if alarm.Status == StatusCleared {
		return alarm, TransitionNone, nil
	}
	alarm.ConditionActive = false
	alarm.LastObserved = o.ObservedAt
	alarm.Evidence = appendEvidence(alarm.Evidence, o.Evidence, m.evidenceLimit)
	alarm.CorrelationID = o.CorrelationID
	if state.pendingClear == nil {
		at := o.ObservedAt
		state.pendingClear = &at
	}
	if o.ObservedAt.Sub(*state.pendingClear) < state.rule.Hysteresis || state.rule.Latching {
		return alarm, TransitionClearPending, nil
	}
	now := o.ObservedAt
	alarm.Status = StatusCleared
	alarm.ClearedAt = &now
	state.pendingClear = nil
	return alarm, TransitionCleared, nil
}

// Acknowledge records operator awareness but never changes ConditionActive.
func (m *Manager) Acknowledge(ctx context.Context, id domain.AlarmID) (Alarm, error) {
	if err := ctx.Err(); err != nil {
		return Alarm{}, err
	}
	m.mu.Lock()
	alarm, ok := m.alarms[id]
	if !ok {
		m.mu.Unlock()
		return Alarm{}, ErrNotFound
	}
	if alarm.Status != StatusActive {
		m.mu.Unlock()
		return Alarm{}, ErrInvalidTransition
	}
	now := m.now().UTC()
	alarm.Status = StatusAcknowledged
	alarm.AcknowledgedAt = &now
	m.alarms[id] = alarm
	m.mu.Unlock()
	return cloneAlarm(alarm), m.emit(ctx, TransitionAcknowledged, alarm)
}

// Clear clears only a healthy condition; this is required for latching rules.
func (m *Manager) Clear(ctx context.Context, id domain.AlarmID) (Alarm, error) {
	if err := ctx.Err(); err != nil {
		return Alarm{}, err
	}
	m.mu.Lock()
	alarm, ok := m.alarms[id]
	if !ok {
		m.mu.Unlock()
		return Alarm{}, ErrNotFound
	}
	if alarm.ConditionActive {
		m.mu.Unlock()
		return Alarm{}, ErrConditionActive
	}
	if alarm.Status == StatusCleared {
		m.mu.Unlock()
		return Alarm{}, ErrInvalidTransition
	}
	now := m.now().UTC()
	alarm.Status = StatusCleared
	alarm.ClearedAt = &now
	m.alarms[id] = alarm
	m.mu.Unlock()
	return cloneAlarm(alarm), m.emit(ctx, TransitionCleared, alarm)
}

// Get returns a defensive copy of an alarm.
func (m *Manager) Get(ctx context.Context, id domain.AlarmID) (Alarm, error) {
	if err := ctx.Err(); err != nil {
		return Alarm{}, err
	}
	m.mu.RLock()
	a, ok := m.alarms[id]
	m.mu.RUnlock()
	if !ok {
		return Alarm{}, ErrNotFound
	}
	return cloneAlarm(a), nil
}

// List returns defensive copies ordered by first observation time.
func (m *Manager) List(ctx context.Context, status Status) ([]Alarm, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	out := make([]Alarm, 0, len(m.alarms))
	for _, a := range m.alarms {
		if status == "" || a.Status == status {
			out = append(out, cloneAlarm(a))
		}
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].FirstObserved.Before(out[j].FirstObserved) })
	return out, nil
}

func (m *Manager) emit(ctx context.Context, transition Transition, alarm Alarm) error {
	if m.publisher == nil {
		return nil
	}
	eventType := events.Type(transition)
	event, err := m.factory.New(m.Name(), eventType, alarm.Severity, alarm, alarm.CorrelationID)
	if err != nil {
		return err
	}
	m.logger.Log(ctx, slog.LevelWarn, "alarm lifecycle transition", "code", transition, "alarm_id", alarm.ID, "rule_id", alarm.RuleID, "correlation_id", alarm.CorrelationID)
	if err = m.publisher.Publish(ctx, event); err != nil {
		return fmt.Errorf("publish %s: %w", transition, err)
	}
	return nil
}
func validateRule(r Rule) error {
	if err := r.ID.Validate(); err != nil {
		return fmt.Errorf("rule ID: %w", err)
	}
	if r.Code == "" || r.Name == "" || r.Subject.Kind == "" {
		return errors.New("rule code, name, and subject kind are required")
	}
	if err := r.Subject.ID.Validate(); err != nil {
		return fmt.Errorf("subject ID: %w", err)
	}
	if r.Severity.Rank() == 0 {
		return errors.New("rule severity is invalid")
	}
	if r.Debounce < 0 || r.Hysteresis < 0 {
		return errors.New("debounce and hysteresis cannot be negative")
	}
	return nil
}
func appendEvidence(values []Evidence, value Evidence, limit int) []Evidence {
	if value.Code == "" && value.Message == "" {
		return values
	}
	value.Metadata = cloneMetadata(value.Metadata)
	values = append(values, value)
	if len(values) > limit {
		values = append([]Evidence(nil), values[len(values)-limit:]...)
	}
	return values
}
func cloneAlarm(a Alarm) Alarm {
	a.Evidence = append([]Evidence(nil), a.Evidence...)
	for i := range a.Evidence {
		a.Evidence[i].Metadata = cloneMetadata(a.Evidence[i].Metadata)
	}
	if a.AcknowledgedAt != nil {
		v := *a.AcknowledgedAt
		a.AcknowledgedAt = &v
	}
	if a.ClearedAt != nil {
		v := *a.ClearedAt
		a.ClearedAt = &v
	}
	return a
}
func cloneMetadata(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	out := make(map[string]string, len(source))
	for k, v := range source {
		out[k] = v
	}
	return out
}
