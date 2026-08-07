package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestStagingStatusNeedsNoProductionPrivilege(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--root", t.TempDir(), "status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"installed": false`) {
		t.Fatalf("output = %s", stdout.String())
	}
}
