package equipment

import (
	"errors"
	"testing"
)

func TestMachineRequiresAcknowledgementBeforeCommandedReconciliation(t *testing.T) {
	machine := NewMachine()
	steps := []struct {
		transition Transition
		want       OperatingState
	}{{TransitionReportedOff, StateOff}, {TransitionCommandOn, StateStarting}, {TransitionAcknowledgedOn, StateStarting}, {TransitionReportedOn, StateOn}, {TransitionCommandOff, StateStopping}, {TransitionAcknowledgedOff, StateStopping}, {TransitionReportedOff, StateOff}}
	for _, step := range steps {
		got, err := machine.Apply(step.transition)
		if err != nil {
			t.Fatalf("Apply(%s): %v", step.transition, err)
		}
		if got != step.want {
			t.Fatalf("Apply(%s)=%s want %s", step.transition, got, step.want)
		}
	}
}

func TestMachineDoesNotTreatReportedStateAsCommandAcknowledgement(t *testing.T) {
	machine := NewMachine()
	_, _ = machine.Apply(TransitionReportedOff)
	_, _ = machine.Apply(TransitionCommandOn)
	if _, err := machine.Apply(TransitionReportedOn); !errors.Is(err, ErrInvalidStateTransition) {
		t.Fatalf("error=%v", err)
	}
}
