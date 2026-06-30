package graph

import "fmt"

// NodeFactory creates a Node from a NodeDef. The id comes from the map key
// in GraphDef.Nodes, not from NodeDef.Kind.
type NodeFactory func(id string, def NodeDef) (Node, error)

// NodeRegistry maps node kinds to factories, enabling Graph construction from
// a serialisable GraphDef.
type NodeRegistry struct {
	factories map[string]NodeFactory
}

// NewNodeRegistry returns an empty NodeRegistry.
func NewNodeRegistry() *NodeRegistry {
	return &NodeRegistry{factories: make(map[string]NodeFactory)}
}

// DefaultNodeRegistry returns a NodeRegistry pre-loaded with all built-in node kinds.
func DefaultNodeRegistry() *NodeRegistry {
	r := NewNodeRegistry()
	r.Register("echo", func(id string, _ NodeDef) (Node, error) { return NewEchoNode(id), nil })
	r.Register("retrieve", func(id string, _ NodeDef) (Node, error) { return NewRetrieveNode(id), nil })
	r.Register("generate", func(id string, _ NodeDef) (Node, error) { return NewGenerateNode(id), nil })
	r.Register("tool_call", func(id string, _ NodeDef) (Node, error) { return NewToolCallNode(id), nil })
	r.Register("critic", func(id string, _ NodeDef) (Node, error) { return NewCriticNode(id), nil })
	r.Register("branch", func(id string, _ NodeDef) (Node, error) { return NewBranchNode(id), nil })
	return r
}

// Register adds a factory for the given node kind.
func (r *NodeRegistry) Register(kind string, factory NodeFactory) {
	r.factories[kind] = factory
}

// Build instantiates a Node from the given NodeDef using a registered factory.
func (r *NodeRegistry) Build(id string, def NodeDef) (Node, error) {
	factory, ok := r.factories[def.Kind]
	if !ok {
		return nil, fmt.Errorf("graph: unknown node kind %q", def.Kind)
	}
	return factory(id, def)
}

// BuildGraph constructs a runtime Graph from a serialisable GraphDef.
// Edges from GraphDef have nil Predicates (unconditional); attach predicates
// at runtime if conditional routing is required.
func (r *NodeRegistry) BuildGraph(gd GraphDef) (*Graph, error) {
	nodes := make(map[string]Node, len(gd.Nodes))
	for id, def := range gd.Nodes {
		n, err := r.Build(id, def)
		if err != nil {
			return nil, err
		}
		nodes[id] = n
	}

	edges := make(map[string][]Edge, len(gd.Edges))
	for src, defs := range gd.Edges {
		for _, ed := range defs {
			edges[src] = append(edges[src], Edge{
				To:            ed.To,
				MaxIterations: ed.MaxIterations,
			})
		}
	}

	return &Graph{Nodes: nodes, Edges: edges, StartID: gd.Start}, nil
}
