package domain

import (
	"math"
	"testing"
	"time"
)

func TestTypedIDsAndQuantityBoundaries(t *testing.T) {
	device, err := NewDeviceID()
	if err != nil || device.Validate() != nil {
		t.Fatalf("device ID=%q err=%v", device, err)
	}
	if _, err := NewQuantity(QuantityPH, 0, UnitPH); err != nil {
		t.Fatal(err)
	}
	if _, err := NewQuantity(QuantityPH, 14, UnitPH); err != nil {
		t.Fatal(err)
	}
	if _, err := NewQuantity(QuantityPH, 14.01, UnitPH); err == nil {
		t.Fatal("out-of-range pH accepted")
	}
	if _, err := NewQuantity(QuantityTemperature, math.NaN(), UnitCelsius); err == nil {
		t.Fatal("NaN accepted")
	}
	if _, err := NewQuantity(QuantityTemperature, 25, UnitPPT); err == nil {
		t.Fatal("incompatible unit accepted")
	}
}

func TestMeasurementFreshnessBoundary(t *testing.T) {
	id, _ := NewSensorID()
	now := time.Now().UTC()
	quantity, _ := NewQuantity(QuantityTemperature, 25, UnitCelsius)
	measurement := Measurement{SensorID: id, Value: quantity, Quality: QualityGood, ObservedAt: now, ReceivedAt: now, FreshFor: time.Minute}
	if err := measurement.Validate(); err != nil {
		t.Fatal(err)
	}
	if quality := measurement.EffectiveQuality(now.Add(time.Minute)); quality != QualityStale {
		t.Fatalf("quality=%s", quality)
	}
	measurement.ObservedAt = now.Add(time.Second)
	if err := measurement.Validate(); err == nil {
		t.Fatal("future observation accepted")
	}
}

func TestCapabilityInvariants(t *testing.T) {
	if err := ValidateCapabilities([]Capability{CapabilityObserve, CapabilityObserve}); err == nil {
		t.Fatal("duplicate capability accepted")
	}
	if err := Capability("vendor.magic").Validate(); err == nil {
		t.Fatal("unknown capability accepted")
	}
}
