// Package telemetry exposes process metadata and future observability ports.
package telemetry

import (
	"runtime/debug"
	"time"
)

// BuildInfo describes the source revision embedded by the Go toolchain.
type BuildInfo struct {
	Version  string    `json:"version"`
	Revision string    `json:"revision,omitempty"`
	BuiltAt  time.Time `json:"builtAt,omitempty"`
	Modified bool      `json:"modified"`
}

// CurrentBuild reads immutable module and VCS settings from the running binary.
// It uses runtime metadata instead of mutable package globals or init-time I/O.
func CurrentBuild() BuildInfo {
	result := BuildInfo{Version: "development"}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return result
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		result.Version = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			result.Revision = setting.Value
		case "vcs.time":
			if parsed, err := time.Parse(time.RFC3339, setting.Value); err == nil {
				result.BuiltAt = parsed.UTC()
			}
		case "vcs.modified":
			result.Modified = setting.Value == "true"
		}
	}
	return result
}
