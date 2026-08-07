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

type failingOptional struct{ stops int }

func (*failingOptional) Name() string                 { return "optional-test" }
func (*failingOptional) Start(context.Context) error  { return errors.New("offline") }
func (f *failingOptional) Stop(context.Context) error { f.stops++; return nil }
func (*failingOptional) Health() health.Status {
	return health.NewStatus("optional-test", health.StateUnhealthy, "offline", time.Now())
}
func TestOptionalFailureDoesNotFailCriticalLifecycle(t *testing.T) {
	component := &failingOptional{}
	wrapper := NewOptional(component, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := wrapper.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if component.stops != 0 {
		t.Fatal("failed optional component was stopped as if started")
	}
	if wrapper.Health().State != health.StateUnhealthy {
		t.Fatal("degraded health was hidden")
	}
}
