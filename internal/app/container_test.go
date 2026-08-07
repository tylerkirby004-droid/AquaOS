package app

import (
	"io"
	"log/slog"
	"testing"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
)

func TestNewWiresCoreServices(t *testing.T) {
	cfg := config.Defaults()
	cfg.HTTP.Address = "localhost:0"
	container, err := New(cfg, "test.yaml", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if container.Devices == nil || container.Sensors == nil || container.Equipment == nil || container.Events == nil {
		t.Fatal("core service was not wired")
	}
}

func TestNewRejectsMissingLogger(t *testing.T) {
	cfg := config.Defaults()
	if _, err := New(cfg, "test.yaml", nil); err == nil {
		t.Fatal("New() expected error")
	}
}

func TestNewSupportsBrokerFreeSimulator(t *testing.T) {
	cfg := config.Defaults()
	cfg.HTTP.Address = "localhost:0"
	container, err := New(cfg, "test.yaml", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if container.MQTT != nil {
		t.Fatal("broker-free composition unexpectedly created MQTT client")
	}
	if container.Simulator == nil {
		t.Fatal("simulator was not composed")
	}
}
