package index

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/memory"
	"github.com/Lordymine/codegraph/internal/similar"
)

// similarThreshold is the minimum estimated Jaccard for a SIMILAR_TO edge. 0.7 catches
// real near-clones (copy-paste, then rename + a small edit lands ~0.78) while still
// being high similarity over token trigrams — random functions sit near 0, so the
// false-positive risk is low. Tunable; a precision refinement (body-only tokenization,
// identifier normalization) could let it go higher.
const similarThreshold = 0.7

// ResolveSimilar emits SIMILAR_TO edges between near-clone functions/methods. It reads
// each file once, tokenizes every function body, and runs the MinHash + LSH pass
// (internal/similar). Cross-file by nature, so it always runs on the full node set.
// Prefer resolveSimilarFromSpans during indexing — reuses CALLS spans and keeps
// only MinHash signatures in RAM, not tokenized bodies for every function.
func ResolveSimilar(project, root string, nodes []graph.Node) []graph.Edge {
	byFile := map[string][]graph.Node{}
	for _, n := range nodes {
		if n.Label == graph.LabelFunction || n.Label == graph.LabelMethod {
			byFile[n.FilePath] = append(byFile[n.FilePath], n)
		}
	}
	return similarEdgesFromFiles(project, root, byFile)
}

// resolveSimilarFromSpans runs the SIMILAR_TO pass on spans already loaded for CALLS.
// Each file is read once; only MinHash signatures are retained.
func resolveSimilarFromSpans(project, root string, spans []graph.FunctionSpan) ([]graph.Edge, error) {
	byFile := map[string][]graph.FunctionSpan{}
	for _, sp := range spans {
		byFile[sp.FilePath] = append(byFile[sp.FilePath], sp)
	}
	var sigDocs []similar.SigDoc
	for file, fns := range byFile {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		data = nil
		for _, sp := range fns {
			toks := similar.Tokenize(linesOf(lines, sp.StartLine, sp.EndLine))
			if len(toks) >= 3 {
				sigDocs = append(sigDocs, similar.SigDoc{
					QN:  sp.QualifiedName,
					Sig: similar.Signature(toks, 3, 128),
				})
			}
		}
		lines = nil
		memory.Gate()
	}
	return similar.EdgesFromSignatures(project, sigDocs, similarThreshold), nil
}

func similarEdgesFromFiles(project, root string, byFile map[string][]graph.Node) []graph.Edge {
	var docs []similar.Doc
	for file, fns := range byFile {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, n := range fns {
			docs = append(docs, similar.Doc{
				QN:     n.QualifiedName,
				Tokens: similar.Tokenize(linesOf(lines, n.StartLine, n.EndLine)),
			})
		}
	}
	return similar.Edges(project, docs, similarThreshold)
}

// linesOf returns the 1-based inclusive line range [start,end] joined by newlines.
func linesOf(lines []string, start, end int) string {
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return ""
	}
	return strings.Join(lines[start-1:end], "\n")
}
