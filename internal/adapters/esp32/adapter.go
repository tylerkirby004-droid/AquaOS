package esp32

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

// Endpoint configures one node and exactly two independently identified probes.
type Endpoint struct {
	ID                domain.EndpointID
	DeviceID          domain.DeviceID
	BaseURL           string
	BearerToken       string
	ProbeIDs          [2]domain.SensorID
	PollInterval      time.Duration
	RequestTimeout    time.Duration
	FreshFor          time.Duration
	MaximumClockSkew  time.Duration
	MaximumDifference float64
}

// MeasurementSink accepts validated generic measurements for canonical state.
type MeasurementSink interface {
	ReportESP32(context.Context, domain.EndpointID, domain.Measurement) error
}

// FailureSink translates node and probe conditions into alarm observations.
type FailureSink interface {
	ESP32Failure(context.Context, Endpoint, bool, string, time.Time) error
}

type sequenceState struct {
	bootID   string
	sequence uint64
}

// Adapter owns explicit cancellable polling workers and sequence validation.
type Adapter struct {
	mu        sync.RWMutex
	client    Client
	sink      MeasurementSink
	failures  FailureSink
	logger    *slog.Logger
	now       func() time.Time
	endpoints []Endpoint
	sequences map[domain.EndpointID]sequenceState
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	started   bool
	errors    map[domain.EndpointID]string
}

// NewAdapter validates all dependencies and node configuration.
func NewAdapter(client Client, sink MeasurementSink, failures FailureSink, logger *slog.Logger, now func() time.Time, endpoints ...Endpoint) (*Adapter, error) {
	if client == nil || sink == nil || failures == nil || logger == nil || now == nil {
		return nil, errors.New("all ESP32 adapter dependencies are required")
	}
	seen := make(map[domain.EndpointID]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		if err := validateEndpoint(endpoint); err != nil {
			return nil, err
		}
		if _, exists := seen[endpoint.ID]; exists {
			return nil, fmt.Errorf("duplicate ESP32 endpoint %s", endpoint.ID)
		}
		seen[endpoint.ID] = struct{}{}
	}
	return &Adapter{client: client, sink: sink, failures: failures, logger: logger, now: now, endpoints: append([]Endpoint(nil), endpoints...), sequences: make(map[domain.EndpointID]sequenceState), errors: make(map[domain.EndpointID]string)}, nil
}

// Name returns the component name.
func (a *Adapter) Name() string { return "adapter-esp32" }

// Start launches one documented context-owned poller per endpoint.
func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return nil
	}
	workerCtx, cancel := context.WithCancel(ctx)
	a.cancel, a.started = cancel, true
	endpoints := append([]Endpoint(nil), a.endpoints...)
	a.mu.Unlock()
	for _, endpoint := range endpoints {
		a.wg.Add(1)
		go a.runEndpoint(workerCtx, endpoint)
	}
	return nil
}

// Stop cancels and joins all pollers.
func (a *Adapter) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	cancel := a.cancel
	a.cancel, a.started = nil, false
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.wg.Wait()
	return ctx.Err()
}

// Health reports lifecycle and current node availability.
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
		message = fmt.Sprintf("%d ESP32 endpoint(s) unhealthy", failureCount)
	}
	return health.NewStatus(a.Name(), state, message, a.now().UTC())
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
	requestCtx, cancel := context.WithTimeout(ctx, endpoint.RequestTimeout)
	snapshot, err := a.client.Snapshot(requestCtx, endpoint.BaseURL, endpoint.BearerToken)
	cancel()
	now := a.now().UTC()
	if err != nil {
		a.fail(ctx, endpoint, "esp32.unreachable", err, now)
		return
	}
	measurements, condition, err := a.validateSnapshot(endpoint, snapshot, now)
	if err != nil {
		a.fail(ctx, endpoint, condition, err, now)
		return
	}
	a.setError(endpoint.ID, "")
	_ = a.failures.ESP32Failure(ctx, endpoint, condition != "", condition, now)
	for _, measurement := range measurements {
		if err := a.sink.ReportESP32(ctx, endpoint.ID, measurement); err != nil {
			a.logger.ErrorContext(ctx, "ESP32 measurement rejected", "code", "esp32.measurement_rejected", "endpoint_id", endpoint.ID, "sensor_id", measurement.SensorID, "error", err)
		}
	}
}

