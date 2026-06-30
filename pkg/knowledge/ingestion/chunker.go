package ingestion

import "github.com/valpere/ragivka/pkg/knowledge"

// ChunkConfig controls the fixed-size character chunker (FR-9).
type ChunkConfig struct {
	// Size is the maximum chunk size in characters.
	Size int
	// Overlap is the number of characters carried over from the previous chunk
	// to preserve context across boundaries.
	Overlap int
}

// DefaultChunkConfig returns a sensible default for bge-m3 (max 8192 tokens ≈ 32 768 chars).
func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{Size: 2000, Overlap: 200}
}

// Chunk splits text into overlapping character windows (FR-9).
// Token count is estimated at 4 characters per token (rough approximation).
func Chunk(text string, cfg ChunkConfig) []knowledge.Chunk {
	if cfg.Size <= 0 {
		cfg = DefaultChunkConfig()
	}
	runes := []rune(text)
	total := len(runes)
	if total == 0 {
		return nil
	}

	step := cfg.Size - cfg.Overlap
	if step <= 0 {
		step = cfg.Size
	}

	var chunks []knowledge.Chunk
	for start, idx := 0, 0; start < total; start, idx = start+step, idx+1 {
		end := start + cfg.Size
		if end > total {
			end = total
		}
		content := string(runes[start:end])
		chunks = append(chunks, knowledge.Chunk{
			Content:    content,
			ChunkIndex: idx,
			TokenCount: len([]rune(content)) / 4,
		})
		if end == total {
			break
		}
	}
	return chunks
}
