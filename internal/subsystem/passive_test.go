package subsystem

import (
	"context"
	"testing"
)

func TestPassiveLifecycle(t *testing.T) {
	component := NewPassive("storage")
	if component.Health().Healthy {
		t.Fatal("new component is healthy")
	}
	_ = component.Start(context.Background())
	if !component.Health().Healthy {
		t.Fatal("started component is unhealthy")
	}
	_ = component.Stop(context.Background())
	if component.Health().Healthy {
		t.Fatal("stopped component is healthy")
	}
}
