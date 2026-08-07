package mqtt

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

type exportTransport struct {
	topic   string
	payload []byte
	err     error
}

func (*exportTransport) Name() string                { return "export-test" }
func (*exportTransport) Start(context.Context) error { return nil }
func (*exportTransport) Stop(context.Context) error  { return nil }
func (*exportTransport) Health() health.Status       { return health.Status{} }
func (*exportTransport) Subscribe(context.Context, string, byte, paho.MessageHandler) error {
	return nil
}
func (t *exportTransport) Publish(_ context.Context, topic string, _ byte, _ bool, payload []byte) error {
	t.topic, t.payload = topic, append([]byte(nil), payload...)
	return t.err
}

func TestEventExporterPublishesSensorStateAndIsolatesFailure(t *testing.T) {
	transport := &exportTransport{}
	exporter, err := NewEventExporter("home-reef", 4096, transport, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	correlationID := domain.CorrelationID("11111111-1111-4111-8111-111111111111")
	value := state.Value{Key: state.Key{EntityKind: state.EntitySensor, EntityID: "tank-temp", Plane: state.PlaneObservation, Attribute: "measurement"}, Revision: 3}
	event, err := events.New("test", events.StateChanged, events.SeverityInfo, value, correlationID)
	if err != nil {
		t.Fatal(err)
	}
	if err = exporter.Handle(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if transport.topic != "aquaos/home-reef/v1/sensors/sensor-tank-temp/state" || len(transport.payload) == 0 {
		t.Fatalf("topic=%q payload=%s", transport.topic, transport.payload)
	}
	transport.err = errors.New("broker unavailable")
	if err = exporter.Handle(context.Background(), event); err != nil {
		t.Fatalf("optional failure escaped: %v", err)
	}
}

func TestEventExporterConstructorValidatesDependencies(t *testing.T) {
	if _, err := NewEventExporter("home-reef", 4096, nil, slog.Default()); err == nil {
		t.Fatal("nil transport accepted")
	}
}
