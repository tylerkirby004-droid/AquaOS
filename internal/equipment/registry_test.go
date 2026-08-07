package equipment

import (
	"context"
	"testing"

	"github.com/tylerkirby004-droid/aquaos/internal/devices"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

func TestRegistryValidatesOwnershipAndCapabilities(t *testing.T) {
	ctx := context.Background()
	owners := devices.NewRegistry(nil)
	device, err := owners.Register(ctx, domain.Device{Name: "controller", Capabilities: []domain.Capability{domain.CapabilitySwitch}})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := owners.RegisterEndpoint(ctx, domain.Endpoint{DeviceID: device.ID, Name: "outlet", Capabilities: []domain.Capability{domain.CapabilitySwitch}})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(nil, owners)
	item := domain.Equipment{DeviceID: device.ID, EndpointID: endpoint.ID, Name: "generic output", Capabilities: []domain.Capability{domain.CapabilitySwitch}}
	registered, err := registry.Register(ctx, item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get(ctx, registered.ID); err != nil {
		t.Fatal(err)
	}
	item.Capabilities = []domain.Capability{domain.CapabilityVariableOutput}
	if _, err := registry.Register(ctx, item); err == nil {
		t.Fatal("capability outside owner accepted")
	}
}
