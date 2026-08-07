// Package logging configures the process-wide structured logger passed to all
// dependencies. It intentionally uses log/slog to avoid a logging framework.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// LevelController applies validated log-level changes without replacing the logger.
type LevelController struct{ level *slog.LevelVar }

// SetLogLevel updates the live logger. Configuration validation guarantees the value.
func (c *LevelController) SetLogLevel(value string) {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(strings.ToUpper(value))); err == nil {
		c.level.Set(parsed)
	}
}

// New constructs a JSON slog logger at the requested level.
func New(out io.Writer, level string) (*slog.Logger, error) {
	logger, _, err := NewDynamic(out, level)
	return logger, err
}

// NewDynamic constructs a JSON logger and its injectable level controller.
func NewDynamic(out io.Writer, level string) (*slog.Logger, *LevelController, error) {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(strings.ToUpper(level))); err != nil {
		return nil, nil, fmt.Errorf("parse log level %q: %w", level, err)
	}
	levelVar := new(slog.LevelVar)
	levelVar.Set(parsed)
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: levelVar})), &LevelController{level: levelVar}, nil
}
