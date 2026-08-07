package mqtt

import (
	"os"
	"strings"
	"testing"
)

func TestExampleACLPreventsAICommandAndDesiredWrites(t *testing.T) {
	contents, err := os.ReadFile("../../configs/mosquitto/acl.example") //nolint:misspell // Mosquitto is the broker's product name.
	if err != nil {
		t.Fatal(err)
	}
	aiRules := aclUserSection(string(contents), "aquaos-vision")
	if aiRules == "" {
		t.Fatal("AI ACL section is missing")
	}
	for _, line := range strings.Split(aiRules, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "topic" || fields[1] != "write" {
			continue
		}
		if strings.Contains(fields[2], "/commands/") || strings.Contains(fields[2], "/desired") {
			t.Fatalf("AI principal has unsafe write rule: %s", line)
		}
	}
}

func TestExampleACLPermitsOnlyRequiredDiscoveryRoles(t *testing.T) {
	for _, path := range []string{"../../configs/mosquitto/acl.example", "../../infrastructure/docker/mosquitto/config/acl"} { //nolint:misspell // Mosquitto is the broker product name.
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		core := aclUserSection(string(contents), "aquaos-core")
		homeAssistant := aclUserSection(string(contents), "home-assistant")
		if !strings.Contains(core, "topic write homeassistant/+/+/config") {
			t.Fatalf("%s does not permit Core discovery publication", path)
		}
		if !strings.Contains(homeAssistant, "topic read  homeassistant/#") || !strings.Contains(homeAssistant, "topic write homeassistant/status") {
			t.Fatalf("%s does not permit Home Assistant discovery and birth status", path)
		}
		if strings.Contains(homeAssistant, "topic write homeassistant/#") {
			t.Fatalf("%s grants Home Assistant an unnecessarily broad discovery write", path)
		}
	}
}

func aclUserSection(contents, user string) string {
	marker := "user " + user
	start := strings.Index(contents, marker)
	if start < 0 {
		return ""
	}
	section := contents[start+len(marker):]
	if end := strings.Index(section, "\nuser "); end >= 0 {
		section = section[:end]
	}
	return section
}
