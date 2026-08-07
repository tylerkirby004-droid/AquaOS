// Package storage implements optional, bounded historical persistence. It is
// never consulted by authoritative control or safety decisions.
package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

// Storage is the lifecycle and enqueue boundary for historical persistence.
type Storage interface {
	health.Component
	Enqueue(Point) error
	Metrics() Metrics
}

// Disabled is the healthy no-op implementation used when persistence is off.
type Disabled struct{}

// NewDisabled constructs disabled optional storage.
func NewDisabled() *Disabled { return &Disabled{} }

// Name returns the lifecycle component name.
func (*Disabled) Name() string { return "storage" }

// Start is a no-op.
func (*Disabled) Start(context.Context) error { return nil }

// Stop is a no-op.
func (*Disabled) Stop(context.Context) error { return nil }

// Health reports healthy because explicitly disabled storage is not degraded.
func (*Disabled) Health() health.Status {
	return health.NewStatus("storage", health.StateHealthy, "disabled", time.Now().UTC())
}

// Enqueue rejects records because storage is disabled.
func (*Disabled) Enqueue(Point) error { return ErrStopped }

// Metrics returns zero counters.
func (*Disabled) Metrics() Metrics { return Metrics{} }

// Field is exactly one supported Influx field value.
type Field struct {
	Float   *float64
	Boolean *bool
	String  *string
}

// FloatField constructs a floating-point field.
func FloatField(value float64) Field { return Field{Float: &value} }

// BooleanField constructs a Boolean field.
func BooleanField(value bool) Field { return Field{Boolean: &value} }

// StringField constructs a string field.
func StringField(value string) Field { return Field{String: &value} }

// Point is a versioned, bounded-cardinality time-series record.
type Point struct {
	Measurement string
	Tags        map[string]string
	Fields      map[string]Field
	Timestamp   time.Time
}

// Client writes one bounded batch to a storage backend.
type Client interface {
	Write(context.Context, []Point) error
}

// Config contains externally bounded writer behavior.
type Config struct {
	QueueCapacity int
	BatchSize     int
	FlushInterval time.Duration
	RetryMinimum  time.Duration
	RetryMaximum  time.Duration
	WriteTimeout  time.Duration
}

// Metrics is a lock-free snapshot of storage behavior.
type Metrics struct {
	Enqueued   uint64 `json:"enqueued"`
	Written    uint64 `json:"written"`
	Dropped    uint64 `json:"dropped"`
	Retries    uint64 `json:"retries"`
	Failures   uint64 `json:"failures"`
	QueueDepth int    `json:"queueDepth"`
}

var (
	// ErrQueueFull reports the documented drop-newest policy.
	ErrQueueFull = errors.New("storage queue is full; newest point dropped")
	// ErrStopped reports enqueue after shutdown.
	ErrStopped = errors.New("storage writer is stopped")
)

// Writer owns one explicit context-cancellable batching goroutine.
type Writer struct {
	cfg      Config
	client   Client
	logger   *slog.Logger
	queue    chan Point
	mu       sync.RWMutex
	cancel   context.CancelFunc
	done     chan struct{}
	running  bool
	lastErr  error
	enqueued atomic.Uint64
	written  atomic.Uint64
	dropped  atomic.Uint64
	retries  atomic.Uint64
	failures atomic.Uint64
}

// New constructs a bounded optional writer.
func New(cfg Config, client Client, logger *slog.Logger) (*Writer, error) {
	if client == nil || logger == nil {
		return nil, errors.New("storage client and logger are required")
	}
	if cfg.QueueCapacity < 1 || cfg.QueueCapacity > 1_000_000 || cfg.BatchSize < 1 || cfg.BatchSize > cfg.QueueCapacity {
		return nil, errors.New("storage queue and batch bounds are invalid")
	}
	if cfg.FlushInterval <= 0 || cfg.RetryMinimum <= 0 || cfg.RetryMaximum < cfg.RetryMinimum || cfg.WriteTimeout <= 0 {
		return nil, errors.New("storage timing bounds are invalid")
	}
	return &Writer{cfg: cfg, client: client, logger: logger, queue: make(chan Point, cfg.QueueCapacity)}, nil
}

// Name returns the lifecycle component name.
func (*Writer) Name() string { return "storage" }

// Start launches the sole owned writer goroutine.
func (w *Writer) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return errors.New("storage writer already started")
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.done = make(chan struct{})
	w.running = true
	go w.run(runCtx, w.done)
	return nil
}

