// Package plugins defines compile-time extension points. AquaOS plugins are
// injected Go components, not dynamically loaded shared objects: this keeps
// versioning, startup, health, and failure behavior visible and testable.
package plugins

import (
	"context"

	"github.com/tylerkirby004-droid/aquaos/internal/alarms"
	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/devices"
	"github.com/tylerkirby004-droid/aquaos/internal/equipment"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
	"github.com/tylerkirby004-droid/aquaos/internal/sensors"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

// Manifest describes a compiled plugin without initializing it.
type Manifest struct {
	ID           string
	Name         string
	Version      string
	Description  string
	Capabilities []string
}

// Host exposes only stable service interfaces. Plugins communicate across
// domains by publishing events; registry references are for their owned data.
type Host interface {
	Devices() devices.DeviceRegistry
	Sensors() sensors.SensorRegistry
	Equipment() equipment.EquipmentRegistry
	State() state.StateManager
	Alarms() alarms.AlarmManager
	Configuration() config.ConfigurationManager
	Events() events.Publisher
	Subscribe(events.Type, events.Handler) (events.Subscription, error)
}

// Plugin is a lifecycle-aware, compile-time AquaOS extension.
type Plugin interface {
	health.Component
	Manifest() Manifest
	Initialize(context.Context, Host) error
}

// Factory makes plugin construction explicit in the composition root and
// provides a future seam for configuration-based enablement.
// Factory constructs a plugin without hiding its creation error.
type Factory interface{ Create() (Plugin, error) }
