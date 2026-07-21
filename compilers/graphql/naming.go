package graphql

import (
	"strings"
	"unicode"

	"github.com/dexpace/morphic/ir"
)

// namingFor builds the neutral Naming of a declared GraphQL name: the source
// spelling plus a canonical lower_snake word sequence. Emitters own all casing,
// acronym policy, and reserved-word escaping (invariant 4); the IR never stores
// a cased identifier.
func namingFor(name string) ir.Naming {
	return ir.Naming{Source: name, Canonical: canonicalWords(name)}
}

// canonicalWords renders name as a neutral lower_snake word sequence: it splits
// on _/-/space and on camel-case and letter/digit boundaries, lowercases, and
// joins with "_". It holds no acronym opinion beyond boundary detection; casing
// policy is an emitter concern. (Mirrors the OpenAPI compiler's helper — the two
// compilers may not import each other, so the neutral-naming rule is duplicated
// rather than shared.)
func canonicalWords(name string) string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		if r == '_' || r == '-' || r == ' ' {
			flush()
			continue
		}
		if len(cur) > 0 && wordBoundary(cur[len(cur)-1], r, runes, i) {
			flush()
		}
		cur = append(cur, r)
	}
	flush()
	return strings.Join(words, "_")
}

// wordBoundary reports whether a new word starts at runes[i] given the previous
// accumulated rune prev.
func wordBoundary(prev, r rune, runes []rune, i int) bool {
	switch {
	case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
		return true // lower/digit -> Upper: "firstName" -> first|Name
	case unicode.IsUpper(prev) && unicode.IsUpper(r) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
		return true // acronym tail: "HTTPServer" -> HTTP|Server
	case unicode.IsLetter(prev) && unicode.IsDigit(r), unicode.IsDigit(prev) && unicode.IsLetter(r):
		return true // letter<->digit boundary
	default:
		return false
	}
}
