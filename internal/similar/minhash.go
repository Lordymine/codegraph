// Package similar finds near-clone code via MinHash over token shingles — the M4
// SIMILAR_TO pass. No embeddings or model: a fixed-size MinHash signature per symbol
// estimates pairwise Jaccard similarity cheaply, and LSH banding (later) turns the
// O(n^2) all-pairs comparison into candidate buckets.
package similar

import (
	"math"
	"strings"
)

const (
	fnvOffset64 = 1469598103934665603
	fnvPrime64  = 1099511628211
)

// hash64 is FNV-1a over a string.
func hash64(s string) uint64 {
	h := uint64(fnvOffset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	return h
}

// splitmix64 mixes a seed into a well-distributed value. Used to derive the per-hash
// permutation constants deterministically, so a signature is reproducible across runs.
func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// shingleHashes returns the deduplicated k-gram shingle hashes of the token stream.
// A stream shorter than k is hashed whole, so tiny bodies still compare.
func shingleHashes(tokens []string, k int) []uint64 {
	if k < 1 {
		k = 1
	}
	seen := map[uint64]bool{}
	var out []uint64
	add := func(h uint64) {
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	if len(tokens) < k {
		add(hash64(strings.Join(tokens, "\x00")))
		return out
	}
	for i := 0; i+k <= len(tokens); i++ {
		add(hash64(strings.Join(tokens[i:i+k], "\x00")))
	}
	return out
}

// Signature is the MinHash signature of the token stream: numHashes positions, each
// the minimum of a distinct universal-hash permutation (a*h+b) over the shingle-hash
// set. The fraction of equal positions between two signatures estimates the Jaccard
// similarity of their shingle sets (see EstJaccard).
func Signature(tokens []string, k, numHashes int) []uint64 {
	sh := shingleHashes(tokens, k)
	sig := make([]uint64, numHashes)
	for i := range sig {
		a := splitmix64(uint64(2*i+1)) | 1 // odd multiplier for a full-period permutation
		b := splitmix64(uint64(2*i + 2))
		min := uint64(math.MaxUint64)
		for _, h := range sh {
			if v := a*h + b; v < min {
				min = v
			}
		}
		sig[i] = min
	}
	return sig
}

// EstJaccard estimates the Jaccard similarity of two token streams from their MinHash
// signatures: the fraction of positions that agree. Signatures must be the same size.
func EstJaccard(a, b []uint64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	eq := 0
	for i := range a {
		if a[i] == b[i] {
			eq++
		}
	}
	return float64(eq) / float64(len(a))
}
