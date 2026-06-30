package aicore

import "regexp"

// injectionPatterns matches common prompt injection attempts (NFR-17).
// These patterns are replaced with a neutral placeholder before the input
// is interpolated into system prompts or tool arguments.
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above)\s+instructions?`),
	regexp.MustCompile(`(?i)forget\s+(everything|all\s+instructions?)`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+\w+`),
	regexp.MustCompile(`(?i)disregard\s+(all\s+)?(previous|prior)\s+instructions?`),
	regexp.MustCompile(`(?i)act\s+as\s+(if\s+you\s+are|a)\s+\w+`),
}

const injectionPlaceholder = "[filtered]"

// SanitizeInput strips known prompt injection patterns from user-supplied text
// before it is interpolated into system prompts or passed as tool arguments (NFR-17).
// It does not validate or escape the text for SQL/HTML — use parameterized queries
// and encoding functions at those respective boundaries.
func SanitizeInput(input string) string {
	for _, p := range injectionPatterns {
		input = p.ReplaceAllLiteralString(input, injectionPlaceholder)
	}
	return input
}
