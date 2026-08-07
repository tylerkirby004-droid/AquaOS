package domain

import (
	"errors"
	"fmt"
)

// Capability is a stable transport-neutral behavior identifier.
type Capability string

//nolint:revive // Capability documents the closed set in this block.
const (
	CapabilityObserve                Capability = "observe"
	CapabilitySwitch                 Capability = "switch"
	CapabilityVariableOutput         Capability = "variable-output"
	CapabilityCommandAcknowledgement Capability = "command-acknowledgement"
	CapabilityReportedState          Capability = "reported-state"
	CapabilityPowerTelemetry         Capability = "power-telemetry"
)

// Validate rejects unknown capabilities until their domain contract exists.
func (c Capability) Validate() error {
	if c != CapabilityObserve && c != CapabilitySwitch && c != CapabilityVariableOutput && c != CapabilityCommandAcknowledgement && c != CapabilityReportedState && c != CapabilityPowerTelemetry {
		return fmt.Errorf("unsupported capability %q", c)
	}
	return nil
}

// Device describes a physical or virtual controller without protocol details.
type Device struct {
	ID           DeviceID          `json:"id"`
	Name         string            `json:"name"`
	Capabilities []Capability      `json:"capabilities"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// Endpoint is an adapter-owned endpoint associated with one device.
type Endpoint struct {
	ID           EndpointID   `json:"id"`
	DeviceID     DeviceID     `json:"deviceId"`
	Name         string       `json:"name"`
	Capabilities []Capability `json:"capabilities"`
}

// Sensor describes a generic observation endpoint and its owner.
type Sensor struct {
	ID           SensorID          `json:"id"`
	DeviceID     DeviceID          `json:"deviceId"`
	EndpointID   EndpointID        `json:"endpointId"`
	Name         string            `json:"name"`
	Quantity     QuantityKind      `json:"quantity"`
	Unit         Unit              `json:"unit"`
	Capabilities []Capability      `json:"capabilities"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// Equipment describes a generic logical output and its owning endpoint.
type Equipment struct {
	ID           EquipmentID       `json:"id"`
	DeviceID     DeviceID          `json:"deviceId"`
	EndpointID   EndpointID        `json:"endpointId"`
	Name         string            `json:"name"`
	Capabilities []Capability      `json:"capabilities"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// ValidateCapabilities rejects duplicates, empty sets, and unsupported values.
func ValidateCapabilities(values []Capability) error {
	if len(values) == 0 {
		return errors.New("at least one capability is required")
	}
	seen := make(map[Capability]struct{}, len(values))
	for _, capability := range values {
		if err := capability.Validate(); err != nil {
			return err
		}
		if _, exists := seen[capability]; exists {
			return fmt.Errorf("duplicate capability %q", capability)
		}
		seen[capability] = struct{}{}
	}
	return nil
}

// SupportsAll reports whether available capabilities include every requirement.
func SupportsAll(available, required []Capability) bool {
	set := make(map[Capability]struct{}, len(available))
	for _, value := range available {
		set[value] = struct{}{}
	}
	for _, value := range required {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}
