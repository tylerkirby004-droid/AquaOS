// Package output is the sole authorized equipment command path.
package output

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
	"github.com/tylerkirby004-droid/aquaos/internal/safety"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

// Status identifies command progress. Only StatusSucceeded represents reconciled success.
type Status string

//nolint:revive // Status values are documented collectively by Status.
const (
	StatusValidated    Status = "validated"
	StatusRejected     Status = "rejected"
	StatusDispatched   Status = "dispatched"
	StatusAcknowledged Status = "acknowledged"
	StatusSucceeded    Status = "succeeded"
	StatusFailed       Status = "failed"
	StatusExpired      Status = "expired"
)

// Stable command result reasons are safe API and event contracts.
const (
	ReasonValidated        = "command.validated"
	ReasonDispatched       = "command.dispatched"
	ReasonRejected         = "command.rejected"
	ReasonExpired          = "command.expired"
	ReasonRevisionConflict = "command.revision_conflict"
	ReasonDispatchFailed   = "command.dispatch_failed"
	ReasonAdapterRejected  = "command.adapter_rejected"
	ReasonAcknowledged     = "command.acknowledged"
	ReasonReconciled       = "command.reconciled"
	ReasonReportedMismatch = "command.reported_mismatch"
)

// Command contains all causal, authorization, expiry, and concurrency metadata.
type Command struct {
	ID               domain.CommandID     `json:"id"`
	IdempotencyKey   string               `json:"idempotencyKey"`
	CorrelationID    domain.CorrelationID `json:"correlationId"`
	EquipmentID      domain.EquipmentID   `json:"equipmentId"`
	Requester        string               `json:"requester"`
	IssuedAt         time.Time            `json:"issuedAt"`
	ExpiresAt        time.Time            `json:"expiresAt"`
	ExpectedRevision *domain.Revision     `json:"expectedRevision,omitempty"`
	On               bool                 `json:"on"`
}

