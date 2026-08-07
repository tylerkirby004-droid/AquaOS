package mqtt

import "testing"

func TestTopicContract(t *testing.T) {
	registry, err := NewRegistry("home-reef")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		purpose         Purpose
		resource, topic string
		qos             byte
		retained        bool
	}{{PurposeCoreStatus, "core", "aquaos/home-reef/v1/system/core/status", 1, true}, {PurposeAvailability, "core", "aquaos/home-reef/v1/system/core/availability", 1, true}, {PurposeSensorState, "temp-main", "aquaos/home-reef/v1/sensors/temp-main/state", 1, true}, {PurposeEquipmentReported, "heater-a", "aquaos/home-reef/v1/equipment/heater-a/reported", 1, true}, {PurposeEquipmentDesired, "heater-a", "aquaos/home-reef/v1/equipment/heater-a/desired", 1, true}, {PurposeCommandRequest, "heater-a", "aquaos/home-reef/v1/commands/heater-a/request", 1, false}, {PurposeCommandResult, "heater-a", "aquaos/home-reef/v1/commands/heater-a/result", 1, false}, {PurposeAlarmState, "high-temp", "aquaos/home-reef/v1/alarms/high-temp/state", 1, true}, {PurposeEventStream, "alarm-raised", "aquaos/home-reef/v1/events/alarm-raised", 1, false}}
	for _, tt := range tests {
		t.Run(string(tt.purpose), func(t *testing.T) {
			topic, policy, err := registry.Topic(tt.purpose, tt.resource)
			if err != nil {
				t.Fatal(err)
			}
			if topic != tt.topic || policy.QoS != tt.qos || policy.Retained != tt.retained {
				t.Fatalf("topic=%q policy=%+v", topic, policy)
			}
		})
	}
	topic, policy, err := registry.AIObservation("vision", "coral")
	if err != nil || topic != "aquaos/home-reef/v1/ai/vision/observations/coral" || policy.Retained || policy.QoS != 1 {
		t.Fatalf("AI topic=%q policy=%+v err=%v", topic, policy, err)
	}
	topic, policy, err = registry.HADiscovery("sensor", "water-temp")
	if err != nil || topic != "homeassistant/sensor/water-temp/config" || !policy.Retained || policy.QoS != 1 {
		t.Fatalf("HA topic=%q policy=%+v err=%v", topic, policy, err)
	}
}

func TestHomeAssistantCommandTopicIsNarrowAndNotRetained(t *testing.T) {
	registry, err := NewRegistry("home-reef")
	if err != nil {
		t.Fatal(err)
	}
	topic, policy, err := registry.HACommand("heater-one")
	if err != nil {
		t.Fatal(err)
	}
	if topic != "aquaos/home-reef/v1/home-assistant/heater-one/set" || policy.QoS != 1 || policy.Retained {
		t.Fatalf("topic=%q policy=%+v", topic, policy)
	}
	filter, qos, err := registry.SubscriptionFilter(PurposeHACommand)
	if err != nil || filter != "aquaos/home-reef/v1/home-assistant/+/set" || qos != 1 {
		t.Fatalf("filter=%q qos=%d err=%v", filter, qos, err)
	}
}

func TestRegistryRejectsWildcardsAndInvalidSite(t *testing.T) {
	if _, err := NewRegistry("Home Reef"); err == nil {
		t.Fatal("invalid site accepted")
	}
	registry, _ := NewRegistry("home-reef")
	if _, _, err := registry.Topic(PurposeCommandRequest, "#"); err == nil {
		t.Fatal("wildcard resource accepted")
	}
}
