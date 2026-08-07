package shelly

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
	"github.com/tylerkirby004-droid/aquaos/internal/output"
)

// PowerReturnPolicy documents the expected device behavior after power loss.
// AquaOS verifies reported state after restart and never assumes this policy ran.
type PowerReturnPolicy string

const (
	// PowerReturnOff requires the external Shelly configuration to return off.
	PowerReturnOff PowerReturnPolicy = "off"
	// PowerReturnRestore permits firmware to restore the prior output state.
	PowerReturnRestore PowerReturnPolicy = "restore"
)

// Endpoint binds one logical equipment identity to one Shelly switch channel.
type Endpoint struct {
	ID                domain.EndpointID
	EquipmentID       domain.EquipmentID
	BaseURL           string
	Channel           int
	PollInterval      time.Duration
	RequestTimeout    time.Duration
	Retries           int
	SafeOn            bool
	PowerReturnPolicy PowerReturnPolicy
}

// Report is one direct reported-state and power observation.
type Report struct {
	EndpointID  domain.EndpointID
	EquipmentID domain.EquipmentID
	On          bool
	APower      float64
	Voltage     float64
	Current     float64
	Source      string
	ObservedAt  time.Time
	DesiredOn   *bool
	Pending     bool
}

// ReportedSink accepts device observations for canonical-state and policy
// application. It must not bypass the output service.
type ReportedSink interface {
	ReportShelly(context.Context, Report) error
}

// CommandTracker completes or expires output-service command records.
type CommandTracker interface {
	Reconcile(context.Context, domain.CommandID, bool, time.Time) (output.Result, error)
	ExpireAcknowledged(context.Context, domain.CommandID, string, time.Time) (output.Result, error)
}

// FailureSink translates adapter availability into alarm observations and
// configured safe-response requests through application services.
type FailureSink interface {
	ShellyFailure(context.Context, Endpoint, bool, string, time.Time) error
}

type pendingCommand struct {
	id        domain.CommandID
	want      bool
	expiresAt time.Time
}

// Adapter owns bounded Shelly RPC dispatch and one documented, cancellable
// reconciliation worker per configured endpoint.
type Adapter struct {
	mu        sync.RWMutex
	client    Client
	reports   ReportedSink
	tracker   CommandTracker
	failures  FailureSink
	logger    *slog.Logger
	now       func() time.Time
	endpoints map[domain.EquipmentID]Endpoint
	pending   map[domain.EquipmentID]pendingCommand
	desired   map[domain.EquipmentID]bool
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	started   bool
	errors    map[domain.EndpointID]string
}

// NewAdapter validates dependencies and endpoint uniqueness.
func NewAdapter(client Client, reports ReportedSink, tracker CommandTracker, failures FailureSink, logger *slog.Logger, now func() time.Time, endpoints ...Endpoint) (*Adapter, error) {
	if client == nil || reports == nil || tracker == nil || failures == nil || logger == nil || now == nil {
		return nil, errors.New("all Shelly adapter dependencies are required")
	}
	adapter := &Adapter{client: client, reports: reports, tracker: tracker, failures: failures, logger: logger, now: now, endpoints: make(map[domain.EquipmentID]Endpoint), pending: make(map[domain.EquipmentID]pendingCommand), desired: make(map[domain.EquipmentID]bool), errors: make(map[domain.EndpointID]string)}
	for _, endpoint := range endpoints {
		if err := validateEndpoint(endpoint); err != nil {
			return nil, err
		}
		if _, exists := adapter.endpoints[endpoint.EquipmentID]; exists {
			return nil, fmt.Errorf("duplicate Shelly equipment endpoint %s", endpoint.EquipmentID)
		}
		adapter.endpoints[endpoint.EquipmentID] = endpoint
	}
	return adapter, nil
}

// Name returns the component name.
func (a *Adapter) Name() string { return "adapter-shelly" }

// Start launches explicit context-owned reconciliation workers.
func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return nil
	}
	workerCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.started = true
	endpoints := make([]Endpoint, 0, len(a.endpoints))
	for _, endpoint := range a.endpoints {
		endpoints = append(endpoints, endpoint)
	}
	a.mu.Unlock()
	for _, endpoint := range endpoints {
		a.wg.Add(1)
		go a.runEndpoint(workerCtx, endpoint)
	}
	return nil
}

