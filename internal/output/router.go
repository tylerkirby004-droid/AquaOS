package output

import (
	"context"
	"errors"
	"sync"

	"github.com/tylerkirby004-droid/aquaos/internal/domain"
)

// ExecutorRouter dispatches each equipment command to its explicitly
// registered owning adapter. Registration happens only in the composition root;
// missing routes reject safely.
type ExecutorRouter struct {
	mu       sync.RWMutex
	routes   map[domain.EquipmentID]Executor
	fallback Executor
}

// NewExecutorRouter constructs an empty router with a rejecting fallback.
func NewExecutorRouter() *ExecutorRouter {
	return &ExecutorRouter{routes: make(map[domain.EquipmentID]Executor), fallback: RejectingExecutor{}}
}

// Register assigns one equipment identity to one executor.
func (r *ExecutorRouter) Register(id domain.EquipmentID, executor Executor) error {
	if err := id.Validate(); err != nil {
		return err
	}
	if executor == nil {
		return errors.New("output executor is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.routes[id]; exists {
		return errors.New("output executor already registered")
	}
	r.routes[id] = executor
	return nil
}

// Dispatch routes a command without applying policy; Service owns validation.
func (r *ExecutorRouter) Dispatch(ctx context.Context, command Command) (Acknowledgement, error) {
	r.mu.RLock()
	executor := r.routes[command.EquipmentID]
	fallback := r.fallback
	r.mu.RUnlock()
	if executor == nil {
		executor = fallback
	}
	return executor.Dispatch(ctx, command)
}
