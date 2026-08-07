package alarms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
	"github.com/tylerkirby004-droid/aquaos/internal/events"
	"github.com/tylerkirby004-droid/aquaos/internal/health"
	"github.com/tylerkirby004-droid/aquaos/internal/state"
)

// ThresholdRule binds a user-facing threshold to the generic alarm engine.
type ThresholdRule struct {
	ID            string
	Name          string
	SensorID      domain.SensorID
	Condition     string
	Threshold     float64
	ThresholdHigh *float64
	Severity      events.Severity
	Delay         time.Duration
	ClearDelay    time.Duration
	Latching      bool
	Scale         float64
	Offset        float64
}

// EventSubscriber is the event-bus subset consumed by the evaluator.
type EventSubscriber interface {
	Subscribe(events.Type, events.Handler) (events.Subscription, error)
}

// Evaluator translates canonical sensor observations into alarm observations.
// It starts no goroutines; event delivery stays synchronous and bounded.
type Evaluator struct {
	mu           sync.RWMutex
	bus          EventSubscriber
	engine       AlarmManager
	rules        map[domain.SensorID][]ThresholdRule
	subscription events.Subscription
	started      bool
}

// NewEvaluator validates and constructs a threshold evaluator.
func NewEvaluator(bus EventSubscriber, engine AlarmManager, rules []ThresholdRule) (*Evaluator, error) {
	if bus == nil || engine == nil {
		return nil, errors.New("alarm evaluator dependencies are required")
	}
	result := &Evaluator{bus: bus, engine: engine, rules: make(map[domain.SensorID][]ThresholdRule)}
	for _, rule := range rules {
		if err := rule.SensorID.Validate(); err != nil || rule.ID == "" || rule.Name == "" || !oneOfCondition(rule.Condition) || rule.Severity.Rank() == 0 || rule.Delay < 0 || rule.ClearDelay < 0 {
			return nil, errors.New("alarm threshold rule is invalid")
		}
		if rule.Scale == 0 {
			rule.Scale = 1
		}
		result.rules[rule.SensorID] = append(result.rules[rule.SensorID], rule)
	}
	return result, nil
}

// Name returns the lifecycle component name.
func (*Evaluator) Name() string { return "alarm-evaluator" }

// Start registers rules and subscribes to canonical state events.
func (e *Evaluator) Start(ctx context.Context) error {
	for _, rules := range e.rules {
		for _, threshold := range rules {
			rule := Rule{ID: thresholdRuleID(threshold.ID), Code: "sensor.threshold." + threshold.Condition, Name: threshold.Name, Subject: Subject{Kind: "sensor", ID: domain.EntityID(threshold.SensorID)}, Severity: threshold.Severity, Debounce: threshold.Delay, Hysteresis: threshold.ClearDelay, Latching: threshold.Latching}
			if err := e.engine.RegisterRule(ctx, rule); err != nil {
				return fmt.Errorf("register configured alarm rule: %w", err)
			}
		}
	}
	subscription, err := e.bus.Subscribe(events.StateChanged, e.handle)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.subscription, e.started = subscription, true
	e.mu.Unlock()
	return nil
}

// Stop unregisters synchronous event delivery.
func (e *Evaluator) Stop(context.Context) error {
	e.mu.Lock()
	if e.subscription != nil {
		e.subscription.Unsubscribe()
	}
	e.subscription, e.started = nil, false
	e.mu.Unlock()
	return nil
}

// Health reports whether event evaluation is active.
func (e *Evaluator) Health() health.Status {
	e.mu.RLock()
	started := e.started
	e.mu.RUnlock()
	status := health.StateUnhealthy
	if started {
		status = health.StateHealthy
	}
	return health.NewStatus(e.Name(), status, "", time.Now().UTC())
}

func (e *Evaluator) handle(ctx context.Context, event events.Event) error {
	var value state.Value
	if err := json.Unmarshal(event.Payload, &value); err != nil {
		return fmt.Errorf("decode state event: %w", err)
	}
	if value.Key.EntityKind != state.EntitySensor || value.Key.Plane != state.PlaneObservation || value.Key.Attribute != "measurement" || value.Quality != domain.QualityGood {
		return nil
	}
	rules := e.rules[domain.SensorID(value.Key.EntityID)]
	for _, rule := range rules {
		active, evidence, err := evaluate(rule, value)
		if err != nil {
			return err
		}
		_, _, err = e.engine.Observe(ctx, Observation{RuleID: thresholdRuleID(rule.ID), Active: active, Severity: rule.Severity, ObservedAt: value.ObservedAt, Evidence: evidence, CorrelationID: event.CorrelationID})
		if err != nil {
			return fmt.Errorf("observe configured alarm rule: %w", err)
		}
	}
	return nil
}

func evaluate(rule ThresholdRule, stateValue state.Value) (bool, Evidence, error) {
	var numeric float64
	var boolean bool
	switch stateValue.Value.Kind {
	case domain.ValueQuantity:
		numeric = stateValue.Value.Quantity.Value*rule.Scale + rule.Offset
	case domain.ValueBoolean:
		boolean = *stateValue.Value.Boolean
	default:
		return false, Evidence{}, errors.New("alarm threshold requires numeric or boolean state")
	}
	active := false
	switch rule.Condition {
	case "above":
		active = numeric > rule.Threshold
	case "below":
		active = numeric < rule.Threshold
	case "outside":
		if rule.ThresholdHigh == nil {
			return false, Evidence{}, errors.New("outside alarm threshold requires upper bound")
		}
		active = numeric < rule.Threshold || numeric > *rule.ThresholdHigh
	case "true":
		active = boolean
	case "false":
		active = !boolean
	}
	return active, Evidence{Code: "sensor.threshold", Message: "configured sensor threshold evaluated", Metadata: map[string]string{"condition": rule.Condition}}, nil
}

func thresholdRuleID(value string) domain.RuleID {
	sum := sha256.Sum256([]byte("aquaos-alarm-rule:" + value))
	sum[6] = (sum[6] & 0x0f) | 0x40
	sum[8] = (sum[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(sum[:16])
	return domain.RuleID(hexValue[:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:32])
}

func oneOfCondition(value string) bool {
	return value == "above" || value == "below" || value == "outside" || value == "true" || value == "false"
}
