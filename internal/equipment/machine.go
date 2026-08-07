package equipment

import "errors"

// OperatingState is the reconciled equipment lifecycle state.
type OperatingState string

//nolint:revive // OperatingState values are documented collectively by OperatingState.
const (
	StateUnknown  OperatingState = "unknown"
	StateOff      OperatingState = "off"
	StateStarting OperatingState = "starting"
	StateOn       OperatingState = "on"
	StateStopping OperatingState = "stopping"
	StateFault    OperatingState = "fault"
)

// Transition identifies an equipment lifecycle input.
type Transition string

//nolint:revive // Transition values are documented collectively by Transition.
const (
	TransitionCommandOn       Transition = "command-on"
	TransitionCommandOff      Transition = "command-off"
	TransitionAcknowledgedOn  Transition = "acknowledged-on"
	TransitionAcknowledgedOff Transition = "acknowledged-off"
	TransitionReportedOn      Transition = "reported-on"
	TransitionReportedOff     Transition = "reported-off"
	TransitionFault           Transition = "fault"
	TransitionReset           Transition = "reset"
)

// ErrInvalidStateTransition rejects impossible equipment transitions.
var ErrInvalidStateTransition = errors.New("invalid equipment state transition")

// Machine is a small explicit equipment state machine. Acknowledgement alone
// never produces StateOn or StateOff; reported-state reconciliation does.
type Machine struct {
	state        OperatingState
	acknowledged bool
}

// NewMachine constructs an equipment state machine in the unknown state.
func NewMachine() *Machine { return &Machine{state: StateUnknown} }

// State returns the current immutable state value.
func (m *Machine) State() OperatingState { return m.state }

// Apply validates and applies one transition.
func (m *Machine) Apply(transition Transition) (OperatingState, error) {
	if transition == TransitionFault {
		m.state = StateFault
		m.acknowledged = false
		return m.state, nil
	}
	switch m.state {
	case StateUnknown:
		switch transition {
		case TransitionReportedOn:
			m.state = StateOn
		case TransitionReportedOff:
			m.state = StateOff
		default:
			return m.state, ErrInvalidStateTransition
		}
	case StateOff:
		switch transition {
		case TransitionCommandOn:
			m.state = StateStarting
			m.acknowledged = false
		case TransitionReportedOn:
			m.state = StateOn
		default:
			return m.state, ErrInvalidStateTransition
		}
	case StateStarting:
		switch transition {
		case TransitionAcknowledgedOn:
			m.acknowledged = true
		case TransitionReportedOn:
			if !m.acknowledged {
				return m.state, ErrInvalidStateTransition
			}
			m.state = StateOn
			m.acknowledged = false
		case TransitionCommandOff:
			m.state = StateStopping
			m.acknowledged = false
		default:
			return m.state, ErrInvalidStateTransition
		}
	case StateOn:
		switch transition {
		case TransitionCommandOff:
			m.state = StateStopping
			m.acknowledged = false
		case TransitionReportedOff:
			m.state = StateOff
		default:
			return m.state, ErrInvalidStateTransition
		}
	case StateStopping:
		switch transition {
		case TransitionAcknowledgedOff:
			m.acknowledged = true
		case TransitionReportedOff:
			if !m.acknowledged {
				return m.state, ErrInvalidStateTransition
			}
			m.state = StateOff
			m.acknowledged = false
		default:
			return m.state, ErrInvalidStateTransition
		}
	case StateFault:
		if transition != TransitionReset {
			return m.state, ErrInvalidStateTransition
		}
		m.state = StateUnknown
		m.acknowledged = false
	default:
		return m.state, ErrInvalidStateTransition
	}
	return m.state, nil
}
