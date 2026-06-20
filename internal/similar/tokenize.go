package similar

import (
	"strings"
	"unicode"
)

// Tokenize splits source into a token stream for similarity: each identifier/number
// run is one token, and every other non-space rune (operators, brackets, punctuation)
// is its own token; whitespace is dropped. Cheap and language-agnostic — near-clones
// share most of this stream regardless of formatting.
func Tokenize(src string) []string {
	var toks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range src {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$':
			cur.WriteRune(r)
		case unicode.IsSpace(r):
			flush()
		default:
			flush()
			toks = append(toks, string(r))
		}
	}
	flush()
	return toks
}
