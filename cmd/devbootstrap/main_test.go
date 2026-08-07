package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunVerifiesBrokerFreeSimulator(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "development.yaml")
	contents := `schema_version: 1
application:
  log_level: info
  startup_timeout: 3s
  shutdown_timeout: 2s
  component_timeout: 1s
http:
  address: "localhost:0"
  read_timeout: 1s
  write_timeout: 1s
  idle_timeout: 2s
mqtt:
  enabled: false
simulator:
  enabled: true
`
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-config", configPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunRejectsMissingConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code == 0 {
		t.Fatal("run() expected failure")
	}
}
