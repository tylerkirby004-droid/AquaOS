package sensors

import (
	"context"
	"testing"

	"github.com/tylerkirby004-droid/aquaos/internal/devices"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
)

func TestRegistryValidatesOwnershipUnitAndCapabilities(t *testing.T) {
	ctx := context.Background()
	owners := devices.NewRegistry(nil)
	device, err := owners.Register(ctx, domain.Device{Name: "controller", Capabilities: []domain.Capability{domain.CapabilityObserve}})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := owners.RegisterEndpoint(ctx, domain.Endpoint{DeviceID: device.ID, Name: "probe", Capabilities: []domain.Capability{domain.CapabilityObserve}})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(events.NewBus(), owners)
	sensor := domain.Sensor{DeviceID: device.ID, EndpointID: endpoint.ID, Name: "temperature", Quantity: domain.QuantityTemperature, Unit: domain.UnitCelsius, Capabilities: []domain.Capability{domain.CapabilityObserve}}
	registered, err := registry.Register(ctx, sensor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Get(ctx, registered.ID); err != nil {
		t.Fatal(err)
	}
	sensor.Unit = domain.UnitPPT
	if _, err := registry.Register(ctx, sensor); err == nil {
		t.Fatal("incompatible unit accepted")
	}
	sensor.Unit = domain.UnitCelsius
	sensor.EndpointID = domain.EndpointID("00000000-0000-4000-8000-000000000000")
	if _, err := registry.Register(ctx, sensor); err == nil {
		t.Fatal("unknown owner accepted")
	}
}
