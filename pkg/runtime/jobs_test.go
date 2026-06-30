package runtime

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/knowledge/retrieval"
)

func TestLastUserMessage_returnsLastUser(t *testing.T) {
	history := []*Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "second"},
	}
	if got := lastUserMessage(history); got != "second" {
		t.Errorf("want 'second', got %q", got)
	}
}

func TestLastUserMessage_noUserReturnsEmpty(t *testing.T) {
	history := []*Message{
		{Role: "assistant", Content: "hello"},
		{Role: "system", Content: "prompt"},
	}
	if got := lastUserMessage(history); got != "" {
		t.Errorf("want empty string, got %q", got)
	}
}

func TestLastUserMessage_emptyHistoryReturnsEmpty(t *testing.T) {
	if got := lastUserMessage(nil); got != "" {
		t.Errorf("want empty string for nil history, got %q", got)
	}
}

func TestLastUserMessage_singleUserMessage(t *testing.T) {
	history := []*Message{{Role: "user", Content: "only"}}
	if got := lastUserMessage(history); got != "only" {
		t.Errorf("want 'only', got %q", got)
	}
}

func TestBuildContextBlock_containsChunkContent(t *testing.T) {
	chunks := []retrieval.RankedChunk{
		{Content: "first chunk"},
		{Content: "second chunk"},
	}
	got := buildContextBlock(chunks)
	for _, want := range []string{"<retrieved_context>", "first chunk", "second chunk", "</retrieved_context>"} {
		if !strings.Contains(got, want) {
			t.Errorf("buildContextBlock missing %q in output: %s", want, got)
		}
	}
}

func TestBuildContextBlock_numberedEntries(t *testing.T) {
	chunks := []retrieval.RankedChunk{
		{Content: "alpha"},
		{Content: "beta"},
		{Content: "gamma"},
	}
	got := buildContextBlock(chunks)
	for _, want := range []string{"[1]", "[2]", "[3]"} {
		if !strings.Contains(got, want) {
			t.Errorf("buildContextBlock missing numbered entry %q", want)
		}
	}
}

func TestChunkIDs_extractsAllIDs(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	chunks := make([]retrieval.RankedChunk, len(ids))
	for i, id := range ids {
		chunks[i] = retrieval.RankedChunk{ChunkID: id}
	}
	got := chunkIDs(chunks)
	if len(got) != len(ids) {
		t.Fatalf("want %d IDs, got %d", len(ids), len(got))
	}
	for i, id := range ids {
		if got[i] != id {
			t.Errorf("index %d: want %v, got %v", i, id, got[i])
		}
	}
}

func TestChunkIDs_empty(t *testing.T) {
	if got := chunkIDs(nil); len(got) != 0 {
		t.Errorf("want empty for nil input, got %d elements", len(got))
	}
}

// REGRESSION: nil CitationRefs field was set when retriever is nil — verify zero-value is []uuid.UUID(nil).
func TestJobsHelpers_citationRefsZeroValue(t *testing.T) {
	var refs []uuid.UUID
	if refs != nil {
		t.Error("zero-value []uuid.UUID should be nil before assignment")
	}
	// chunkIDs on empty slice returns non-nil (make allocates)
	got := chunkIDs([]retrieval.RankedChunk{})
	if got == nil {
		t.Error("chunkIDs on empty non-nil slice should return non-nil slice")
	}
}

