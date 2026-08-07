package main

import (
	"bytes"
	"testing"
)

func TestRunRequiresKnownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"unknown"}, &bytes.Buffer{}, &stdout, &stderr); code != 2 {
		t.Fatalf("code = %d", code)
	}
}
