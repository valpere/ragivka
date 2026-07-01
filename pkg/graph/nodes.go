package graph

import "context"

// EchoNode passes NodeInput through to NodeOutput unchanged.
// Useful as a passthrough or for testing.
type EchoNode struct{ id string }

func NewEchoNode(id string) *EchoNode { return &EchoNode{id: id} }
func (n *EchoNode) ID() string        { return n.id }
func (n *EchoNode) Run(_ context.Context, in NodeInput) (NodeOutput, error) {
	out := NodeOutput{Data: make(map[string]any, len(in.Data))}
	for k, v := range in.Data {
		out.Data[k] = v
	}
	return out, nil
}

// FuncNode wraps an arbitrary function as a Node. Intended for tests and
// one-off graph steps where a named type is unnecessary.
type FuncNode struct {
	id string
	fn func(context.Context, NodeInput) (NodeOutput, error)
}

func NewFuncNode(id string, fn func(context.Context, NodeInput) (NodeOutput, error)) *FuncNode {
	return &FuncNode{id: id, fn: fn}
}
func (n *FuncNode) ID() string { return n.id }
func (n *FuncNode) Run(ctx context.Context, in NodeInput) (NodeOutput, error) {
	return n.fn(ctx, in)
}

// Built-in typed nodes (FR-4). Full LLM / retrieval / tool integration is
// deferred to Phase 3+; stubs satisfy the Node interface now.

// RetrieveNode is a graph step for hybrid-retrieval (integrates with
// pkg/knowledge/retrieval in Phase 3+).
type RetrieveNode struct{ id string }

func NewRetrieveNode(id string) *RetrieveNode { return &RetrieveNode{id: id} }
func (n *RetrieveNode) ID() string            { return n.id }
func (n *RetrieveNode) Run(_ context.Context, in NodeInput) (NodeOutput, error) {
	return NodeOutput(in), nil
}

// GenerateNode is a graph step for LLM generation (integrates with
// pkg/aicore.ModelRouter in Phase 3+).
type GenerateNode struct{ id string }

func NewGenerateNode(id string) *GenerateNode { return &GenerateNode{id: id} }
func (n *GenerateNode) ID() string            { return n.id }
func (n *GenerateNode) Run(_ context.Context, in NodeInput) (NodeOutput, error) {
	return NodeOutput(in), nil
}

// ToolCallNode is a graph step that invokes a registered tool (integrates with
// pkg/tools.Registry in Phase 3+).
type ToolCallNode struct{ id string }

func NewToolCallNode(id string) *ToolCallNode { return &ToolCallNode{id: id} }
func (n *ToolCallNode) ID() string            { return n.id }
func (n *ToolCallNode) Run(_ context.Context, in NodeInput) (NodeOutput, error) {
	return NodeOutput(in), nil
}

// CriticNode is a graph step that evaluates output quality (integrates with
// pkg/aicore + guardrails in Phase 3+).
type CriticNode struct{ id string }

func NewCriticNode(id string) *CriticNode { return &CriticNode{id: id} }
func (n *CriticNode) ID() string          { return n.id }
func (n *CriticNode) Run(_ context.Context, in NodeInput) (NodeOutput, error) {
	return NodeOutput(in), nil
}

// BranchNode routes to one of two downstream paths based on a condition.
// The routing condition is expressed as a Predicate on the outgoing Edge,
// not inside BranchNode itself.
type BranchNode struct{ id string }

func NewBranchNode(id string) *BranchNode { return &BranchNode{id: id} }
func (n *BranchNode) ID() string          { return n.id }
func (n *BranchNode) Run(_ context.Context, in NodeInput) (NodeOutput, error) {
	return NodeOutput(in), nil
}