// Stop cancels and joins every reconciliation worker.
func (a *Adapter) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	cancel := a.cancel
	a.cancel = nil
	a.started = false
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.wg.Wait()
	return ctx.Err()
}

// Health reports lifecycle and latest endpoint failure state.
func (a *Adapter) Health() health.Status {
	a.mu.RLock()
	started, failureCount := a.started, len(a.errors)
	a.mu.RUnlock()
	state := health.StateHealthy
	if !started {
		state = health.StateUnhealthy
	} else if failureCount > 0 {
		state = health.StateDegraded
	}
	message := ""
	if failureCount > 0 {
		message = fmt.Sprintf("%d Shelly endpoint(s) unhealthy", failureCount)
	}
	return health.NewStatus(a.Name(), state, message, a.now().UTC())
}

// Dispatch implements output.Executor. RPC acceptance is only an
// acknowledgement; success still requires a later reported-state poll.
func (a *Adapter) Dispatch(ctx context.Context, command output.Command) (output.Acknowledgement, error) {
	a.mu.RLock()
	endpoint, ok := a.endpoints[command.EquipmentID]
	a.mu.RUnlock()
	if !ok {
		return output.Acknowledgement{Accepted: false, Reason: "shelly.endpoint_not_found"}, nil
	}
	if err := a.setWithRetry(ctx, endpoint, command.On); err != nil {
		return output.Acknowledgement{}, err
	}
	now := a.now().UTC()
	a.mu.Lock()
	a.pending[command.EquipmentID] = pendingCommand{id: command.ID, want: command.On, expiresAt: command.ExpiresAt}
	a.desired[command.EquipmentID] = command.On
	a.mu.Unlock()
	return output.Acknowledgement{Accepted: true, Reason: "shelly.rpc_accepted", AcknowledgedAt: now}, nil
}

func (a *Adapter) runEndpoint(ctx context.Context, endpoint Endpoint) {
	defer a.wg.Done()
	a.poll(ctx, endpoint)
	ticker := time.NewTicker(endpoint.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.poll(ctx, endpoint)
		}
	}
}

func (a *Adapter) poll(ctx context.Context, endpoint Endpoint) {
	configCtx, configCancel := context.WithTimeout(ctx, endpoint.RequestTimeout)
	config, configErr := a.client.GetSwitchConfig(configCtx, endpoint.BaseURL, endpoint.Channel)
	configCancel()
	if configErr != nil {
		a.failPoll(ctx, endpoint, "shelly.config_unavailable", configErr)
		return
	}
	expectedInitialState := "off"
	if endpoint.PowerReturnPolicy == PowerReturnRestore {
		expectedInitialState = "restore_last"
	}
	if config.InitialState != expectedInitialState {
		a.failPoll(ctx, endpoint, "shelly.power_return_mismatch", fmt.Errorf("configured initial_state %q, expected %q", config.InitialState, expectedInitialState))
		return
	}
	requestCtx, cancel := context.WithTimeout(ctx, endpoint.RequestTimeout)
	status, err := a.client.GetSwitchStatus(requestCtx, endpoint.BaseURL, endpoint.Channel)
	cancel()
	now := a.now().UTC()
	if err != nil {
		reason := "shelly.unreachable: " + err.Error()
		a.setError(endpoint.ID, reason)
		a.logger.ErrorContext(ctx, "Shelly status failed", "code", "shelly.unreachable", "endpoint_id", endpoint.ID, "equipment_id", endpoint.EquipmentID, "error", err)
		_ = a.failures.ShellyFailure(ctx, endpoint, true, "shelly.unreachable", now)
		a.expireIfNeeded(ctx, endpoint, now, true)
		return
	}
	a.setError(endpoint.ID, "")
	a.mu.RLock()
	desired, hasDesired := a.desired[endpoint.EquipmentID]
	_, hasPending := a.pending[endpoint.EquipmentID]
	a.mu.RUnlock()
	var desiredPointer *bool
	if hasDesired {
		desiredPointer = &desired
	}
	if err := a.reports.ReportShelly(ctx, Report{EndpointID: endpoint.ID, EquipmentID: endpoint.EquipmentID, On: status.Output, APower: status.APower, Voltage: status.Voltage, Current: status.Current, Source: status.Source, ObservedAt: now, DesiredOn: desiredPointer, Pending: hasPending}); err != nil {
		a.logger.ErrorContext(ctx, "Shelly report rejected", "code", "shelly.report_rejected", "endpoint_id", endpoint.ID, "error", err)
	}
	a.mu.RLock()
	pending, exists := a.pending[endpoint.EquipmentID]
	a.mu.RUnlock()
	if !exists {
		return
	}
	if !now.Before(pending.expiresAt) {
		a.expireIfNeeded(ctx, endpoint, now, false)
		return
	}
	if status.Output == pending.want {
		if _, err := a.tracker.Reconcile(ctx, pending.id, status.Output, now); err == nil {
			a.mu.Lock()
			delete(a.pending, endpoint.EquipmentID)
			a.mu.Unlock()
		}
	}
}

