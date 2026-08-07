package alarms

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

func TestEvaluatorAppliesCalibrationAndRaisesConfiguredThreshold(t *testing.T) {
	bus := events.NewBus()
	manager := NewManager(bus, slog.New(slog.NewTextHandler(io.Discard, nil)))
	sensorID := domain.SensorID("10000000-0000-4000-8000-000000000001")
	evaluator, err := NewEvaluator(bus, manager, []ThresholdRule{{ID: "temperature-high", Name: "Temperature high", SensorID: sensorID, Condition: "above", Threshold: 27, Severity: events.SeverityCritical, Scale: 1, Offset: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = evaluator.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	quantity, _ := domain.NewQuantity(domain.QuantityTemperature, 26.5, domain.UnitCelsius)
	value := state.Value{Key: state.Key{EntityKind: state.EntitySensor, EntityID: domain.EntityID(sensorID), Plane: state.PlaneObservation, Attribute: "measurement"}, Value: domain.NewQuantityValue(quantity), Quality: domain.QualityGood, ObservedAt: time.Now().UTC()}
	event, _ := events.New("test", events.StateChanged, events.SeverityInfo, value, "")
	if err = bus.Publish(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	active, err := manager.List(context.Background(), StatusActive)
	if err != nil || len(active) != 1 {
		t.Fatalf("active=%+v err=%v", active, err)
	}
}
