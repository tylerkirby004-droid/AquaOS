package config

import "testing"

func TestDefaultsAreBrokerFreeAndSimulatorSafe(t *testing.T) {
	cfg := Defaults()
	if cfg.MQTT.Enabled {
		t.Fatal("MQTT must default to disabled")
	}
	if !cfg.Simulator.Enabled {
		t.Fatal("simulator must default to enabled")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
