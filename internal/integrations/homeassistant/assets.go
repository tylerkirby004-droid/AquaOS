package homeassistant

import (
	"strings"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
)

// Dashboard renders a dependency-free Home Assistant YAML dashboard whose
// entity IDs match AquaOS MQTT Discovery's explicit object IDs.
func Dashboard(cfg config.Config, _ string) ([]byte, error) {
	var output strings.Builder
	output.WriteString("title: AquaOS\nviews:\n  - title: Overview\n    path: aquaos\n    icon: mdi:fishbowl\n    cards:\n")
	writeEntitiesCard(&output, "System and alarms", []string{"sensor." + entityObjectID(cfg.MQTT.SiteID, "core"), "binary_sensor." + entityObjectID(cfg.MQTT.SiteID, "alarm"), "binary_sensor." + entityObjectID(cfg.MQTT.SiteID, "notification")})
	sensors := make([]string, 0, len(cfg.Inventory.Sensors))
	for _, item := range cfg.Inventory.Sensors {
		sensors = append(sensors, "sensor."+entityObjectID("sensor", item.EntityID))
	}
	writeEntitiesCard(&output, "Validated sensors", sensors)
	equipment := make([]string, 0, len(cfg.Inventory.Equipment))
	for _, item := range cfg.Inventory.Equipment {
		equipment = append(equipment, "switch."+entityObjectID("equipment", item.EntityID))
	}
	writeEntitiesCard(&output, "Equipment — commands remain safety checked", equipment)
	trendEntities := append(append([]string(nil), sensors...), equipment...)
	if len(trendEntities) > 0 {
		output.WriteString("      - type: history-graph\n        title: 24-hour sensor and equipment trends\n        hours_to_show: 24\n        entities:\n")
		for _, entity := range trendEntities {
			output.WriteString("          - ")
			output.WriteString(entity)
			output.WriteByte('\n')
		}
	}
	return []byte(output.String()), nil
}

func writeEntitiesCard(output *strings.Builder, title string, entities []string) {
	output.WriteString("      - type: entities\n        title: \"")
	output.WriteString(strings.ReplaceAll(title, "\"", "'"))
	output.WriteString("\"\n        show_header_toggle: false\n        entities:\n")
	if len(entities) == 0 {
		output.WriteString("          []\n")
		return
	}
	for _, entity := range entities {
		output.WriteString("          - ")
		output.WriteString(entity)
		output.WriteByte('\n')
	}
}
