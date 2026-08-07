package esp32

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

const (
	espEndpoint = domain.EndpointID("11111111-1111-4111-8111-111111111111")
	espDevice   = domain.DeviceID("22222222-2222-4222-8222-222222222222")
	espProbeA   = domain.SensorID("33333333-3333-4333-8333-333333333333")
	espProbeB   = domain.SensorID("44444444-4444-4444-8444-444444444444")
)

type fakeNodeClient struct {
	mu       sync.Mutex
	snapshot SnapshotDTO
	err      error
}

func (f *fakeNodeClient) Snapshot(context.Context, string, string) (SnapshotDTO, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot, f.err
}

type measurementCollector struct{ measurements []domain.Measurement }

func (c *measurementCollector) ReportESP32(_ context.Context, _ domain.EndpointID, measurement domain.Measurement) error {
	c.measurements = append(c.measurements, measurement)
	return nil
}

type espFailure struct {
	active bool
	reason string
}
type failureCollector struct{ records []espFailure }

func (c *failureCollector) ESP32Failure(_ context.Context, _ Endpoint, active bool, reason string, _ time.Time) error {
	c.records = append(c.records, espFailure{active: active, reason: reason})
	return nil
}

func TestDualProbeAgreementAndDisagreement(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	first, second := 25.0, 25.2
	client := &fakeNodeClient{snapshot: validSnapshot(now, "boot-a", 1, first, second)}
	sink, failures := &measurementCollector{}, &failureCollector{}
	adapter := newESPAdapter(t, client, sink, failures, func() time.Time { return now })
	adapter.poll(context.Background(), adapter.endpoints[0])
	if len(sink.measurements) != 2 || sink.measurements[0].Quality != domain.QualityGood {
		t.Fatalf("measurements = %+v", sink.measurements)
	}
	client.snapshot = validSnapshot(now, "boot-a", 2, first, 27.0)
	adapter.poll(context.Background(), adapter.endpoints[0])
	if sink.measurements[2].Quality != domain.QualitySuspect || failures.records[len(failures.records)-1].reason != "esp32.probe_disagreement" {
		t.Fatalf("disagreement not surfaced: %+v %+v", sink.measurements, failures.records)
	}
}

func TestDuplicateSequenceRejectedAndRebootAccepted(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	first, second := 25.0, 25.1
	client := &fakeNodeClient{snapshot: validSnapshot(now, "boot-a", 9, first, second)}
	sink, failures := &measurementCollector{}, &failureCollector{}
	adapter := newESPAdapter(t, client, sink, failures, func() time.Time { return now })
	adapter.poll(context.Background(), adapter.endpoints[0])
	adapter.poll(context.Background(), adapter.endpoints[0])
	if failures.records[len(failures.records)-1].reason != "esp32.sequence_stale" {
		t.Fatalf("records = %+v", failures.records)
	}
	client.snapshot = validSnapshot(now, "boot-b", 1, first, second)
	adapter.poll(context.Background(), adapter.endpoints[0])
	if len(sink.measurements) != 4 {
		t.Fatalf("reboot snapshot was not accepted: %d", len(sink.measurements))
	}
}

func TestStaleAndPartialProbeQuality(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 10, 0, time.UTC)
	first := 25.0
	snapshot := validSnapshot(now.Add(-10*time.Second), "boot-a", 1, first, 25.1)
	snapshot.Probes[1].Valid, snapshot.Probes[1].Celsius = false, nil
	client := &fakeNodeClient{snapshot: snapshot}
	sink, failures := &measurementCollector{}, &failureCollector{}
	adapter := newESPAdapter(t, client, sink, failures, func() time.Time { return now })
	adapter.poll(context.Background(), adapter.endpoints[0])
	if sink.measurements[0].Quality != domain.QualityStale || sink.measurements[1].Quality != domain.QualityInvalid {
		t.Fatalf("qualities = %s, %s", sink.measurements[0].Quality, sink.measurements[1].Quality)
	}
}

func TestStartStopJoinsPoller(t *testing.T) {
	now := time.Now().UTC()
	client := &fakeNodeClient{snapshot: validSnapshot(now, "boot-a", 1, 25, 25)}
	adapter := newESPAdapter(t, client, &measurementCollector{}, &failureCollector{}, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	if err := adapter.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := adapter.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func validSnapshot(observed time.Time, boot string, sequence uint64, first, second float64) SnapshotDTO {
	return SnapshotDTO{SchemaVersion: "1.0", NodeID: string(espDevice), Firmware: "bench", BootID: boot, Sequence: sequence, ObservedAt: observed, Probes: []ProbeDTO{{SensorID: string(espProbeA), Celsius: &first, Valid: true}, {SensorID: string(espProbeB), Celsius: &second, Valid: true}}}
}
func newESPAdapter(t *testing.T, client Client, sink MeasurementSink, failures FailureSink, now func() time.Time) *Adapter {
	t.Helper()
	adapter, err := NewAdapter(client, sink, failures, slog.New(slog.NewTextHandler(io.Discard, nil)), now, Endpoint{ID: espEndpoint, DeviceID: espDevice, BaseURL: "http://esp32.local", ProbeIDs: [2]domain.SensorID{espProbeA, espProbeB}, PollInterval: time.Second, RequestTimeout: 100 * time.Millisecond, FreshFor: 5 * time.Second, MaximumClockSkew: time.Second, MaximumDifference: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}
