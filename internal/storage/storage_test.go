package storage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
)

type fakeClient struct {
	mu       sync.Mutex
	failures int
	batches  [][]Point
	notify   chan struct{}
}

func (f *fakeClient) Write(_ context.Context, points []Point) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures > 0 {
		f.failures--
		return errors.New("offline")
	}
	f.batches = append(f.batches, points)
	select {
	case f.notify <- struct{}{}:
	default:
	}
	return nil
}
func testPoint() Point {
	return Point{Measurement: "aquaos_measurements_v1", Tags: map[string]string{"entity_id": "sensor-1"}, Fields: map[string]Field{"value": FloatField(25.1)}, Timestamp: time.Unix(100, 0).UTC()}
}
func testWriter(t *testing.T, client Client) *Writer {
	t.Helper()
	writer, err := New(Config{QueueCapacity: 4, BatchSize: 1, FlushInterval: 10 * time.Millisecond, RetryMinimum: 5 * time.Millisecond, RetryMaximum: 20 * time.Millisecond, WriteTimeout: 100 * time.Millisecond}, client, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return writer
}

func TestWriterRecoversFromOutageWithBoundedRetry(t *testing.T) {
	client := &fakeClient{failures: 1, notify: make(chan struct{}, 1)}
	writer := testWriter(t, client)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := writer.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := writer.Enqueue(testPoint()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.notify:
	case <-time.After(time.Second):
		t.Fatal("write did not recover")
	}
	metrics := writer.Metrics()
	if metrics.Written != 1 || metrics.Failures == 0 || metrics.Retries == 0 {
		t.Fatalf("metrics = %+v", metrics)
	}
	if writer.Health().State != "healthy" {
		t.Fatalf("health = %+v", writer.Health())
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := writer.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func TestQueueOverflowDropsNewestWithoutBlocking(t *testing.T) {
	writer := testWriter(t, &fakeClient{})
	writer.mu.Lock()
	writer.running = true
	writer.mu.Unlock()
	if err := writer.Enqueue(testPoint()); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < cap(writer.queue)-1; index++ {
		writer.queue <- testPoint()
	}
	if err := writer.Enqueue(testPoint()); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("error = %v", err)
	}
	if writer.Metrics().Dropped != 1 {
		t.Fatal("drop was not observable")
	}
}

func TestEventSinkNeverPropagatesStorageFailure(t *testing.T) {
	writer := testWriter(t, &fakeClient{})
	sink, err := NewEventSink(writer, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	event, err := events.New("sensor", events.StateChanged, events.SeverityInfo, map[string]string{"invalid": "payload"}, domain.CorrelationID("11111111-1111-4111-8111-111111111111"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Handle(context.Background(), event); err != nil {
		t.Fatalf("optional failure reached publisher: %v", err)
	}
}

func TestInfluxClientWritesVersionedLineProtocolAndToken(t *testing.T) {
	var body, authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		body = string(payload)
		authorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewInfluxClient(InfluxConfig{URL: server.URL, Organization: "reef", Bucket: "history", Token: "secret-token"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Write(context.Background(), []Point{testPoint()}); err != nil {
		t.Fatal(err)
	}
	if authorization != "Token secret-token" {
		t.Fatalf("authorization = %q", authorization)
	}
	if !strings.Contains(body, "aquaos_measurements_v1,entity_id=sensor-1 value=25.1 100000000000") {
		t.Fatalf("line protocol = %q", body)
	}
}

func TestInfluxHTTPFailureIsExplicit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := NewInfluxClient(InfluxConfig{URL: server.URL, Organization: "reef", Bucket: "history", Token: "token"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Write(context.Background(), []Point{testPoint()}); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("error = %v", err)
	}
}
