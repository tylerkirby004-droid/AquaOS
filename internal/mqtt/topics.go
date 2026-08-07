package mqtt

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const contractMajorVersion = "v1"

var topicSegmentPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// Purpose identifies one stable public MQTT contract.
type Purpose string

//nolint:revive // Purpose values are documented collectively by Purpose.
const (
	PurposeCoreStatus        Purpose = "core-status"
	PurposeAvailability      Purpose = "availability"
	PurposeSensorState       Purpose = "sensor-state"
	PurposeEquipmentReported Purpose = "equipment-reported"
	PurposeEquipmentDesired  Purpose = "equipment-desired"
	PurposeCommandRequest    Purpose = "command-request"
	PurposeCommandResult     Purpose = "command-result"
	PurposeAlarmState        Purpose = "alarm-state"
	PurposeEventStream       Purpose = "event-stream"
	PurposeAIObservation     Purpose = "ai-observation"
	PurposeHADiscovery       Purpose = "ha-discovery"
)

// Policy fixes delivery semantics for a public topic.
type Policy struct {
	Purpose  Purpose `json:"purpose"`
	QoS      byte    `json:"qos"`
	Retained bool    `json:"retained"`
}

// Registry generates versioned topics from an externally supplied site ID.
type Registry struct{ siteID string }

// NewRegistry validates and stores one stable site topic segment.
func NewRegistry(siteID string) (*Registry, error) {
	if !topicSegmentPattern.MatchString(siteID) {
		return nil, errors.New("MQTT site ID must be lowercase kebab-case")
	}
	return &Registry{siteID: siteID}, nil
}

// SiteID returns the configured stable site identity.
func (r *Registry) SiteID() string { return r.siteID }

// Topic returns the exact topic and fixed policy for a purpose. Resource and
// channel values are validated segments; callers cannot publish wildcards.
func (r *Registry) Topic(purpose Purpose, resource string) (string, Policy, error) {
	if strings.ContainsAny(resource, "+#/") || !topicSegmentPattern.MatchString(resource) {
		return "", Policy{}, errors.New("MQTT resource ID must be one lowercase kebab-case segment")
	}
	root := "aquaos/" + r.siteID + "/" + contractMajorVersion
	policy := Policy{Purpose: purpose}
	var topic string
	switch purpose {
	case PurposeCoreStatus:
		topic = root + "/system/core/status"
		policy.QoS = 1
		policy.Retained = true
	case PurposeAvailability:
		topic = root + "/system/core/availability"
		policy.QoS = 1
		policy.Retained = true
	case PurposeSensorState:
		topic = fmt.Sprintf("%s/sensors/%s/state", root, resource)
		policy.QoS = 1
		policy.Retained = true
	case PurposeEquipmentReported:
		topic = fmt.Sprintf("%s/equipment/%s/reported", root, resource)
		policy.QoS = 1
		policy.Retained = true
	case PurposeEquipmentDesired:
		topic = fmt.Sprintf("%s/equipment/%s/desired", root, resource)
		policy.QoS = 1
		policy.Retained = true
	case PurposeCommandRequest:
		topic = fmt.Sprintf("%s/commands/%s/request", root, resource)
		policy.QoS = 1
	case PurposeCommandResult:
		topic = fmt.Sprintf("%s/commands/%s/result", root, resource)
		policy.QoS = 1
	case PurposeAlarmState:
		topic = fmt.Sprintf("%s/alarms/%s/state", root, resource)
		policy.QoS = 1
		policy.Retained = true
	case PurposeEventStream:
		topic = fmt.Sprintf("%s/events/%s", root, resource)
		policy.QoS = 1
	case PurposeAIObservation, PurposeHADiscovery:
		return "", Policy{}, errors.New("purpose requires its dedicated multi-segment constructor")
	default:
		return "", Policy{}, errors.New("unknown MQTT topic purpose")
	}
	return topic, policy, nil
}

// AIObservation returns the versioned, non-retained AI observation topic.
func (r *Registry) AIObservation(service, kind string) (string, Policy, error) {
	if !topicSegmentPattern.MatchString(service) || !topicSegmentPattern.MatchString(kind) {
		return "", Policy{}, errors.New("AI service and kind must be lowercase kebab-case")
	}
	return fmt.Sprintf("aquaos/%s/%s/ai/%s/observations/%s", r.siteID, contractMajorVersion, service, kind), Policy{Purpose: PurposeAIObservation, QoS: 1}, nil
}

// HADiscovery returns the retained Home Assistant discovery contract. Entity
// generation and cleanup remain Prompt 10 responsibilities.
func (r *Registry) HADiscovery(component, objectID string) (string, Policy, error) {
	if !topicSegmentPattern.MatchString(component) || !topicSegmentPattern.MatchString(objectID) {
		return "", Policy{}, errors.New("Home Assistant component and object ID must be lowercase kebab-case") //nolint:staticcheck // Home Assistant is a proper product name.
	}
	return fmt.Sprintf("homeassistant/%s/%s/config", component, objectID), Policy{Purpose: PurposeHADiscovery, QoS: 1, Retained: true}, nil
}

// SubscriptionFilter returns narrowly scoped subscriber filters. It never
// returns the broker-wide # wildcard.
func (r *Registry) SubscriptionFilter(purpose Purpose) (string, byte, error) {
	root := "aquaos/" + r.siteID + "/" + contractMajorVersion
	switch purpose {
	case PurposeCommandRequest:
		return root + "/commands/+/request", 1, nil
	case PurposeAIObservation:
		return root + "/ai/+/observations/+", 1, nil
	default:
		return "", 0, errors.New("purpose has no wildcard subscription contract")
	}
}
