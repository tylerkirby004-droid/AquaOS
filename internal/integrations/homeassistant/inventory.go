package homeassistant

import (
	"context"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

// DeviceLister supplies stable device inventory.
type DeviceLister interface {
	List(context.Context) ([]domain.Device, error)
}

// SensorLister supplies stable sensor inventory.
type SensorLister interface {
	List(context.Context) ([]domain.Sensor, error)
}

// EquipmentLister supplies stable equipment inventory.
type EquipmentLister interface {
	List(context.Context) ([]domain.Equipment, error)
}

// RegistryInventory adapts focused registries to the discovery inventory port.
type RegistryInventory struct {
	DeviceRegistry    DeviceLister
	SensorRegistry    SensorLister
	EquipmentRegistry EquipmentLister
}

// Devices returns devices through the configured registry.
func (r RegistryInventory) Devices(ctx context.Context) ([]domain.Device, error) {
	return r.DeviceRegistry.List(ctx)
}

// Sensors returns sensors through the configured registry.
func (r RegistryInventory) Sensors(ctx context.Context) ([]domain.Sensor, error) {
	return r.SensorRegistry.List(ctx)
}

// Equipment returns equipment through the configured registry.
func (r RegistryInventory) Equipment(ctx context.Context) ([]domain.Equipment, error) {
	return r.EquipmentRegistry.List(ctx)
}
