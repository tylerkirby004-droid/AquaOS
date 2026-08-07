package plugins

import (
	"context"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

type contractPlugin struct{}

func (contractPlugin) Name() string                { return "contract" }
func (contractPlugin) Start(context.Context) error { return nil }
func (contractPlugin) Stop(context.Context) error  { return nil }
func (contractPlugin) Health() health.Status {
	return health.Status{Name: "contract", Healthy: true, CheckedAt: time.Now()}
}
func (contractPlugin) Manifest() Manifest {
	return Manifest{ID: "contract", Name: "Contract", Version: "1.0.0"}
}
func (contractPlugin) Initialize(context.Context, Host) error { return nil }

func TestPluginContractIncludesLifecycleAndManifest(t *testing.T) {
	var plugin Plugin = contractPlugin{}
	if plugin.Manifest().ID == "" || !plugin.Health().Healthy {
		t.Fatal("plugin contract did not expose identity and health")
	}
}
