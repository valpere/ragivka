package graph_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valpere/ragivka/pkg/graph"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// countNode counts how many times it has been executed and passes data through.
type countNode struct {
	id    string
	count int
}

func newCount(id string) *countNode { return &countNode{id: id} }
func (n *countNode) ID() string     { return n.id }
func (n *countNode) Run(_ context.Context, in graph.NodeInput) (graph.NodeOutput, error) {
	n.count++
	out := graph.NodeOutput{Data: make(map[string]any, len(in.Data))}
	for k, v := range in.Data {
		out.Data[k] = v
	}
	return out, nil
}

func engine() *graph.GraphEngine { return graph.NewGraphEngine() }

// ---------------------------------------------------------------------------
// Linear graph execution
// ---------------------------------------------------------------------------

func TestEngine_LinearGraph_executesAllNodes(t *testing.T) {
	a, b, c := newCount("A"), newCount("B"), newCount("C")
	g := &graph.Graph{
		Nodes:   map[string]graph.Node{"A": a, "B": b, "C": c},
		Edges:   map[string][]graph.Edge{"A": {{To: "B"}}, "B": {{To: "C"}}},
		StartID: "A",
	}

	out, err := engine().Execute(context.Background(), g, graph.NodeInput{Data: map[string]any{"v": 42}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Data["v"] != 42 {
		t.Errorf("output data[v]: got %v, want 42", out.Data["v"])
	}
	if a.count != 1 || b.count != 1 || c.count != 1 {
		t.Errorf("each node must run exactly once: A=%d B=%d C=%d", a.count, b.count, c.count)
	}
}

func TestEngine_SingleNode_noEdges(t *testing.T) {
	n := newCount("only")
	g := &graph.Graph{
		Nodes:   map[string]graph.Node{"only": n},
		Edges:   map[string][]graph.Edge{},
		StartID: "only",
	}
	if _, err := engine().Execute(context.Background(), g, graph.NodeInput{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if n.count != 1 {
		t.Errorf("node run count: got %d, want 1", n.count)
	}
}

func TestEngine_UnknownStartNode_returnsNotFound(t *testing.T) {
	g := &graph.Graph{
		Nodes:   map[string]graph.Node{},
		Edges:   map[string][]graph.Edge{},
		StartID: "missing",
	}
	_, err := engine().Execute(context.Background(), g, graph.NodeInput{})
	if !errors.Is(err, graph.ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestEngine_UnknownTargetNode_returnsNotFound(t *testing.T) {
	a := newCount("A")
	g := &graph.Graph{
		Nodes:   map[string]graph.Node{"A": a},
		Edges:   map[string][]graph.Edge{"A": {{To: "ghost"}}},
		StartID: "A",
	}
	_, err := engine().Execute(context.Background(), g, graph.NodeInput{})
	if !errors.Is(err, graph.ErrNodeNotFound) {
		t.Errorf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestEngine_NodeRunError_propagates(t *testing.T) {
	boom := graph.NewFuncNode("boom", func(_ context.Context, _ graph.NodeInput) (graph.NodeOutput, error) {
		return graph.NodeOutput{}, errors.New("simulated failure")
	})
	g := &graph.Graph{
		Nodes:   map[string]graph.Node{"boom": boom},
		Edges:   map[string][]graph.Edge{},
		StartID: "boom",
	}
	_, err := engine().Execute(context.Background(), g, graph.NodeInput{})
	if err == nil {
		t.Error("expected error from failing node, got nil")
	}
}

// ---------------------------------------------------------------------------
// Branch condition routing
// ---------------------------------------------------------------------------

func TestEngine_BranchRouting_takesFirstMatchingEdge(t *testing.T) {
	pathTrue  := newCount("pathTrue")
	pathFalse := newCount("pathFalse")

	mkGraph := func(branchVal bool) *graph.Graph {
		entry := graph.NewFuncNode("entry", func(_ context.Context, _ graph.NodeInput) (graph.NodeOutput, error) {
			return graph.NodeOutput{Data: map[string]any{"branch": branchVal}}, nil
		})
		return &graph.Graph{
			Nodes: map[string]graph.Node{
				"entry":     entry,
				"pathTrue":  pathTrue,
				"pathFalse": pathFalse,
			},
			Edges: map[string][]graph.Edge{
				"entry": {
					{To: "pathTrue",  Predicate: func(o graph.NodeOutput) bool { return o.Data["branch"] == true }},
					{To: "pathFalse", Predicate: func(o graph.NodeOutput) bool { return o.Data["branch"] == false }},
				},
			},
			StartID: "entry",
		}
	}

	eng := engine()

	if _, err := eng.Execute(context.Background(), mkGraph(true), graph.NodeInput{}); err != nil {
		t.Fatalf("Execute(true): %v", err)
	}
	if pathTrue.count != 1 || pathFalse.count != 0 {
		t.Errorf("true branch: pathTrue=%d (want 1) pathFalse=%d (want 0)", pathTrue.count, pathFalse.count)
	}

	if _, err := eng.Execute(context.Background(), mkGraph(false), graph.NodeInput{}); err != nil {
		t.Fatalf("Execute(false): %v", err)
	}
	if pathTrue.count != 1 || pathFalse.count != 1 {
		t.Errorf("false branch: pathTrue=%d (want 1) pathFalse=%d (want 1)", pathTrue.count, pathFalse.count)
	}
}

func TestEngine_NoMatchingEdge_treatsAsTerminal(t *testing.T) {
	entry := graph.NewFuncNode("entry", func(_ context.Context, _ graph.NodeInput) (graph.NodeOutput, error) {
		return graph.NodeOutput{Data: map[string]any{"x": 0}}, nil
	})
	never := newCount("never")
	g := &graph.Graph{
		Nodes: map[string]graph.Node{"entry": entry, "never": never},
		Edges: map[string][]graph.Edge{
			"entry": {{To: "never", Predicate: func(o graph.NodeOutput) bool { return o.Data["x"] == 99 }}},
		},
		StartID: "entry",
	}
	if _, err := engine().Execute(context.Background(), g, graph.NodeInput{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if never.count != 0 {
		t.Errorf("'never' node should not run: got count %d", never.count)
	}
}

// ---------------------------------------------------------------------------
// Loop termination at max iterations
// ---------------------------------------------------------------------------

func TestEngine_LoopSelf_terminatesAtMaxIterations(t *testing.T) {
	work := newCount("work")
	const maxIter = 3
	g := &graph.Graph{
		Nodes:   map[string]graph.Node{"work": work},
		Edges:   map[string][]graph.Edge{"work": {{To: "work", MaxIterations: maxIter}}},
		StartID: "work",
	}

	_, err := engine().Execute(context.Background(), g, graph.NodeInput{})
	if !errors.Is(err, graph.ErrMaxIterationsExceeded) {
		t.Fatalf("expected ErrMaxIterationsExceeded, got %v", err)
	}
	// Initial run + maxIter loop-backs before the (maxIter+1)-th attempt is blocked.
	if work.count != maxIter+1 {
		t.Errorf("work node run count: got %d, want %d", work.count, maxIter+1)
	}
}

func TestEngine_LoopBackEdge_terminatesAtMaxIterations(t *testing.T) {
	a, b := newCount("A"), newCount("B")
	// A → B → A (loop-back), MaxIterations=2 on B→A.
	g := &graph.Graph{
		Nodes: map[string]graph.Node{"A": a, "B": b},
		Edges: map[string][]graph.Edge{
			"A": {{To: "B"}},
			"B": {{To: "A", MaxIterations: 2}},
		},
		StartID: "A",
	}

	_, err := engine().Execute(context.Background(), g, graph.NodeInput{})
	if !errors.Is(err, graph.ErrMaxIterationsExceeded) {
		t.Fatalf("expected ErrMaxIterationsExceeded, got %v", err)
	}
	// Pattern: A B A B A B | blocked
	// A runs: iteration 0 start, then after each loop-back: 3 times total.
	if a.count != 3 {
		t.Errorf("A run count: got %d, want 3", a.count)
	}
	if b.count != 3 {
		t.Errorf("B run count: got %d, want 3", b.count)
	}
}

func TestEngine_CycleWithoutGuard_returnsCycleDetected(t *testing.T) {
	a, b := newCount("A"), newCount("B")
	// A → B → A (MaxIterations=0 on B→A → cycle error).
	g := &graph.Graph{
		Nodes: map[string]graph.Node{"A": a, "B": b},
		Edges: map[string][]graph.Edge{
			"A": {{To: "B"}},
			"B": {{To: "A"}}, // no MaxIterations → ErrCycleDetected
		},
		StartID: "A",
	}

	_, err := engine().Execute(context.Background(), g, graph.NodeInput{})
	if !errors.Is(err, graph.ErrCycleDetected) {
		t.Errorf("expected ErrCycleDetected, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// NodeRegistry / GraphDef round-trip
// ---------------------------------------------------------------------------

func TestDefaultNodeRegistry_BuildGraph_linearDef(t *testing.T) {
	def := graph.GraphDef{
		Start: "step1",
		Nodes: map[string]graph.NodeDef{
			"step1": {Kind: "echo"},
			"step2": {Kind: "echo"},
		},
		Edges: map[string][]graph.EdgeDef{
			"step1": {{To: "step2"}},
		},
	}

	reg := graph.DefaultNodeRegistry()
	g, err := reg.BuildGraph(def)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if g.StartID != "step1" {
		t.Errorf("StartID: got %q, want step1", g.StartID)
	}

	out, err := engine().Execute(context.Background(), g, graph.NodeInput{Data: map[string]any{"k": "v"}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Data["k"] != "v" {
		t.Errorf("output data[k]: got %v, want v", out.Data["k"])
	}
}

func TestNodeRegistry_UnknownKind_returnsError(t *testing.T) {
	def := graph.GraphDef{
		Start: "n",
		Nodes: map[string]graph.NodeDef{"n": {Kind: "nonexistent"}},
	}
	reg := graph.DefaultNodeRegistry()
	if _, err := reg.BuildGraph(def); err == nil {
		t.Error("expected error for unknown node kind, got nil")
	}
}
