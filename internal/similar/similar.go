package similar

import (
	"sort"

	"github.com/Lordymine/codegraph/internal/graph"
)

// Tuning for the SIMILAR_TO pass: 128 hashes split into 32 bands of 4 rows puts the
// LSH probability knee near Jaccard ~0.7 — pairs above are very likely to share a band
// (become candidates), pairs well below are very unlikely, so we avoid the O(n^2) scan.
const (
	shingleK  = 3
	numHashes = 128
	lshBands  = 32
	lshRows   = numHashes / lshBands
)

// Doc is a symbol to compare for near-cloning: its qualified name and token stream.
type Doc struct {
	QN     string
	Tokens []string
}

// SigDoc carries a precomputed MinHash signature — used by the memory-budget indexer
// so tokenized function bodies are not all retained at once.
type SigDoc struct {
	QN  string
	Sig []uint64
}

// Edges returns SIMILAR_TO edges between docs whose estimated Jaccard similarity is at
// least threshold. Candidate pairs come from LSH banding (not an all-pairs scan); each
// surviving pair yields one symmetric pair (smaller QN -> larger) carrying the score.
// Docs too short to form a shingle are ignored (trivial bodies are not clones).
func Edges(project string, docs []Doc, threshold float64) []graph.Edge {
	sigDocs := make([]SigDoc, 0, len(docs))
	for _, d := range docs {
		if len(d.Tokens) >= shingleK {
			sigDocs = append(sigDocs, SigDoc{QN: d.QN, Sig: Signature(d.Tokens, shingleK, numHashes)})
		}
	}
	return EdgesFromSignatures(project, sigDocs, threshold)
}

// EdgesFromSignatures is like Edges but accepts precomputed signatures only.
func EdgesFromSignatures(project string, docs []SigDoc, threshold float64) []graph.Edge {
	sigs := make([][]uint64, len(docs))
	for i, d := range docs {
		sigs[i] = d.Sig
	}
	qns := make([]string, len(docs))
	for i, d := range docs {
		qns[i] = d.QN
	}
	return edgesFromSigs(project, qns, sigs, threshold)
}

func edgesFromSigs(project string, qns []string, sigs [][]uint64, threshold float64) []graph.Edge {

	// LSH: bucket doc indices by (band, band-hash); a shared bucket is a candidate pair.
	type bucket struct{ band, key uint64 }
	buckets := map[bucket][]int{}
	for i, sig := range sigs {
		if sig == nil {
			continue
		}
		for b := 0; b < lshBands; b++ {
			bk := bucket{uint64(b), bandHash(sig[b*lshRows : (b+1)*lshRows])}
			buckets[bk] = append(buckets[bk], i)
		}
	}

	type pair struct{ i, j int }
	seen := map[pair]bool{}
	var edges []graph.Edge
	for _, idxs := range buckets {
		for a := 0; a < len(idxs); a++ {
			for b := a + 1; b < len(idxs); b++ {
				i, j := idxs[a], idxs[b]
				if i > j {
					i, j = j, i
				}
				if seen[pair{i, j}] {
					continue
				}
				seen[pair{i, j}] = true
				score := EstJaccard(sigs[i], sigs[j])
				if score < threshold {
					continue
				}
				src, dst := qns[i], qns[j]
				if src > dst {
					src, dst = dst, src
				}
				edges = append(edges, graph.Edge{
					Project: project, SourceQN: src, TargetQN: dst,
					Type:  graph.EdgeSimilarTo,
					Props: map[string]any{"similarity": round2(score)},
				})
			}
		}
	}

	// LSH bucket iteration is map-random; sort for a deterministic edge set.
	sort.Slice(edges, func(a, b int) bool {
		if edges[a].SourceQN != edges[b].SourceQN {
			return edges[a].SourceQN < edges[b].SourceQN
		}
		return edges[a].TargetQN < edges[b].TargetQN
	})
	return edges
}

// bandHash folds a band's rows into one key (FNV-1a over their bytes).
func bandHash(rows []uint64) uint64 {
	h := uint64(fnvOffset64)
	for _, v := range rows {
		for s := 0; s < 64; s += 8 {
			h ^= (v >> s) & 0xff
			h *= fnvPrime64
		}
	}
	return h
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
