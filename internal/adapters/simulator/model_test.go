package simulator

import (
	"encoding/json"
	"testing"
	"time"
)

func TestScenarioIsDeterministic(t *testing.T) {
	scenario, err := LoadScenario("../../../configs/scenarios/adapter-and-integration-faults.json")
	if err != nil {
		t.Fatal(err)
	}
	first, err := Run(scenario)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(scenario)
	if err != nil {
		t.Fatal(err)
	}
	left, _ := json.Marshal(first)
	right, _ := json.Marshal(second)
	if string(left) != string(right) {
		t.Fatal("same scenario and seed produced different traces")
	}
}

func TestNormalTemperatureSupervisionStaysWithinBand(t *testing.T) {
	scenario, err := LoadScenario("../../../configs/scenarios/normal-temperature.json")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(scenario)
	if err != nil {
		t.Fatal(err)
	}
	for _, trace := range result.Traces {
		if trace.TemperatureC < scenario.Model.TemperatureLowC-0.1 || trace.TemperatureC > scenario.Model.TemperatureHighC+0.1 {
			t.Fatalf("step %d temperature %.4f escaped supervised band", trace.Step, trace.TemperatureC)
		}
	}
}

func TestFaultScenarioProducesExpectedAlarmsAndSafeTransitions(t *testing.T) {
	scenario, err := LoadScenario("../../../configs/scenarios/safety-faults.json")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(scenario)
	if err != nil {
		t.Fatal(err)
	}
	if err = Verify(scenario, result); err != nil {
		t.Fatal(err)
	}
	for _, trace := range result.Traces {
		if trace.Leak && (trace.Desired.Heater || trace.Desired.ATO || trace.Desired.DosingPump || trace.Desired.ReturnPump || trace.Desired.CirculationPump) {
			t.Fatalf("step %d leak did not request safe off state: %+v", trace.Step, trace.Desired)
		}
		if trace.TemperatureQuality == "stale" && trace.Desired.Heater {
			t.Fatalf("step %d stale temperature permitted heater", trace.Step)
		}
	}
}

func TestBrokerAndStorageLossDoNotChangeLocalControl(t *testing.T) {
	scenario := testScenario()
	scenario.Faults = []Fault{{At: 2, Duration: 4, Type: FaultBrokerLoss}, {At: 3, Duration: 4, Type: FaultStorageLoss}}
	withLoss, err := Run(scenario)
	if err != nil {
		t.Fatal(err)
	}
	scenario.Faults = nil
	withoutLoss, err := Run(scenario)
	if err != nil {
		t.Fatal(err)
	}
	for index := range withLoss.Traces {
		left, right := withLoss.Traces[index], withoutLoss.Traces[index]
		if left.Desired != right.Desired || left.Reported != right.Reported || left.TemperatureC != right.TemperatureC || left.LevelPercent != right.LevelPercent {
			t.Fatalf("integration loss changed local control at step %d", index)
		}
	}
}

func TestAdapterAndIntegrationFixtureExposesEveryInjectedFailure(t *testing.T) {
	scenario, err := LoadScenario("../../../configs/scenarios/adapter-and-integration-faults.json")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(scenario)
	if err != nil {
		t.Fatal(err)
	}
	if err = Verify(scenario, result); err != nil {
		t.Fatal(err)
	}
	var delayed, lost, brokerDown, storageDown, suspect bool
	for _, trace := range result.Traces {
		delayed = delayed || trace.Acknowledgement == "delayed"
		lost = lost || trace.Acknowledgement == "lost"
		brokerDown = brokerDown || !trace.BrokerAvailable
		storageDown = storageDown || !trace.StorageAvailable
		suspect = suspect || trace.TemperatureQuality == "suspect"
	}
	if !delayed || !lost || !brokerDown || !storageDown || !suspect {
		t.Fatalf("fault evidence delayed=%v lost=%v broker=%v storage=%v suspect=%v", delayed, lost, brokerDown, storageDown, suspect)
	}
}

func TestScenarioValidationRejectsUnknownAndUnboundedInput(t *testing.T) {
	scenario := testScenario()
	scenario.Faults = []Fault{{At: 0, Type: "unknown"}}
	if err := scenario.Validate(); err == nil {
		t.Fatal("unknown fault accepted")
	}
	scenario = testScenario()
	scenario.Steps = 100001
	if err := scenario.Validate(); err == nil {
		t.Fatal("unbounded scenario accepted")
	}
	if _, err := LoadScenario(""); err == nil {
		t.Fatal("empty path accepted")
	}
}

func testScenario() Scenario {
	return Scenario{SchemaVersion: 1, Name: "test", Seed: 1, Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Step: Duration(time.Minute), Steps: 10, Model: ModelConfig{InitialTemperatureC: 24.9, AmbientTemperatureC: 24, InitialLevelPercent: 70, TemperatureLowC: 24.8, TemperatureHighC: 25.2, LevelLowPercent: 69, LevelHighPercent: 71, AmbientExchangePerHour: 0.08, HeaterInfluenceCPerHour: 1.2, EvaporationPercentPerHour: 0.2, ATOFillPercentPerHour: 2, SensorFreshFor: Duration(5 * time.Minute)}}
}

func TestDurationRejectsNumericJSON(t *testing.T) {
	var duration Duration
	if err := json.Unmarshal([]byte(`12`), &duration); err == nil {
		t.Fatal("numeric duration accepted")
	}
	if err := json.Unmarshal([]byte(`"bad"`), &duration); err == nil {
		t.Fatal("invalid duration accepted")
	}
	if err := json.Unmarshal([]byte(`"1s"`), &duration); err != nil || time.Duration(duration) != time.Second {
		t.Fatalf("duration=%v err=%v", duration, err)
	}
}
