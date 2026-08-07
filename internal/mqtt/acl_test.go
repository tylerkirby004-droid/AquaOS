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
