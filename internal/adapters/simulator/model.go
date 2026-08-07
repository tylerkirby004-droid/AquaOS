package simulator

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

// Duration is a JSON string duration used by external scenario contracts.
type Duration time.Duration

// UnmarshalJSON parses a Go duration string and rejects numeric ambiguity.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("duration must be a quoted Go duration string")
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("parse duration: %w", err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalJSON emits the duration as a stable string.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// FaultType identifies one reproducible simulator fault injection.
type FaultType string

//nolint:revive // Fault values form one documented external scenario enum.
const (
	FaultSensorNoise       FaultType = "sensor-noise"
	FaultSensorDrift       FaultType = "sensor-drift"
	FaultSensorStale       FaultType = "sensor-stale"
	FaultRelayStuckOn      FaultType = "relay-stuck-on"
	FaultAckDelay          FaultType = "ack-delay"
	FaultAckLoss           FaultType = "ack-loss"
	FaultBrokerLoss        FaultType = "broker-loss"
	FaultStorageLoss       FaultType = "storage-loss"
	FaultLeak              FaultType = "leak"
	FaultReturnPumpFailure FaultType = "return-pump-failure"
)

// Fault schedules a fault for Duration steps beginning at At. Zero duration
// keeps the fault active through the remainder of the scenario.
type Fault struct {
	At       int       `json:"at"`
	Duration int       `json:"duration,omitempty"`
	Type     FaultType `json:"type"`
	Value    float64   `json:"value,omitempty"`
}

// ModelConfig contains all physical and supervisory constants used by a run.
type ModelConfig struct {
	InitialTemperatureC       float64  `json:"initialTemperatureC"`
	AmbientTemperatureC       float64  `json:"ambientTemperatureC"`
	InitialLevelPercent       float64  `json:"initialLevelPercent"`
	TemperatureLowC           float64  `json:"temperatureLowC"`
	TemperatureHighC          float64  `json:"temperatureHighC"`
	LevelLowPercent           float64  `json:"levelLowPercent"`
	LevelHighPercent          float64  `json:"levelHighPercent"`
	AmbientExchangePerHour    float64  `json:"ambientExchangePerHour"`
	HeaterInfluenceCPerHour   float64  `json:"heaterInfluenceCPerHour"`
	EvaporationPercentPerHour float64  `json:"evaporationPercentPerHour"`
	ATOFillPercentPerHour     float64  `json:"atoFillPercentPerHour"`
	SensorFreshFor            Duration `json:"sensorFreshFor"`
}

// Scenario is a complete reproducible workbench run.
type Scenario struct {
	SchemaVersion int          `json:"schemaVersion"`
	Name          string       `json:"name"`
	Seed          int64        `json:"seed"`
	Start         time.Time    `json:"start"`
	Step          Duration     `json:"step"`
	Steps         int          `json:"steps"`
	Model         ModelConfig  `json:"model"`
	Faults        []Fault      `json:"faults,omitempty"`
	Expected      Expectations `json:"expected"`
}

// Expectations declares fixture outcomes checked by the scenario harness.
type Expectations struct {
	AlarmCodes      []string `json:"alarmCodes,omitempty"`
	SafeTransitions []string `json:"safeTransitions,omitempty"`
}

// EquipmentState is the simulator's hardware-incapable reported state.
type EquipmentState struct {
	Heater          bool `json:"heater"`
	ReturnPump      bool `json:"returnPump"`
	CirculationPump bool `json:"circulationPump"`
	ATO             bool `json:"ato"`
	DosingPump      bool `json:"dosingPump"`
}

// Trace is one immutable simulator step suitable for JSON Lines output.
type Trace struct {
	Step                 int            `json:"step"`
	Timestamp            time.Time      `json:"timestamp"`
	TemperatureC         float64        `json:"temperatureC"`
	ObservedTemperatureC float64        `json:"observedTemperatureC"`
	TemperatureQuality   domain.Quality `json:"temperatureQuality"`
	LevelPercent         float64        `json:"levelPercent"`
	Leak                 bool           `json:"leak"`
	Desired              EquipmentState `json:"desired"`
	Reported             EquipmentState `json:"reported"`
	Acknowledgement      string         `json:"acknowledgement"`
	BrokerAvailable      bool           `json:"brokerAvailable"`
	StorageAvailable     bool           `json:"storageAvailable"`
	AlarmCodes           []string       `json:"alarmCodes,omitempty"`
	SafeTransitions      []string       `json:"safeTransitions,omitempty"`
}

// Result contains a complete bounded scenario trace.
type Result struct {
	Name   string  `json:"name"`
	Seed   int64   `json:"seed"`
	Traces []Trace `json:"traces"`
}

// Validate rejects ambiguous, unbounded, or physically invalid scenarios.
func (s Scenario) Validate() error {
	if s.SchemaVersion != 1 {
		return errors.New("scenario schemaVersion must equal 1")
	}
	if s.Name == "" || len(s.Name) > 128 || s.Start.IsZero() {
		return errors.New("scenario name and start are required")
	}
	if time.Duration(s.Step) <= 0 || time.Duration(s.Step) > time.Hour || s.Steps < 1 || s.Steps > 100000 {
		return errors.New("scenario step must be within (0,1h] and steps within [1,100000]")
	}
	m := s.Model
	values := []float64{m.InitialTemperatureC, m.AmbientTemperatureC, m.InitialLevelPercent, m.TemperatureLowC, m.TemperatureHighC, m.LevelLowPercent, m.LevelHighPercent, m.AmbientExchangePerHour, m.HeaterInfluenceCPerHour, m.EvaporationPercentPerHour, m.ATOFillPercentPerHour}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("scenario model values must be finite")
		}
	}
	if m.TemperatureLowC >= m.TemperatureHighC || m.LevelLowPercent >= m.LevelHighPercent || m.InitialLevelPercent < 0 || m.InitialLevelPercent > 100 || time.Duration(m.SensorFreshFor) <= 0 {
		return errors.New("scenario model thresholds or initial level are invalid")
	}
	if m.AmbientExchangePerHour < 0 || m.HeaterInfluenceCPerHour < 0 || m.EvaporationPercentPerHour < 0 || m.ATOFillPercentPerHour <= 0 {
		return errors.New("scenario physical rates are invalid")
	}
	if len(s.Faults) > 10000 {
		return errors.New("scenario cannot contain more than 10000 faults")
	}
	for index, fault := range s.Faults {
		if fault.At < 0 || fault.At >= s.Steps || fault.Duration < 0 {
			return fmt.Errorf("fault %d schedule is outside the scenario", index)
		}
		if !validFault(fault.Type) {
			return fmt.Errorf("fault %d has unsupported type %q", index, fault.Type)
		}
		if fault.Type == FaultSensorNoise && fault.Value < 0 {
			return fmt.Errorf("fault %d sensor noise cannot be negative", index)
		}
	}
	return nil
}

