package graph

import (
	"context"
	"errors"
)

// FR-4: Graph execution errors.
var (
	ErrNodeNotFound          = errors.New("graph: node not found")
	ErrCycleDetected         = errors.New("graph: cycle detected without max-iterations guard")
	ErrMaxIterationsExceeded = errors.New("graph: max iterations exceeded")
)

// NodeInput carries data into a Node from its predecessor.
type NodeInput struct {
	Data map[string]any
}

// NodeOutput carries data out of a Node to its successors.
type NodeOutput struct {
	Data map[string]any
}

// Node is the unit of work in a Graph (FR-4).
type Node interface {
	ID() string
	Run(ctx context.Context, in NodeInput) (NodeOutput, error)
}

// Edge connects a source node to a target node.
//
// Predicate, if non-nil, controls whether this edge is followed.
// MaxIterations > 0 marks this edge as a loop-back; the engine enforces the
// limit and returns ErrMaxIterationsExceeded when the count is exceeded.
// MaxIterations == 0 on an edge that leads to an ancestor returns ErrCycleDetected.
type Edge struct {
	To            string
	Predicate     func(NodeOutput) bool // nil = unconditional
	MaxIterations int                   // 0 = single-traversal (no loop-back)
}

// Graph is the runtime DAG: nodes keyed by ID, outgoing edges keyed by source ID.
type Graph struct {
	Nodes   map[string]Node
	Edges   map[string][]Edge
	StartID string
}

// --- Serialisable form (stored in DB / PROMPT_VERSION) ---

// GraphDef is the JSON-serialisable representation of a Graph.
// Predicates cannot be serialised; attach at runtime via NodeRegistry.BuildGraph.
type GraphDef struct {
	Start string               `json:"start"`
	Nodes map[string]NodeDef   `json:"nodes"`
	Edges map[string][]EdgeDef `json:"edges"`
}

// NodeDef is the serialisable description of a single node.
type NodeDef struct {
	Kind   string         `json:"kind"`
	Params map[string]any `json:"params,omitempty"`
}

// EdgeDef is the serialisable description of a single edge.
// Predicates are nil in deserialised graphs; attach at runtime when needed.
type EdgeDef struct {
	To            string `json:"to"`
	MaxIterations int    `json:"max_iterations,omitempty"`
}
