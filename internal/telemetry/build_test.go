package telemetry

import "testing"

func TestCurrentBuildAlwaysHasVersion(t *testing.T) {
	if CurrentBuild().Version == "" {
		t.Fatal("CurrentBuild() returned an empty version")
	}
}
