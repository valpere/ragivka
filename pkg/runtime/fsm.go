package runtime

import "fmt"

// validTransitions encodes the FSM spec (FR-5).
// Expired and Completed are terminal — they appear as values but never as keys.
var validTransitions = map[State][]State{
	StateActive:          {StateWaitingForHuman, StateCompleted, StateExpired},
	StateWaitingForHuman: {StateActive, StateCompleted},
}

// ErrInvalidTransition is returned when a state change violates the FSM spec.
type ErrInvalidTransition struct{ From, To State }

func (e ErrInvalidTransition) Error() string {
	return fmt.Sprintf("invalid FSM transition: %s → %s", e.From, e.To)
}

// Allowed reports whether the from→to transition is permitted by the FSM.
func Allowed(from, to State) bool {
	for _, s := range validTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}
