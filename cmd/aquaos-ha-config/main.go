// Command aquaos-ha-config renders Home Assistant assets from validated AquaOS configuration.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
	"github.com/tylerkirby004-droid/aquaos/internal/integrations/homeassistant"
)

func main() {
	configuration := flag.String("config", "", "validated AquaOS configuration")
	grafanaURL := flag.String("grafana-url", "", "optional operator-reachable Grafana base URL")
	output := flag.String("output", "", "dashboard output path")
	flag.Parse()
	if *configuration == "" || *output == "" {
		_, _ = fmt.Fprintln(os.Stderr, "config and output are required")
		os.Exit(2)
	}
	cfg, err := config.Load(*configuration)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	payload, err := homeassistant.Dashboard(cfg, *grafanaURL)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err = os.WriteFile(*output, payload, 0o640); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
