package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Registry stores tools and enforces ToolKind permission boundaries (NFR-10).
// Read tools cannot invoke Write tools: callers pass the highest ToolKind they
// are permitted to use via maxKind; Execute rejects any tool whose Kind exceeds it.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool. Returns ErrAlreadyExists if the name is taken.
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[t.Name()]; ok {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, t.Name())
	}
	r.tools[t.Name()] = t
	return nil
}

// Get returns the tool by name without executing it.
func (r *Registry) Get(name string) (Tool, error) {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return t, nil
}

// Execute runs the named tool if its Kind() <= maxKind, enforcing NFR-10.
// Pass KindRead for read-only contexts; KindWrite for full-access contexts.
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage, maxKind ToolKind) (json.RawMessage, error) {
	t, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	if t.Kind() > maxKind {
		return nil, fmt.Errorf("%w: %s requires %d, caller allows %d", ErrKindViolation, name, t.Kind(), maxKind)
	}
	return t.Execute(ctx, args)
}
