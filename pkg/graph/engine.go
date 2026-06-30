package graph

import (
	"context"
	"fmt"
)

// GraphEngine executes a Graph sequentially from StartID (FR-4).
//
// Algorithm: single-path walk — at each step run the current node, pick the
// first matching outgoing edge (predicate check), advance.
//
// Loop-back detection: the engine maintains the current traversal path as a
// stack. An edge whose target is already on the stack is a loop-back.
//   - MaxIterations == 0 on a loop-back edge → ErrCycleDetected.
//   - MaxIterations > 0 → engine tracks per-edge traversal count; when the
//     count exceeds the limit → ErrMaxIterationsExceeded.
//
// On a valid loop-back the stack is popped back to the target node so forward
// edges from that target are not mistakenly treated as back-edges in subsequent
// iterations.
type GraphEngine struct{}

// NewGraphEngine constructs a GraphEngine.
func NewGraphEngine() *GraphEngine { return &GraphEngine{} }

// Execute runs g from g.StartID, propagating each NodeOutput as the next
// NodeInput. Returns the NodeOutput of the last terminal node or an error.
func (e *GraphEngine) Execute(ctx context.Context, g *Graph, initialInput NodeInput) (NodeOutput, error) {
	if _, ok := g.Nodes[g.StartID]; !ok {
		return NodeOutput{}, fmt.Errorf("%w: %s", ErrNodeNotFound, g.StartID)
	}

	type frame struct{ nodeID string }

	// path tracks the current traversal stack.
	// pathIdx maps nodeID → index in path (present = on stack).
	path    := []frame{{g.StartID}}
	pathIdx := map[string]int{g.StartID: 0}
	iters   := map[string]int{} // "from:to" → traversal count

	currentID    := g.StartID
	currentInput := initialInput
	var lastOutput NodeOutput

	for {
		node, ok := g.Nodes[currentID]
		if !ok {
			return NodeOutput{}, fmt.Errorf("%w: %s", ErrNodeNotFound, currentID)
		}

		out, err := node.Run(ctx, currentInput)
		if err != nil {
			return NodeOutput{}, fmt.Errorf("graph: node %q: %w", currentID, err)
		}
		lastOutput = out

		// Pick first matching outgoing edge.
		var next *Edge
		for i := range g.Edges[currentID] {
			edg := &g.Edges[currentID][i]
			if edg.Predicate == nil || edg.Predicate(out) {
				next = edg
				break
			}
		}
		if next == nil {
			break // terminal node — no matching outgoing edge
		}

		if _, ok := g.Nodes[next.To]; !ok {
			return NodeOutput{}, fmt.Errorf("%w: %s", ErrNodeNotFound, next.To)
		}

		edgeKey := currentID + ":" + next.To

		if idx, inPath := pathIdx[next.To]; inPath {
			// Loop-back edge — target is an ancestor on the current path.
			if next.MaxIterations == 0 {
				return NodeOutput{}, fmt.Errorf("%w: edge %s → %s requires MaxIterations > 0",
					ErrCycleDetected, currentID, next.To)
			}
			iters[edgeKey]++
			if iters[edgeKey] > next.MaxIterations {
				return NodeOutput{}, fmt.Errorf("%w: edge %s → %s (limit %d)",
					ErrMaxIterationsExceeded, currentID, next.To, next.MaxIterations)
			}
			// Pop path back to the loop-back target so nodes between it and
			// the current node are no longer considered "on path".
			for i := len(path) - 1; i > idx; i-- {
				delete(pathIdx, path[i].nodeID)
			}
			path = path[:idx+1]
		} else {
			// Forward edge — extend path.
			pathIdx[next.To] = len(path)
			path = append(path, frame{next.To})
		}

		currentID = next.To
		currentInput = NodeInput{Data: out.Data}
	}

	return lastOutput, nil
}