func (a *Adapter) failPoll(ctx context.Context, endpoint Endpoint, code string, err error) {
	now := a.now().UTC()
	reason := code + ": " + err.Error()
	a.setError(endpoint.ID, reason)
	a.logger.ErrorContext(ctx, "Shelly verification failed", "code", code, "endpoint_id", endpoint.ID, "equipment_id", endpoint.EquipmentID, "error", err)
	_ = a.failures.ShellyFailure(ctx, endpoint, true, code, now)
	a.expireIfNeeded(ctx, endpoint, now, true)
}

func (a *Adapter) expireIfNeeded(ctx context.Context, endpoint Endpoint, now time.Time, force bool) {
	a.mu.RLock()
	pending, exists := a.pending[endpoint.EquipmentID]
	a.mu.RUnlock()
	if !exists || (!force && now.Before(pending.expiresAt)) {
		return
	}
	if _, err := a.tracker.ExpireAcknowledged(ctx, pending.id, "shelly.reconciliation_expired", now); err == nil {
		a.mu.Lock()
		delete(a.pending, endpoint.EquipmentID)
		a.mu.Unlock()
	}
	_ = a.failures.ShellyFailure(ctx, endpoint, true, "shelly.reconciliation_expired", now)
}

func (a *Adapter) setWithRetry(ctx context.Context, endpoint Endpoint, on bool) error {
	var last error
	for attempt := 0; attempt <= endpoint.Retries; attempt++ {
		requestCtx, cancel := context.WithTimeout(ctx, endpoint.RequestTimeout)
		_, last = a.client.SetSwitch(requestCtx, endpoint.BaseURL, endpoint.Channel, on)
		cancel()
		if last == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return fmt.Errorf("shelly Switch.Set failed after %d attempts: %w", endpoint.Retries+1, last)
}

func (a *Adapter) setError(endpointID domain.EndpointID, message string) {
	a.mu.Lock()
	if message == "" {
		delete(a.errors, endpointID)
	} else {
		a.errors[endpointID] = message
	}
	a.mu.Unlock()
}

func validateEndpoint(endpoint Endpoint) error {
	if err := endpoint.ID.Validate(); err != nil {
		return fmt.Errorf("shelly endpoint ID: %w", err)
	}
	if err := endpoint.EquipmentID.Validate(); err != nil {
		return fmt.Errorf("shelly equipment ID: %w", err)
	}
	if _, err := rpcURL(endpoint.BaseURL, "Switch.GetStatus"); err != nil {
		return err
	}
	if endpoint.Channel < 0 || endpoint.Channel > 31 {
		return errors.New("shelly channel must be between 0 and 31")
	}
	if endpoint.PollInterval <= 0 || endpoint.RequestTimeout <= 0 || endpoint.RequestTimeout > endpoint.PollInterval {
		return errors.New("shelly request timeout must be positive and no greater than poll interval")
	}
	if endpoint.Retries < 0 || endpoint.Retries > 5 {
		return errors.New("shelly retries must be between 0 and 5")
	}
	if endpoint.PowerReturnPolicy != PowerReturnOff && endpoint.PowerReturnPolicy != PowerReturnRestore {
		return errors.New("shelly power-return policy must be off or restore")
	}
	return nil
}
