package runtime

import "testing"

func TestAllowed_validTransitions(t *testing.T) {
	cases := []struct{ from, to State }{
		{StateActive, StateWaitingForHuman},
		{StateActive, StateCompleted},
		{StateActive, StateExpired},
		{StateWaitingForHuman, StateActive},
		{StateWaitingForHuman, StateCompleted},
	}
	for _, c := range cases {
		if !Allowed(c.from, c.to) {
			t.Errorf("Allowed(%s, %s) = false, want true", c.from, c.to)
		}
	}
}

func TestAllowed_invalidTransitions(t *testing.T) {
	cases := []struct{ from, to State }{
		{StateCompleted, StateActive},
		{StateCompleted, StateWaitingForHuman},
		{StateExpired, StateActive},
		{StateExpired, StateWaitingForHuman},
		{StateWaitingForHuman, StateExpired},
		{StateActive, StateActive},
	}
	for _, c := range cases {
		if Allowed(c.from, c.to) {
			t.Errorf("Allowed(%s, %s) = true, want false", c.from, c.to)
		}
	}
}

func TestErrInvalidTransition_Error(t *testing.T) {
	err := ErrInvalidTransition{From: StateActive, To: StateActive}
	want := "invalid FSM transition: Active → Active"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}
