package domain

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// QuantityKind identifies the physical or logical quantity being measured.
type QuantityKind string

//nolint:revive // QuantityKind documents the closed set in this block.
const (
	QuantityTemperature QuantityKind = "temperature"
	QuantityPH          QuantityKind = "ph"
	QuantitySalinity    QuantityKind = "salinity"
	QuantityFlow        QuantityKind = "flow"
	QuantityLevel       QuantityKind = "level"
)

// Unit is a canonical unit identifier; values are normalized at boundaries.
type Unit string

//nolint:revive // Unit documents the canonical unit set in this block.
const (
	UnitCelsius       Unit = "celsius"
	UnitPH            Unit = "pH"
	UnitPPT           Unit = "ppt"
	UnitLitersPerHour Unit = "liters_per_hour"
	UnitPercent       Unit = "percent"
)

// Quality describes confidence in a measurement without changing its value.
type Quality string

//nolint:revive // Quality documents the closed set in this block.
const (
	QualityGood        Quality = "good"
	QualitySuspect     Quality = "suspect"
	QualityStale       Quality = "stale"
	QualityInvalid     Quality = "invalid"
	QualityUnavailable Quality = "unavailable"
)

// Quantity is a finite value paired with explicit kind and canonical unit.
type Quantity struct {
	Kind  QuantityKind `json:"kind"`
	Value float64      `json:"value"`
	Unit  Unit         `json:"unit"`
}

// NewQuantity validates and constructs a quantity.
func NewQuantity(kind QuantityKind, value float64, unit Unit) (Quantity, error) {
	quantity := Quantity{Kind: kind, Value: value, Unit: unit}
	if err := quantity.Validate(); err != nil {
		return Quantity{}, err
	}
	return quantity, nil
}

// Validate enforces finite values and compatible canonical units.
func (q Quantity) Validate() error {
	if math.IsNaN(q.Value) || math.IsInf(q.Value, 0) {
		return errors.New("quantity value must be finite")
	}
	want := map[QuantityKind]Unit{QuantityTemperature: UnitCelsius, QuantityPH: UnitPH, QuantitySalinity: UnitPPT, QuantityFlow: UnitLitersPerHour, QuantityLevel: UnitPercent}
	unit, ok := want[q.Kind]
	if !ok {
		return fmt.Errorf("unsupported quantity kind %q", q.Kind)
	}
	if q.Unit != unit {
		return fmt.Errorf("unit %q is incompatible with quantity kind %q", q.Unit, q.Kind)
	}
	if q.Kind == QuantityPH && (q.Value < 0 || q.Value > 14) {
		return errors.New("pH must be between 0 and 14")
	}
	if q.Kind == QuantityLevel && (q.Value < 0 || q.Value > 100) {
		return errors.New("level percent must be between 0 and 100")
	}
	return nil
}

// Measurement is one validated observation with explicit event and receipt time.
type Measurement struct {
	SensorID   SensorID      `json:"sensorId"`
	Value      Quantity      `json:"value"`
	Quality    Quality       `json:"quality"`
	ObservedAt time.Time     `json:"observedAt"`
	ReceivedAt time.Time     `json:"receivedAt"`
	FreshFor   time.Duration `json:"freshFor"`
}

// Validate checks measurement identity, timestamps, quality, and freshness bounds.
func (m Measurement) Validate() error {
	if err := m.SensorID.Validate(); err != nil {
		return fmt.Errorf("sensor ID: %w", err)
	}
	if err := m.Value.Validate(); err != nil {
		return fmt.Errorf("value: %w", err)
	}
	if !validQuality(m.Quality) {
		return fmt.Errorf("unsupported quality %q", m.Quality)
	}
	if m.ObservedAt.IsZero() || m.ReceivedAt.IsZero() {
		return errors.New("observedAt and receivedAt are required")
	}
	if m.ObservedAt.After(m.ReceivedAt) {
		return errors.New("observedAt must not be after receivedAt")
	}
	if m.FreshFor <= 0 {
		return errors.New("freshFor must be positive")
	}
	return nil
}

// EffectiveQuality evaluates freshness at the supplied injected time.
func (m Measurement) EffectiveQuality(now time.Time) Quality {
	if m.Quality == QualityInvalid || m.Quality == QualityUnavailable {
		return m.Quality
	}
	if !now.Before(m.ObservedAt.Add(m.FreshFor)) {
		return QualityStale
	}
	return m.Quality
}

func validQuality(value Quality) bool {
	return value == QualityGood || value == QualitySuspect || value == QualityStale || value == QualityInvalid || value == QualityUnavailable
}
