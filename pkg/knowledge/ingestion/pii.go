package ingestion

import "regexp"

// PIIScrubber replaces personally identifiable information before embedding (NFR-18).
// Only the chunk content is scrubbed — raw documents in S3 are never modified.
type PIIScrubber interface {
	Scrub(text string) string
}

// piiPattern pairs a compiled regex with a replacement string.
type piiPattern struct {
	re          *regexp.Regexp
	replacement string
}

// regexScrubber is a regex-based PIIScrubber.
type regexScrubber struct{ patterns []piiPattern }

// NewRegexScrubber returns a PIIScrubber that replaces common PII patterns.
// Patterns are compiled once at construction time (safe for concurrent use).
func NewRegexScrubber() PIIScrubber {
	patterns := []struct {
		expr        string
		replacement string
	}{
		{`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`, "[EMAIL]"},
		{`\b(\+?1[\s\-.]?)?\(?\d{3}\)?[\s\-.]?\d{3}[\s\-.]?\d{4}\b`, "[PHONE]"},
		{`\b\d{3}-\d{2}-\d{4}\b`, "[SSN]"},
		{`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12})\b`, "[CARD]"},
		{`\b[A-Z]{1,2}\d{6,9}\b`, "[PASSPORT]"},
	}

	compiled := make([]piiPattern, 0, len(patterns))
	for _, p := range patterns {
		compiled = append(compiled, piiPattern{
			re:          regexp.MustCompile(p.expr),
			replacement: p.replacement,
		})
	}
	return &regexScrubber{patterns: compiled}
}

func (s *regexScrubber) Scrub(text string) string {
	for _, p := range s.patterns {
		text = p.re.ReplaceAllString(text, p.replacement)
	}
	return text
}
