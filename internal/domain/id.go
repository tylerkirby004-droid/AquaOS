package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
)

// DeviceID uniquely identifies a physical or virtual device.
type DeviceID string

// SensorID uniquely identifies a measurement source.
type SensorID string

// EquipmentID uniquely identifies a controllable logical endpoint.
type EquipmentID string

// EndpointID uniquely identifies an adapter endpoint owning observations or commands.
type EndpointID string

// EntityID is the canonical textual form used only where heterogeneous state keys require one ID type.
type EntityID string

// AlarmID uniquely identifies one alarm instance.
type AlarmID string

// RuleID uniquely identifies one alarm evaluation rule.
type RuleID string

// CommandID uniquely identifies one requested operation.
type CommandID string

// OverrideID uniquely identifies a temporary operator override.
type OverrideID string

// CorrelationID links work caused by one initiating operation.
type CorrelationID string

// Revision is a monotonically increasing local canonical-state revision.
type Revision uint64

// NewDeviceID creates a random RFC 4122 version 4 device ID.
func NewDeviceID() (DeviceID, error) { value, err := newUUID(); return DeviceID(value), err }

// NewSensorID creates a random RFC 4122 version 4 sensor ID.
func NewSensorID() (SensorID, error) { value, err := newUUID(); return SensorID(value), err }

// NewEquipmentID creates a random RFC 4122 version 4 equipment ID.
func NewEquipmentID() (EquipmentID, error) { value, err := newUUID(); return EquipmentID(value), err }

// NewEndpointID creates a random RFC 4122 version 4 adapter endpoint ID.
func NewEndpointID() (EndpointID, error) { value, err := newUUID(); return EndpointID(value), err }

// NewAlarmID creates a random RFC 4122 version 4 alarm ID.
func NewAlarmID() (AlarmID, error) { value, err := newUUID(); return AlarmID(value), err }

// NewRuleID creates a random RFC 4122 version 4 rule ID.
func NewRuleID() (RuleID, error) { value, err := newUUID(); return RuleID(value), err }

// NewCommandID creates a random RFC 4122 version 4 command ID.
func NewCommandID() (CommandID, error) { value, err := newUUID(); return CommandID(value), err }

// NewOverrideID creates a random RFC 4122 version 4 override ID.
func NewOverrideID() (OverrideID, error) { value, err := newUUID(); return OverrideID(value), err }

// NewCorrelationID creates a random RFC 4122 version 4 correlation ID.
func NewCorrelationID() (CorrelationID, error) {
	value, err := newUUID()
	return CorrelationID(value), err
}

// Validate verifies the canonical UUID representation.
func (id DeviceID) Validate() error { return validateUUID(string(id)) }

// Validate verifies the canonical UUID representation.
func (id SensorID) Validate() error { return validateUUID(string(id)) }

// Validate verifies the canonical UUID representation.
func (id EquipmentID) Validate() error { return validateUUID(string(id)) }

// Validate verifies the canonical UUID representation.
func (id EndpointID) Validate() error { return validateUUID(string(id)) }

// Validate verifies the canonical UUID representation.
func (id EntityID) Validate() error { return validateUUID(string(id)) }

// Validate verifies the canonical UUID representation.
func (id AlarmID) Validate() error { return validateUUID(string(id)) }

// Validate verifies the canonical UUID representation.
func (id RuleID) Validate() error { return validateUUID(string(id)) }

// Validate verifies the canonical UUID representation.
func (id CommandID) Validate() error { return validateUUID(string(id)) }

// Validate verifies the canonical UUID representation.
func (id OverrideID) Validate() error { return validateUUID(string(id)) }

// Validate verifies the canonical UUID representation.
func (id CorrelationID) Validate() error { return validateUUID(string(id)) }

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	buffer := make([]byte, 36)
	hex.Encode(buffer[0:8], value[0:4])
	buffer[8] = '-'
	hex.Encode(buffer[9:13], value[4:6])
	buffer[13] = '-'
	hex.Encode(buffer[14:18], value[6:8])
	buffer[18] = '-'
	hex.Encode(buffer[19:23], value[8:10])
	buffer[23] = '-'
	hex.Encode(buffer[24:36], value[10:16])
	return string(buffer), nil
}

func validateUUID(value string) error {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return errors.New("must be a canonical RFC 4122 UUID")
	}
	if _, err := hex.DecodeString(strings.ReplaceAll(value, "-", "")); err != nil {
		return errors.New("must be a canonical RFC 4122 UUID")
	}
	return nil
}