// Run executes a scenario synchronously using only its injected start, step,
// and random seed. It cannot open a socket or invoke physical hardware.
func Run(s Scenario) (Result, error) {
	if err := s.Validate(); err != nil {
		return Result{}, err
	}
	runner := modelRunner{
		scenario:         s,
		random:           rand.New(rand.NewSource(s.Seed)), //nolint:gosec // Reproducibility, not security, is required.
		temperature:      s.Model.InitialTemperatureC,
		level:            s.Model.InitialLevelPercent,
		desired:          EquipmentState{ReturnPump: true, CirculationPump: true},
		reported:         EquipmentState{ReturnPump: true, CirculationPump: true},
		brokerAvailable:  true,
		storageAvailable: true,
	}
	result := Result{Name: s.Name, Seed: s.Seed, Traces: make([]Trace, 0, s.Steps)}
	for step := 0; step < s.Steps; step++ {
		result.Traces = append(result.Traces, runner.tick(step))
	}
	return result, nil
}

type modelRunner struct {
	scenario         Scenario
	random           *rand.Rand
	temperature      float64
	level            float64
	desired          EquipmentState
	reported         EquipmentState
	lastObserved     float64
	lastObservedAt   time.Time
	brokerAvailable  bool
	storageAvailable bool
}

func (r *modelRunner) tick(step int) Trace {
	now := r.scenario.Start.Add(time.Duration(step) * time.Duration(r.scenario.Step)).UTC()
	faults := r.activeFaults(step)
	noise := faultValue(faults, FaultSensorNoise)
	drift := faultValue(faults, FaultSensorDrift) * now.Sub(r.scenario.Start).Hours()
	quality := domain.QualityGood
	observed := r.temperature + drift
	if noise != 0 {
		observed += r.random.NormFloat64() * noise
		quality = domain.QualitySuspect
	}
	if hasFault(faults, FaultSensorStale) {
		observed = r.lastObserved
		if r.lastObservedAt.IsZero() || !now.Before(r.lastObservedAt.Add(time.Duration(r.scenario.Model.SensorFreshFor))) {
			quality = domain.QualityStale
		}
	} else {
		r.lastObserved, r.lastObservedAt = observed, now
	}
	leak := hasFault(faults, FaultLeak)
	alarms := make([]string, 0, 4)
	previousDesired := r.desired
	r.desired.ReturnPump = true
	r.desired.CirculationPump = true
	r.desired.DosingPump = false
	if quality == domain.QualityStale {
		r.desired.Heater = false
		alarms = append(alarms, "sim.alarm.sensor-stale")
	} else if observed <= r.scenario.Model.TemperatureLowC {
		r.desired.Heater = true
	} else if observed >= r.scenario.Model.TemperatureHighC {
		r.desired.Heater = false
		alarms = append(alarms, "sim.alarm.high-temperature")
	}
	if r.level <= r.scenario.Model.LevelLowPercent {
		r.desired.ATO = true
	} else if r.level >= r.scenario.Model.LevelHighPercent {
		r.desired.ATO = false
	}
	if leak {
		r.desired = EquipmentState{}
		alarms = append(alarms, "sim.alarm.leak")
	}
	if hasFault(faults, FaultReturnPumpFailure) {
		alarms = append(alarms, "sim.alarm.return-pump-failure")
	}
	ack := "acknowledged"
	if hasFault(faults, FaultAckLoss) {
		ack = "lost"
		alarms = append(alarms, "sim.alarm.command-acknowledgement-lost")
	} else if hasFault(faults, FaultAckDelay) {
		ack = "delayed"
		alarms = append(alarms, "sim.alarm.command-acknowledgement-delayed")
	}
	r.reported = r.desired
	if hasFault(faults, FaultRelayStuckOn) {
		r.reported.Heater = true
		if !r.desired.Heater {
			alarms = append(alarms, "sim.alarm.relay-stuck-on")
		}
	}
	if hasFault(faults, FaultReturnPumpFailure) {
		r.reported.ReturnPump = false
	}
	r.brokerAvailable = !hasFault(faults, FaultBrokerLoss)
	r.storageAvailable = !hasFault(faults, FaultStorageLoss)
	transitions := safeTransitions(previousDesired, r.desired)
	r.advancePhysics()
	sort.Strings(alarms)
	sort.Strings(transitions)
	return Trace{Step: step, Timestamp: now, TemperatureC: round(r.temperature), ObservedTemperatureC: round(observed), TemperatureQuality: quality, LevelPercent: round(r.level), Leak: leak, Desired: r.desired, Reported: r.reported, Acknowledgement: ack, BrokerAvailable: r.brokerAvailable, StorageAvailable: r.storageAvailable, AlarmCodes: alarms, SafeTransitions: transitions}
}

