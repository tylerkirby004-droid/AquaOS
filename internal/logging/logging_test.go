package logging

import (
	"bytes"
	"testing"
)

func TestNewRejectsInvalidLevel(t *testing.T) {
	if _, err := New(&bytes.Buffer{}, "verbose-ish"); err == nil {
		t.Fatal("New() expected error")
	}
}

func TestNewWritesStructuredJSON(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, "info")
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("started", "component", "test")
	if output.Len() == 0 || output.Bytes()[0] != '{' {
		t.Fatalf("output = %q", output.String())
	}
}

func TestDynamicLevelControllerAppliesReload(t *testing.T) {
	var output bytes.Buffer
	logger, controller, err := NewDynamic(&output, "info")
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("before")
	controller.SetLogLevel("debug")
	logger.Debug("after")
	if bytes.Contains(output.Bytes(), []byte("before")) || !bytes.Contains(output.Bytes(), []byte("after")) {
		t.Fatalf("output = %q", output.String())
	}
}