func (a *Adapter) validateSnapshot(endpoint Endpoint, snapshot SnapshotDTO, now time.Time) ([]domain.Measurement, string, error) {
	if snapshot.SchemaVersion != "1.0" {
		return nil, "esp32.schema_unsupported", errors.New("unsupported ESP32 snapshot schema")
	}
	if snapshot.NodeID != string(endpoint.DeviceID) || snapshot.BootID == "" || len(snapshot.BootID) > 128 || snapshot.Sequence == 0 {
		return nil, "esp32.identity_invalid", errors.New("invalid ESP32 node identity, boot ID, or sequence")
	}
	if snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(now.Add(endpoint.MaximumClockSkew)) {
		return nil, "esp32.timestamp_invalid", errors.New("invalid ESP32 observation timestamp")
	}
	if len(snapshot.Probes) != 2 {
		return nil, "esp32.probe_count_invalid", errors.New("esp32 snapshot must contain exactly two probes")
	}
	byID := make(map[domain.SensorID]ProbeDTO, 2)
	for _, probe := range snapshot.Probes {
		id := domain.SensorID(probe.SensorID)
		if err := id.Validate(); err != nil {
			return nil, "esp32.probe_identity_invalid", errors.New("esp32 probe identity is invalid")
		}
		if _, exists := byID[id]; exists {
			return nil, "esp32.probe_identity_invalid", errors.New("esp32 probe identity is duplicated")
		}
		byID[id] = probe
	}
	quality := domain.QualityGood
	condition := ""
	values := make([]float64, 2)
	valid := [2]bool{}
	for index, id := range endpoint.ProbeIDs {
		probe, exists := byID[id]
		if !exists || !probe.Valid || probe.Celsius == nil || math.IsNaN(valueOrZero(probe.Celsius)) || math.IsInf(valueOrZero(probe.Celsius), 0) {
			condition = "esp32.probe_invalid"
			continue
		}
		values[index], valid[index] = *probe.Celsius, true
	}
	if valid[0] && valid[1] && math.Abs(values[0]-values[1]) > endpoint.MaximumDifference {
		quality, condition = domain.QualitySuspect, "esp32.probe_disagreement"
	}
	if !snapshot.ObservedAt.Add(endpoint.FreshFor).After(now) {
		quality, condition = domain.QualityStale, "esp32.snapshot_stale"
	}
	measurements := make([]domain.Measurement, 0, 2)
	for index, id := range endpoint.ProbeIDs {
		probeQuality := quality
		value := values[index]
		if !valid[index] {
			probeQuality = domain.QualityInvalid
		}
		quantity, err := domain.NewQuantity(domain.QuantityTemperature, value, domain.UnitCelsius)
		if err != nil {
			return nil, "esp32.temperature_invalid", err
		}
		measurements = append(measurements, domain.Measurement{SensorID: id, Value: quantity, Quality: probeQuality, ObservedAt: snapshot.ObservedAt.UTC(), ReceivedAt: now, FreshFor: endpoint.FreshFor})
	}
	a.mu.Lock()
	previous := a.sequences[endpoint.ID]
	if previous.bootID == snapshot.BootID && snapshot.Sequence <= previous.sequence {
		a.mu.Unlock()
		return nil, "esp32.sequence_stale", errors.New("duplicate or out-of-order ESP32 snapshot")
	}
	a.sequences[endpoint.ID] = sequenceState{bootID: snapshot.BootID, sequence: snapshot.Sequence}
	a.mu.Unlock()
	return measurements, condition, nil
}

func (a *Adapter) fail(ctx context.Context, endpoint Endpoint, code string, err error, now time.Time) {
	a.setError(endpoint.ID, code+": "+err.Error())
	a.logger.ErrorContext(ctx, "ESP32 snapshot failed", "code", code, "endpoint_id", endpoint.ID, "device_id", endpoint.DeviceID, "error", err)
	_ = a.failures.ESP32Failure(ctx, endpoint, true, code, now)
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
func valueOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func validateEndpoint(endpoint Endpoint) error {
	if err := endpoint.ID.Validate(); err != nil {
		return fmt.Errorf("esp32 endpoint ID: %w", err)
	}
	if err := endpoint.DeviceID.Validate(); err != nil {
		return fmt.Errorf("esp32 device ID: %w", err)
	}
	if endpoint.ProbeIDs[0] == endpoint.ProbeIDs[1] {
		return errors.New("esp32 probes must have independent sensor IDs")
	}
	for _, id := range endpoint.ProbeIDs {
		if err := id.Validate(); err != nil {
			return fmt.Errorf("esp32 probe ID: %w", err)
		}
	}
	if _, err := snapshotURL(endpoint.BaseURL); err != nil {
		return err
	}
	if endpoint.PollInterval <= 0 || endpoint.RequestTimeout <= 0 || endpoint.RequestTimeout > endpoint.PollInterval {
		return errors.New("esp32 request timeout must be positive and no greater than poll interval")
	}
	if endpoint.FreshFor < endpoint.PollInterval || endpoint.MaximumClockSkew < 0 || endpoint.MaximumClockSkew > time.Minute {
		return errors.New("esp32 freshness or clock-skew configuration is invalid")
	}
	if endpoint.MaximumDifference <= 0 || endpoint.MaximumDifference > 10 {
		return errors.New("esp32 maximum probe difference must be greater than zero and at most 10 celsius")
	}
	return nil
}
