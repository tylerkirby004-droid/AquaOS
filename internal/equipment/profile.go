package equipment

import (
	"errors"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

// Kind identifies a generic equipment safety profile, not a hardware protocol.
type Kind string

//nolint:revive // Kind values are documented collectively by Kind.
const (
	KindOutlet          Kind = "outlet"
	KindHeater          Kind = "heater"
	KindReturnPump      Kind = "return-pump"
	KindCirculationPump Kind = "circulation-pump"
	KindATO             Kind = "ato"
	KindDosingPump      Kind = "dosing-pump"
)

// InputRequirement declares canonical state required before hazardous activation.
type InputRequirement struct {
	Key            state.Key `json:"key"`
	RequireBoolean *bool     `json:"requireBoolean,omitempty"`
}

// Limits contains hard safety constraints. A zero duration disables a constraint
// only where explicitly allowed by Validate; callers must supply these externally.
type Limits struct {
	MaximumOn      time.Duration `json:"maximumOn"`
	MaximumDailyOn time.Duration `json:"maximumDailyOn"`
	MinimumOff     time.Duration `json:"minimumOff"`
}

// Profile binds generic equipment identity to its safety constraints.
type Profile struct {
	EquipmentID    domain.EquipmentID  `json:"equipmentId"`
	Kind           Kind                `json:"kind"`
	Hazardous      bool                `json:"hazardous"`
	FailSafeOn     bool                `json:"failSafeOn"`
	Capabilities   []domain.Capability `json:"capabilities"`
	Limits         Limits              `json:"limits"`
	RequiredInputs []InputRequirement  `json:"requiredInputs,omitempty"`
}

// Validate rejects incomplete or unsafe profiles.
func (p Profile) Validate() error {
	if err := p.EquipmentID.Validate(); err != nil {
		return err
	}
	if err := domain.ValidateCapabilities(p.Capabilities); err != nil {
		return err
	}
	if !domain.SupportsAll(p.Capabilities, []domain.Capability{domain.CapabilitySwitch, domain.CapabilityCommandAcknowledgement, domain.CapabilityReportedState}) {
		return errors.New("controlled equipment requires switch, acknowledgement, and reported-state capabilities")
	}
	switch p.Kind {
	case KindOutlet, KindReturnPump, KindCirculationPump:
	case KindHeater, KindATO, KindDosingPump:
		if !p.Hazardous {
			return errors.New("heater, ATO, and dosing profiles must be hazardous")
		}
		if p.Limits.MaximumOn <= 0 {
			return errors.New("hazardous equipment requires a positive maximum-on limit")
		}
		if len(p.RequiredInputs) == 0 {
			return errors.New("hazardous equipment requires at least one safety input")
		}
	default:
		return errors.New("unsupported equipment kind")
	}
	if p.Limits.MaximumOn < 0 || p.Limits.MaximumDailyOn < 0 || p.Limits.MinimumOff < 0 {
		return errors.New("equipment limits cannot be negative")
	}
	if p.Limits.MaximumDailyOn > 0 && p.Limits.MaximumOn > p.Limits.MaximumDailyOn {
		return errors.New("maximum-on cannot exceed maximum-daily-on")
	}
	if p.FailSafeOn && p.Hazardous {
		return errors.New("hazardous equipment cannot fail safe on")
	}
	if p.Kind == KindDosingPump && p.Limits.MaximumDailyOn <= 0 {
		return errors.New("dosing equipment requires a positive maximum-daily-on limit")
	}
	for _, requirement := range p.RequiredInputs {
		if requirement.Key.EntityKind != state.EntitySensor || requirement.Key.Plane != state.PlaneObservation || requirement.Key.Attribute == "" {
			return errors.New("safety inputs must reference a sensor observation attribute")
		}
		if err := requirement.Key.EntityID.Validate(); err != nil {
			return errors.New("safety input entity ID is invalid")
		}
	}
	return nil
}

// Clone returns a pointer-independent profile copy.
func (p Profile) Clone() Profile {
	p.Capabilities = append([]domain.Capability(nil), p.Capabilities...)
	p.RequiredInputs = append([]InputRequirement(nil), p.RequiredInputs...)
	for i := range p.RequiredInputs {
		if p.RequiredInputs[i].RequireBoolean != nil {
			value := *p.RequiredInputs[i].RequireBoolean
			p.RequiredInputs[i].RequireBoolean = &value
		}
	}
	return p
}
