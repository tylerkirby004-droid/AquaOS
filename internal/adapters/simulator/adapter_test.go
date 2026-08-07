package simulator

import (
	"context"
	"testing"
)

func TestAdapterLifecycleHasNoBackgroundWork(t *testing.T) {
	adapter := New()
	if err := adapter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !adapter.Health().Healthy {
		t.Fatal("started simulator is unhealthy")
	}
	if err := adapter.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
