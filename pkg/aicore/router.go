package aicore

import (
	"context"
	"fmt"
)

// RouterPolicy maps task kinds to model names and defines a fallback (FR-13).
type RouterPolicy struct {
	// Models maps each TaskKind to a preferred model name.
	// Empty entries fall through to Default.
	Models  map[TaskKind]string
	// Default is the model used when no task-specific entry exists.
	Default string
	// Fallback is tried once if the primary model returns an error.
	// Empty = no fallback.
	Fallback string
}

// DefaultPolicy returns a RouterPolicy that routes all tasks to qwen3.5:cloud.
// Replace with a multi-model policy when additional providers are integrated.
func DefaultPolicy() RouterPolicy {
	return RouterPolicy{
		Default:  "qwen3.5:cloud",
		Fallback: "",
		Models:   map[TaskKind]string{},
	}
}

type defaultRouter struct {
	client LLMClient
	policy RouterPolicy
}

// NewRouter constructs a ModelRouter backed by client using the given policy.
func NewRouter(client LLMClient, policy RouterPolicy) ModelRouter {
	return &defaultRouter{client: client, policy: policy}
}

func (r *defaultRouter) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	if req.Model == "" {
		if m, ok := r.policy.Models[req.TaskHint]; ok {
			req.Model = m
		} else {
			req.Model = r.policy.Default
		}
	}

	resp, err := r.client.Generate(ctx, req)
	if err != nil && r.policy.Fallback != "" && req.Model != r.policy.Fallback {
		req.Model = r.policy.Fallback
		var fallbackErr error
		resp, fallbackErr = r.client.Generate(ctx, req)
		if fallbackErr != nil {
			return GenerateResponse{}, fmt.Errorf("primary (%w) and fallback: %v", err, fallbackErr)
		}
		return resp, nil // fallback succeeded — primary error is irrelevant
	}
	return resp, err
}
