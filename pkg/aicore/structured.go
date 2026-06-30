package aicore

import (
	"encoding/json"
	"fmt"
)

// ParseStructured parses an LLM JSON response into T (FR-15).
// The request must have ForceJSON=true to ensure the model outputs valid JSON.
// Returns an error if content is not valid JSON or cannot be decoded into T.
func ParseStructured[T any](content string) (T, error) {
	var result T
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return result, fmt.Errorf("structured output parse failed: %w", err)
	}
	return result, nil
}
