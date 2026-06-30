package tools

import "fmt"

// HITLGate evaluates an AI confidence score against a threshold (FR-18, NFR-14).
// When confidence is below threshold, returns ErrHITLRequired — the caller is
// responsible for transitioning the session to WaitingForHuman via SessionRepository.
// Runtime blocking deferred to Phase 3; this gate is the decision boundary only.
type HITLGate struct {
	threshold float64
}

func NewHITLGate(threshold float64) *HITLGate {
	return &HITLGate{threshold: threshold}
}

// Evaluate returns ErrHITLRequired if confidence < threshold.
func (g *HITLGate) Evaluate(confidence float64) error {
	if confidence < g.threshold {
		return fmt.Errorf("%w (confidence=%.3f, threshold=%.3f)", ErrHITLRequired, confidence, g.threshold)
	}
	return nil
}