// Stop cancels and joins the writer. A final batch attempt remains bounded.
func (w *Writer) Stop(ctx context.Context) error {
	w.mu.Lock()
	cancel, done := w.cancel, w.done
	w.cancel = nil
	w.done = nil
	w.running = false
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Health reports degradation without making storage authoritative.
func (w *Writer) Health() health.Status {
	w.mu.RLock()
	running, lastErr := w.running, w.lastErr
	w.mu.RUnlock()
	state := health.StateUnhealthy
	message := ""
	if running && lastErr == nil {
		state = health.StateHealthy
	}
	if lastErr != nil {
		message = lastErr.Error()
	}
	return health.NewStatus(w.Name(), state, message, time.Now().UTC())
}

// Enqueue validates and non-blockingly queues a point. Overflow drops newest.
func (w *Writer) Enqueue(point Point) error {
	if err := validatePoint(point); err != nil {
		return err
	}
	w.mu.RLock()
	running := w.running
	w.mu.RUnlock()
	if !running {
		return ErrStopped
	}
	select {
	case w.queue <- clonePoint(point):
		w.enqueued.Add(1)
		return nil
	default:
		w.dropped.Add(1)
		return ErrQueueFull
	}
}

// Metrics returns operational counters and current bounded depth.
func (w *Writer) Metrics() Metrics {
	return Metrics{Enqueued: w.enqueued.Load(), Written: w.written.Load(), Dropped: w.dropped.Load(), Retries: w.retries.Load(), Failures: w.failures.Load(), QueueDepth: len(w.queue)}
}

func (w *Writer) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(w.cfg.FlushInterval)
	defer ticker.Stop()
	batch := make([]Point, 0, w.cfg.BatchSize)
	backoff := w.cfg.RetryMinimum
	for {
		if len(batch) == w.cfg.BatchSize {
			if w.write(ctx, batch) == nil {
				batch = batch[:0]
				backoff = w.cfg.RetryMinimum
			} else if !wait(ctx, backoff) {
				w.final(batch)
				return
			} else {
				w.retries.Add(1)
				backoff *= 2
				if backoff > w.cfg.RetryMaximum {
					backoff = w.cfg.RetryMaximum
				}
			}
			continue
		}
		select {
		case <-ctx.Done():
			for len(batch) < w.cfg.BatchSize {
				select {
				case point := <-w.queue:
					batch = append(batch, point)
				default:
					w.final(batch)
					return
				}
			}
			w.final(batch)
			return
		case point := <-w.queue:
			batch = append(batch, point)
		case <-ticker.C:
			if len(batch) > 0 {
				if w.write(ctx, batch) == nil {
					batch = batch[:0]
					backoff = w.cfg.RetryMinimum
				} else {
					w.retries.Add(1)
				}
			}
		}
	}
}
func (w *Writer) write(parent context.Context, batch []Point) error {
	ctx, cancel := context.WithTimeout(parent, w.cfg.WriteTimeout)
	defer cancel()
	err := w.client.Write(ctx, append([]Point(nil), batch...))
	w.mu.Lock()
	w.lastErr = err
	w.mu.Unlock()
	if err != nil {
		w.failures.Add(1)
		w.logger.WarnContext(parent, "optional storage write failed", "error", err, "batch_size", len(batch))
		return err
	}
	w.written.Add(uint64(len(batch)))
	return nil
}
func (w *Writer) final(batch []Point) {
	if len(batch) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), w.cfg.WriteTimeout)
	defer cancel()
	_ = w.write(ctx, batch)
}
func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func validatePoint(point Point) error {
	if point.Measurement == "" || point.Timestamp.IsZero() || len(point.Fields) == 0 {
		return errors.New("measurement, timestamp, and fields are required")
	}
	if len(point.Tags) > 16 || len(point.Fields) > 32 {
		return errors.New("point exceeds tag or field count limit")
	}
	for key, value := range point.Tags {
		if key == "" || value == "" || len(key) > 64 || len(value) > 128 {
			return fmt.Errorf("invalid tag %q", key)
		}
	}
	for key, value := range point.Fields {
		count := 0
		if value.Float != nil {
			count++
		}
		if value.Boolean != nil {
			count++
		}
		if value.String != nil {
			count++
		}
		if key == "" || len(key) > 64 || count != 1 {
			return fmt.Errorf("invalid field %q", key)
		}
	}
	return nil
}
func clonePoint(point Point) Point {
	result := point
	result.Tags = make(map[string]string, len(point.Tags))
	for key, value := range point.Tags {
		result.Tags[key] = value
	}
	result.Fields = make(map[string]Field, len(point.Fields))
	for key, value := range point.Fields {
		clonedField := value
		if value.Float != nil {
			v := *value.Float
			clonedField.Float = &v
		}
		if value.Boolean != nil {
			v := *value.Boolean
			clonedField.Boolean = &v
		}
		if value.String != nil {
			v := *value.String
			clonedField.String = &v
		}
		result.Fields[key] = clonedField
	}
	return result
}
func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
