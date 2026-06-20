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
// the minimum over the shingle-hash set of a distinct strong-mix permutation
// (splitmix64 of the shingle XOR a per-position seed). The fraction of equal positions
// between two signatures estimates the Jaccard similarity of their shingle sets (see
// EstJaccard). A strong avalanche permutation matters: the cheap a*h+b universal hash
// is only 2-universal and its MinHash estimate is too noisy at a tight threshold.
func Signature(tokens []string, k, numHashes int) []uint64 {
	sh := shingleHashes(tokens, k)
	sig := make([]uint64, numHashes)
	for i := range sig {
		seed := uint64(i) * 0x9e3779b97f4a7c15 // distinct per position
		min := uint64(math.MaxUint64)
		for _, h := range sh {
			if v := splitmix64(h ^ seed); v < min {
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
