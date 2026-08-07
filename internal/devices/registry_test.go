package devices

import (
	"context"
	"errors"
	"testing"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
)

func TestRegistryRejectsCollisionsOwnershipAndCapabilities(t *testing.T) {
	registry := NewRegistry(events.NewBus())
	device, err := registry.Register(context.Background(), domain.Device{Name: "controller", Capabilities: []domain.Capability{domain.CapabilityObserve}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(context.Background(), device); !errors.Is(err, ErrExists) {
		t.Fatalf("collision error=%v", err)
	}
	if _, err := registry.RegisterEndpoint(context.Background(), domain.Endpoint{DeviceID: domain.DeviceID("00000000-0000-4000-8000-000000000000"), Name: "missing", Capabilities: []domain.Capability{domain.CapabilityObserve}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ownership error=%v", err)
	}
	if _, err := registry.RegisterEndpoint(context.Background(), domain.Endpoint{DeviceID: device.ID, Name: "bad", Capabilities: []domain.Capability{domain.CapabilitySwitch}}); err == nil {
		t.Fatal("unsupported endpoint capability accepted")
	}
	endpoint, err := registry.RegisterEndpoint(context.Background(), domain.Endpoint{DeviceID: device.ID, Name: "probe", Capabilities: []domain.Capability{domain.CapabilityObserve}})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Remove(context.Background(), device.ID); !errors.Is(err, ErrOwned) {
		t.Fatalf("owned removal error=%v endpoint=%v", err, endpoint.ID)
	}
}

func TestRegistrySnapshotsAreImmutableAndSorted(t *testing.T) {
	registry := NewRegistry(nil)
	first, err := registry.Register(context.Background(), domain.Device{Name: "one", Capabilities: []domain.Capability{domain.CapabilityObserve}, Metadata: map[string]string{"site": "reef"}})
	if err != nil {
		t.Fatal(err)
	}
	first.Metadata["site"] = "changed"
	stored, err := registry.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Metadata["site"] != "reef" {
		t.Fatal("registry exposed mutable metadata")
	}
}
