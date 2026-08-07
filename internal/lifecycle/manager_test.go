package lifecycle

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

type fakeComponent struct {
	name     string
	calls    *[]string
	startErr error
}

func (f *fakeComponent) Name() string { return f.name }
func (f *fakeComponent) Start(context.Context) error {
	*f.calls = append(*f.calls, "start "+f.name)
	return f.startErr
}

type workerComponent struct {
	started chan struct{}
	done    chan struct{}
}

func (w *workerComponent) Name() string { return "worker" }
func (w *workerComponent) Start(ctx context.Context) error {
	go func() { close(w.started); <-ctx.Done(); close(w.done) }()
	return nil
}
func (w *workerComponent) Stop(ctx context.Context) error {
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (w *workerComponent) Health() health.Status {
	return health.NewStatus(w.Name(), health.StateHealthy, "", time.Now())
}
func (f *fakeComponent) Stop(context.Context) error {
	*f.calls = append(*f.calls, "stop "+f.name)
	return nil
}

func TestStopCancelsAndJoinsWorkers(t *testing.T) {
	worker := &workerComponent{started: make(chan struct{}), done: make(chan struct{})}
	manager := NewConfigured(slog.New(slog.NewTextHandler(io.Discard, nil)), Timeouts{Startup: time.Second, Shutdown: time.Second, Component: 500 * time.Millisecond}, worker)
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.Start(root); err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-worker.done:
	default:
		t.Fatal("worker remained after shutdown")
	}
}

func TestStartTimeoutCancelsBlockedComponent(t *testing.T) {
	blocked := &blockingComponent{done: make(chan struct{})}
	manager := NewConfigured(slog.New(slog.NewTextHandler(io.Discard, nil)), Timeouts{Startup: time.Second, Shutdown: time.Second, Component: 10 * time.Millisecond}, blocked)
	if err := manager.Start(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-blocked.done:
	default:
		t.Fatal("blocked start worker was not joined")
	}
}

type blockingComponent struct{ done chan struct{} }

func (b *blockingComponent) Name() string { return "blocked" }
func (b *blockingComponent) Start(ctx context.Context) error {
	<-ctx.Done()
	close(b.done)
	return ctx.Err()
}
func (b *blockingComponent) Stop(context.Context) error { return nil }
func (b *blockingComponent) Health() health.Status {
	return health.NewStatus(b.Name(), health.StateUnhealthy, "", time.Now())
}
func (f *fakeComponent) Health() health.Status {
	return health.Status{Name: f.name, Healthy: true, CheckedAt: time.Now()}
}

func TestStartFailureRollsBackInReverseOrder(t *testing.T) {
	var calls []string
	wantErr := errors.New("broken")
	manager := New(slog.New(slog.NewTextHandler(io.Discard, nil)),
		&fakeComponent{name: "one", calls: &calls},
		&fakeComponent{name: "two", calls: &calls, startErr: wantErr},
	)
	if err := manager.Start(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Start() error = %v", err)
	}
	want := []string{"start one", "start two", "stop one"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v", calls)
		}
	}
}
