package merge

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/ir"
)

// canonicalValue spells v so that two JSON-equal values spell alike: a number
// by its value (annotation.CanonicalDecimal), an object by its members in name
// order. ok is false for a kind JSON has no instance of, a number outside the
// decimal grammar, or nesting past maxJudgeDepth.
func canonicalValue(v ir.Value, depth int) (string, bool) {
	if depth > maxJudgeDepth {
		return "", false
	}
	switch v.Kind {
	case ir.ValueNull:
		return "null", true
	case ir.ValueBool:
		return strconv.FormatBool(v.Bool), true
	case ir.ValueString:
		return strconv.Quote(v.Str), true
	case ir.ValueNumber:
		return annotation.CanonicalDecimal(v.Num)
	case ir.ValueList:
		return canonicalMembers("[", "]", len(v.List), func(i int) (string, bool) {
			return canonicalValue(v.List[i], depth+1)
		})
	case ir.ValueObject:
		fields := slices.SortedFunc(slices.Values(v.Object), func(a, b ir.Field) int { return strings.Compare(a.Name, b.Name) })
		return canonicalMembers("{", "}", len(fields), func(i int) (string, bool) {
			member, ok := canonicalValue(fields[i].Value, depth+1)
			return strconv.Quote(fields[i].Name) + ":" + member, ok
		})
	case ir.ValueBytes, ir.ValueSymbol, ir.ValueRefKind, ir.ValueCtor:
		return "", false
	default:
		return "", false
	}
}

// canonicalMembers joins the n spellings member gives between open and close,
// or fails with the first member that cannot be spelled.
func canonicalMembers(open, closing string, n int, member func(i int) (string, bool)) (string, bool) {
	parts := make([]string, 0, n)
	for i := range n {
		part, ok := member(i)
		if !ok {
			return "", false
		}
		parts = append(parts, part)
	}
	return open + strings.Join(parts, ",") + closing, true
}