// Result is the durable-shape command lifecycle record.
type Result struct {
	Command        Command    `json:"command"`
	Status         Status     `json:"status"`
	Reason         string     `json:"reason"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	AcknowledgedAt *time.Time `json:"acknowledgedAt,omitempty"`
	ReconciledAt   *time.Time `json:"reconciledAt,omitempty"`
}

// Acknowledgement is an adapter's explicit acceptance or rejection of dispatch.
type Acknowledgement struct {
	Accepted       bool      `json:"accepted"`
	Reason         string    `json:"reason"`
	AcknowledgedAt time.Time `json:"acknowledgedAt"`
}

// Executor is the hardware boundary owned by the command service.
type Executor interface {
	Dispatch(context.Context, Command) (Acknowledgement, error)
}

// Policy is the consumer-owned safety evaluation boundary.
type Policy interface {
	Evaluate(context.Context, safety.Intent) (safety.Decision, error)
	RecordReported(context.Context, domain.EquipmentID, bool, time.Time) error
	CheckWatchdogs(context.Context) ([]safety.WatchdogAction, error)
}

// RevisionReader supplies optimistic concurrency state.
type RevisionReader interface {
	Snapshot(context.Context) (state.Snapshot, error)
}

var (
	// ErrNotFound indicates an unknown command.
	ErrNotFound = errors.New("command not found")
	// ErrConflict indicates reuse of an idempotency key for different content.
	ErrConflict = errors.New("idempotency key conflict")
	// ErrInvalidTransition indicates an impossible lifecycle transition.
	ErrInvalidTransition = errors.New("invalid command transition")
)

// Service validates, authorizes, dispatches, acknowledges, and reconciles commands.
type Service struct {
	mu               sync.RWMutex
	results          map[domain.CommandID]Result
	idempotency      map[string]domain.CommandID
	policy           Policy
	revisions        RevisionReader
	executor         Executor
	publisher        events.Publisher
	factory          *events.Factory
	logger           *slog.Logger
	now              func() time.Time
	newCommandID     func() (domain.CommandID, error)
	newCorrelationID func() (domain.CorrelationID, error)
	started          bool
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// NewService constructs a command service with explicit boundaries.
func NewService(policy Policy, revisions RevisionReader, executor Executor, publisher events.Publisher, logger *slog.Logger) (*Service, error) {
	clock := systemClock{}
	factory, err := events.NewFactory(clock, domain.NewCorrelationID)
	if err != nil {
		return nil, err
	}
	return NewServiceWithDependencies(policy, revisions, executor, publisher, logger, clock.Now, domain.NewCommandID, domain.NewCorrelationID, factory)
}

// NewServiceWithDependencies constructs a deterministic service for tests and adapters.
func NewServiceWithDependencies(policy Policy, revisions RevisionReader, executor Executor, publisher events.Publisher, logger *slog.Logger, now func() time.Time, newCommandID func() (domain.CommandID, error), newCorrelationID func() (domain.CorrelationID, error), factory *events.Factory) (*Service, error) {
	if policy == nil || revisions == nil || executor == nil || logger == nil || now == nil || newCommandID == nil || newCorrelationID == nil || factory == nil {
		return nil, errors.New("all command service dependencies are required")
	}
	return &Service{results: make(map[domain.CommandID]Result), idempotency: make(map[string]domain.CommandID), policy: policy, revisions: revisions, executor: executor, publisher: publisher, factory: factory, logger: logger, now: now, newCommandID: newCommandID, newCorrelationID: newCorrelationID}, nil
}

// Name returns the lifecycle component name.
func (s *Service) Name() string { return "output" }

// Start marks the command service available.
func (s *Service) Start(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = true
	return nil
}

// Stop prevents lifecycle readiness without discarding command records.
func (s *Service) Stop(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = false
	return nil
}

// Health reports command-service lifecycle state.
func (s *Service) Health() health.Status {
	s.mu.RLock()
	started := s.started
	s.mu.RUnlock()
	condition := health.StateUnhealthy
	if started {
		condition = health.StateHealthy
	}
	return health.NewStatus(s.Name(), condition, "", s.now().UTC())
}

// Submit runs structural validation, idempotency, expiry, revision, safety, and dispatch in that order.
func (s *Service) Submit(ctx context.Context, command Command) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if command.ID == "" {
		id, err := s.newCommandID()
		if err != nil {
			return Result{}, err
		}
		command.ID = id
	}
	if command.CorrelationID == "" {
		id, err := s.newCorrelationID()
		if err != nil {
			return Result{}, err
		}
		command.CorrelationID = id
	}
	if err := validateCommand(command); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	if id, exists := s.idempotency[command.IdempotencyKey]; exists {
		existing := s.results[id]
		s.mu.Unlock()
		if equivalent(existing.Command, command) {
			return cloneResult(existing), nil
		}
		return Result{}, ErrConflict
	}
	s.mu.Unlock()
	now := s.now().UTC()
	if !now.Before(command.ExpiresAt) {
		return s.record(ctx, command, StatusExpired, ReasonExpired, events.CommandExpired)
	}
	if command.ExpectedRevision != nil {
		snapshot, err := s.revisions.Snapshot(ctx)
		if err != nil {
			return Result{}, err
		}
		if snapshot.Revision != *command.ExpectedRevision {
			return s.record(ctx, command, StatusRejected, ReasonRevisionConflict, events.CommandRejected)
		}
	}
	decision, err := s.policy.Evaluate(ctx, safety.Intent{EquipmentID: command.EquipmentID, On: command.On, IssuedAt: command.IssuedAt})
	if err != nil {
		return Result{}, err
	}
	if !decision.Allowed {
		return s.record(ctx, command, StatusRejected, decision.Reason, events.CommandRejected)
	}
	result, err := s.record(ctx, command, StatusValidated, ReasonValidated, events.CommandValidated)
	if err != nil {
		return result, err
	}
	result, err = s.transition(ctx, command.ID, StatusDispatched, ReasonDispatched, events.CommandDispatched, nil)
	if err != nil {
		return result, err
	}
	acknowledgement, dispatchErr := s.executor.Dispatch(ctx, command)
	if dispatchErr != nil {
		return s.transition(ctx, command.ID, StatusFailed, ReasonDispatchFailed, events.CommandRejected, nil)
	}
	if !acknowledgement.Accepted {
		return s.transition(ctx, command.ID, StatusFailed, ReasonAdapterRejected, events.CommandRejected, nil)
	}
	acknowledgedAt := acknowledgement.AcknowledgedAt
	if acknowledgedAt.IsZero() {
		acknowledgedAt = s.now().UTC()
	}
	return s.transition(ctx, command.ID, StatusAcknowledged, ReasonAcknowledged, events.CommandAcknowledged, &acknowledgedAt)
}

// Reconcile confirms reported physical state. It is the only path to success.
func (s *Service) Reconcile(ctx context.Context, id domain.CommandID, reportedOn bool, observedAt time.Time) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if observedAt.IsZero() {
		return Result{}, errors.New("reconciliation timestamp is required")
	}
	s.mu.RLock()
	result, ok := s.results[id]
	s.mu.RUnlock()
	if !ok {
		return Result{}, ErrNotFound
	}
	if result.Status != StatusAcknowledged {
		return Result{}, ErrInvalidTransition
	}
	if err := s.policy.RecordReported(ctx, result.Command.EquipmentID, reportedOn, observedAt); err != nil {
		return Result{}, err
	}
	if reportedOn != result.Command.On {
		return s.transition(ctx, id, StatusFailed, ReasonReportedMismatch, events.CommandReconciled, nil)
	}
	return s.transition(ctx, id, StatusSucceeded, ReasonReconciled, events.CommandReconciled, &observedAt)
}

// Get returns a defensive copy of one command result.
func (s *Service) Get(ctx context.Context, id domain.CommandID) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	s.mu.RLock()
	result, ok := s.results[id]
	s.mu.RUnlock()
	if !ok {
		return Result{}, ErrNotFound
	}
	return cloneResult(result), nil
}

// List returns command results in issued-time order.
func (s *Service) List(ctx context.Context) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	result := make([]Result, 0, len(s.results))
	for _, item := range s.results {
		result = append(result, cloneResult(item))
	}
	s.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Command.IssuedAt.Before(result[j].Command.IssuedAt) })
	return result, nil
}

// RunWatchdogs evaluates hard-limit and override expiry actions through the same command pipeline.
func (s *Service) RunWatchdogs(ctx context.Context, ttl time.Duration) ([]Result, error) {
	if ttl <= 0 {
		return nil, errors.New("watchdog command TTL must be positive")
	}
	actions, err := s.policy.CheckWatchdogs(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(actions))
	for _, action := range actions {
		id, createErr := s.newCommandID()
		if createErr != nil {
			return results, createErr
		}
		correlationID, createErr := s.newCorrelationID()
		if createErr != nil {
			return results, createErr
		}
		command := Command{ID: id, IdempotencyKey: "watchdog/" + string(id), CorrelationID: correlationID, EquipmentID: action.EquipmentID, Requester: "safety-watchdog", IssuedAt: action.ObservedAt, ExpiresAt: action.ObservedAt.Add(ttl), On: action.On}
		result, submitErr := s.Submit(ctx, command)
		results = append(results, result)
		if submitErr != nil {
			return results, submitErr
		}
	}
	return results, nil
}

func (s *Service) record(ctx context.Context, command Command, status Status, reason string, eventType events.Type) (Result, error) {
	result := Result{Command: cloneCommand(command), Status: status, Reason: reason, UpdatedAt: s.now().UTC()}
	s.mu.Lock()
	s.results[command.ID] = result
	s.idempotency[command.IdempotencyKey] = command.ID
	s.mu.Unlock()
	return cloneResult(result), s.emit(ctx, eventType, result)
}
func (s *Service) transition(ctx context.Context, id domain.CommandID, status Status, reason string, eventType events.Type, at *time.Time) (Result, error) {
	s.mu.Lock()
	result, ok := s.results[id]
	if !ok {
		s.mu.Unlock()
		return Result{}, ErrNotFound
	}
	result.Status = status
	result.Reason = reason
	result.UpdatedAt = s.now().UTC()
	if status == StatusAcknowledged {
		result.AcknowledgedAt = cloneTime(at)
	}
	if status == StatusSucceeded {
		result.ReconciledAt = cloneTime(at)
	}
	s.results[id] = result
	s.mu.Unlock()
	return cloneResult(result), s.emit(ctx, eventType, result)
}
func (s *Service) emit(ctx context.Context, eventType events.Type, result Result) error {
	s.logger.InfoContext(ctx, "equipment command lifecycle", "code", result.Reason, "command_id", result.Command.ID, "equipment_id", result.Command.EquipmentID, "correlation_id", result.Command.CorrelationID, "status", result.Status)
	if s.publisher == nil {
		return nil
	}
	severity := events.SeverityInfo
	if result.Status == StatusRejected || result.Status == StatusFailed || result.Status == StatusExpired {
		severity = events.SeverityWarning
	}
	event, err := s.factory.New(s.Name(), eventType, severity, result, result.Command.CorrelationID)
	if err != nil {
		return err
	}
	return s.publisher.Publish(ctx, event)
}
func validateCommand(command Command) error {
	if err := command.ID.Validate(); err != nil {
		return fmt.Errorf("command ID: %w", err)
	}
	if err := command.CorrelationID.Validate(); err != nil {
		return fmt.Errorf("correlation ID: %w", err)
	}
	if err := command.EquipmentID.Validate(); err != nil {
		return fmt.Errorf("equipment ID: %w", err)
	}
	if command.IdempotencyKey == "" || len(command.IdempotencyKey) > 128 {
		return errors.New("idempotency key is required and cannot exceed 128 bytes")
	}
	if command.Requester == "" {
		return errors.New("command requester is required")
	}
	if command.IssuedAt.IsZero() || command.ExpiresAt.IsZero() || !command.ExpiresAt.After(command.IssuedAt) {
		return errors.New("command expiry must follow issue time")
	}
	return nil
}
func equivalent(left, right Command) bool {
	return left.IdempotencyKey == right.IdempotencyKey && left.EquipmentID == right.EquipmentID && left.Requester == right.Requester && left.On == right.On
}
func cloneCommand(command Command) Command {
	if command.ExpectedRevision != nil {
		value := *command.ExpectedRevision
		command.ExpectedRevision = &value
	}
	return command
}
func cloneResult(result Result) Result {
	result.Command = cloneCommand(result.Command)
	result.AcknowledgedAt = cloneTime(result.AcknowledgedAt)
	result.ReconciledAt = cloneTime(result.ReconciledAt)
	return result
}
func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// RejectingExecutor is the safe default until a local adapter is configured.
// It never reaches hardware.
type RejectingExecutor struct{}

// Dispatch rejects every command explicitly.
func (RejectingExecutor) Dispatch(context.Context, Command) (Acknowledgement, error) {
	return Acknowledgement{Accepted: false, Reason: "adapter.not_configured"}, nil
}
