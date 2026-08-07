package app

import (
	"io"
	"log/slog"
	"testing"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
)

func TestNewSupportsBrokerFreeSimulator(t *testing.T) {
	cfg := config.Defaults()
	cfg.HTTP.Address = "localhost:0"
	container, err := New(cfg, "test.yaml", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if container.Simulator == nil || container.Health == nil || container.Lifecycle == nil {
		t.Fatal("foundation dependency was not composed")
	}
}

func TestNewRejectsMissingLogger(t *testing.T) {
	if _, err := New(config.Defaults(), "test.yaml", nil); err == nil {
		t.Fatal("New() expected error")
	}
}