func (r *modelRunner) advancePhysics() {
	hours := time.Duration(r.scenario.Step).Hours()
	r.temperature += (r.scenario.Model.AmbientTemperatureC - r.temperature) * r.scenario.Model.AmbientExchangePerHour * hours
	if r.reported.Heater {
		r.temperature += r.scenario.Model.HeaterInfluenceCPerHour * hours
	}
	r.level -= r.scenario.Model.EvaporationPercentPerHour * hours
	if r.reported.ATO {
		r.level += r.scenario.Model.ATOFillPercentPerHour * hours
	}
	r.level = math.Max(0, math.Min(100, r.level))
}

func (r *modelRunner) activeFaults(step int) []Fault {
	active := make([]Fault, 0)
	for _, fault := range r.scenario.Faults {
		if step >= fault.At && (fault.Duration == 0 || step < fault.At+fault.Duration) {
			active = append(active, fault)
		}
	}
	return active
}

func safeTransitions(previous, next EquipmentState) []string {
	transitions := make([]string, 0, 5)
	if previous.Heater && !next.Heater {
		transitions = append(transitions, "heater:off")
	}
	if previous.ReturnPump && !next.ReturnPump {
		transitions = append(transitions, "return-pump:off")
	}
	if previous.CirculationPump && !next.CirculationPump {
		transitions = append(transitions, "circulation-pump:off")
	}
	if previous.ATO && !next.ATO {
		transitions = append(transitions, "ato:off")
	}
	if previous.DosingPump && !next.DosingPump {
		transitions = append(transitions, "dosing-pump:off")
	}
	return transitions
}

func hasFault(faults []Fault, kind FaultType) bool {
	for _, fault := range faults {
		if fault.Type == kind {
			return true
		}
	}
	return false
}
func faultValue(faults []Fault, kind FaultType) float64 {
	for _, fault := range faults {
		if fault.Type == kind {
			return fault.Value
		}
	}
	return 0
}
func validFault(kind FaultType) bool {
	switch kind {
	case FaultSensorNoise, FaultSensorDrift, FaultSensorStale, FaultRelayStuckOn, FaultAckDelay, FaultAckLoss, FaultBrokerLoss, FaultStorageLoss, FaultLeak, FaultReturnPumpFailure:
		return true
	default:
		return false
	}
}
func round(value float64) float64 { return math.Round(value*10000) / 10000 }
