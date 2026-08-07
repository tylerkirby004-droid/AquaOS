package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunEmitsJSONLinesForFixture(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-scenario", "../../configs/scenarios/normal-temperature.json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if lines := strings.Count(strings.TrimSpace(stdout.String()), "\n") + 1; lines != 96 {
		t.Fatalf("trace lines=%d", lines)
	}
}

func TestRunRejectsMissingScenario(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "scenario path is required") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}
