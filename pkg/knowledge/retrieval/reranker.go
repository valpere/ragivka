package retrieval

import "sort"

// dotProductReranker reorders candidates by VecScore (cosine similarity) alone,
// approximating bi-encoder rescoring without an extra embedding fetch (FR-11).
// Phase 3 can swap this for a full cross-encoder via the Reranker interface.
type dotProductReranker struct{}

// NewDotProductReranker returns a Reranker backed by cosine-similarity rescoring.
func NewDotProductReranker() Reranker { return &dotProductReranker{} }

func (r *dotProductReranker) Rerank(_ string, candidates []RankedChunk, topK int) []RankedChunk {
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].VecScore > candidates[j].VecScore
	})
	if topK > 0 && len(candidates) > topK {
		return candidates[:topK]
	}
	return candidates
}
