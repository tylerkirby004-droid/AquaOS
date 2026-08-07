package storage

import (
	"context"
	"testing"
	"time"

	"github.com/tylerkirby004-droid/aquaos/internal/health"
)

type contractStorage struct{}

func (contractStorage) Name() string                { return "contract" }
func (contractStorage) Start(context.Context) error { return nil }
func (contractStorage) Stop(context.Context) error  { return nil }
func (contractStorage) Health() health.Status {
	return health.Status{Name: "contract", Healthy: true, CheckedAt: time.Now()}
}

func TestStorageContractRequiresLifecycleHealth(t *testing.T) {
	var backend Storage = contractStorage{}
	if backend.Name() == "" || !backend.Health().Healthy {
		t.Fatal("storage contract omitted lifecycle health")
	}
}
