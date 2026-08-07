package output

import (
	"context"
	"testing"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

type acceptingExecutor struct{}

func (acceptingExecutor) Dispatch(context.Context, Command) (Acknowledgement, error) {
	return Acknowledgement{Accepted: true}, nil
}

func TestExecutorRouterRejectsUnknownAndRoutesOwnedEquipment(t *testing.T) {
	t.Parallel()
	router := NewExecutorRouter()
	owned := domain.EquipmentID("11111111-1111-4111-8111-111111111111")
	unknown := domain.EquipmentID("22222222-2222-4222-8222-222222222222")
	if err := router.Register(owned, acceptingExecutor{}); err != nil {
		t.Fatal(err)
	}
	ack, err := router.Dispatch(context.Background(), Command{EquipmentID: unknown})
	if err != nil || ack.Accepted {
		t.Fatalf("unknown route was not rejected: %+v %v", ack, err)
	}
	ack, err = router.Dispatch(context.Background(), Command{EquipmentID: owned})
	if err != nil || !ack.Accepted {
		t.Fatalf("owned route was not accepted: %+v %v", ack, err)
	}
	if err := router.Register(owned, acceptingExecutor{}); err == nil {
		t.Fatal("expected duplicate registration rejection")
	}
}
