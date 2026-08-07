package simulator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const maximumScenarioBytes = 1024 * 1024

// LoadScenario strictly loads one bounded external JSON scenario fixture.
func LoadScenario(path string) (Scenario, error) {
	if path == "" {
		return Scenario{}, errors.New("scenario path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("open scenario: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maximumScenarioBytes+1))
	if err != nil {
		return Scenario{}, fmt.Errorf("read scenario: %w", err)
	}
	if len(data) > maximumScenarioBytes {
		return Scenario{}, errors.New("scenario exceeds one MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var scenario Scenario
	if err = decoder.Decode(&scenario); err != nil {
		return Scenario{}, fmt.Errorf("decode scenario: %w", err)
	}
	if err = ensureJSONEOF(decoder); err != nil {
		return Scenario{}, err
	}
	if err = scenario.Validate(); err != nil {
		return Scenario{}, err
	}
	return scenario, nil
}

// Verify checks that all fixture-declared outcomes occurred at least once.
func Verify(scenario Scenario, result Result) error {
	alarms := make(map[string]struct{})
	transitions := make(map[string]struct{})
	for _, trace := range result.Traces {
		for _, code := range trace.AlarmCodes {
			alarms[code] = struct{}{}
		}
		for _, transition := range trace.SafeTransitions {
			transitions[transition] = struct{}{}
		}
	}
	for _, expected := range scenario.Expected.AlarmCodes {
		if _, exists := alarms[expected]; !exists {
			return fmt.Errorf("expected alarm %q was not observed", expected)
		}
	}
	for _, expected := range scenario.Expected.SafeTransitions {
		if _, exists := transitions[expected]; !exists {
			return fmt.Errorf("expected safe transition %q was not observed", expected)
		}
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("scenario contains trailing JSON")
	}
	return fmt.Errorf("decode trailing scenario data: %w", err)
}
