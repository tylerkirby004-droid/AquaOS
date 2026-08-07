// Command aquaos-sim runs deterministic, hardware-incapable scenario fixtures.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"os"

	"github.com/tylerkirby004-droid/aquaos/internal/adapters/simulator"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	flags := flag.NewFlagSet("aquaos-sim", flag.ContinueOnError)
	flags.SetOutput(stderr)
	scenarioPath := flags.String("scenario", "", "path to a simulator JSON scenario")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	scenario, err := simulator.LoadScenario(*scenarioPath)
	if err != nil {
		logger.Error("load simulator scenario", "code", "sim.scenario_load_failed", "error", err)
		return 1
	}
	result, err := simulator.Run(scenario)
	if err != nil {
		logger.Error("run simulator scenario", "code", "sim.scenario_run_failed", "error", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	for _, trace := range result.Traces {
		if err = encoder.Encode(trace); err != nil {
			logger.Error("write simulator trace", "code", "sim.trace_write_failed", "error", err)
			return 1
		}
	}
	if err = simulator.Verify(scenario, result); err != nil {
		logger.Error("verify simulator scenario", "code", "sim.scenario_verify_failed", "error", err)
		return 1
	}
	return 0
}
