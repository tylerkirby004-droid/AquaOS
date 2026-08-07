package health_test

import (
	"context"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

type component struct {
	name   string
	status health.Status
}

func (c *component) Name() string                { return c.name }
func (c *component) Start(context.Context) error { return nil }
func (c *component) Stop(context.Context) error  { return nil }
func (c *component) Health() health.Status       { return c.status }

func TestReportDistinguishesLifecycleStates(t *testing.T) {
	fixed := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	manager := health.NewManager(health.WithClock(func() time.Time { return fixed }))
	required := &component{name: "required", status: health.NewStatus("required", health.StateHealthy, "", fixed)}
	optional := &component{name: "optional", status: health.NewStatus("optional", health.StateUnhealthy, "offline", fixed)}
	manager.RegisterComponent(required, true)
	manager.RegisterComponent(optional, false)

	before := manager.Report()
	if !before.Live || before.Ready || before.State != health.StateUnhealthy {
		t.Fatalf("stopped report = %+v", before)
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	degraded := manager.Report()
	if !degraded.Live || !degraded.Ready || degraded.State != health.StateDegraded {
		t.Fatalf("degraded report = %+v", degraded)
	}
	optional.status = health.NewStatus("optional", health.StateHealthy, "", fixed)
	if report := manager.Report(); !report.Ready || report.State != health.StateHealthy {
		t.Fatalf("healthy report = %+v", report)
	}
	required.status = health.NewStatus("required", health.StateUnhealthy, "failed", fixed)
	if report := manager.Report(); report.Ready || report.State != health.StateUnhealthy {
		t.Fatalf("unhealthy report = %+v", report)
	}
	if manager.Health().CheckedAt != fixed {
		t.Fatal("injected clock was not used")
	}
}

func TestNewStatusKeepsCompatibilityFlagConsistent(t *testing.T) {
	if status := health.NewStatus("test", health.StateDegraded, "", time.Time{}); status.Healthy {
		t.Fatal("degraded status reported healthy")
	}
}
