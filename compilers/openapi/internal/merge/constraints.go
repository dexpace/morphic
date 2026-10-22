package merge

import (
	"cmp"
	"fmt"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/ir"
)

// mergeConstraints folds src's constraint keywords into dst under allOf
// intersection semantics: dst keeps every keyword it sets and adopts any it
// leaves unset (nil, "" or false), so a keyword only one branch constrains is
// never dropped.
//
// The four numeric bounds are separate keywords, so each is adopted on its own:
// a branch declaring only exclusiveMinimum contributes it beside a minimum from
// elsewhere. UniqueItems has no absent state, but under intersection a true
// from either branch is always correct, so cmp.Or never wrongly downgrades dst.
func mergeConstraints(dst, src *ir.Constraints) *ir.Constraints {
	if dst == nil {
		return src
	}
	if src == nil {
		return dst
	}
	dst.Min = cmp.Or(dst.Min, src.Min)
	dst.Max = cmp.Or(dst.Max, src.Max)
	dst.ExclusiveMin = cmp.Or(dst.ExclusiveMin, src.ExclusiveMin)
	dst.ExclusiveMax = cmp.Or(dst.ExclusiveMax, src.ExclusiveMax)
	dst.MultipleOf = cmp.Or(dst.MultipleOf, src.MultipleOf)
	dst.Precision = cmp.Or(dst.Precision, src.Precision)
	dst.Scale = cmp.Or(dst.Scale, src.Scale)
	dst.MinLength = cmp.Or(dst.MinLength, src.MinLength)
	dst.MaxLength = cmp.Or(dst.MaxLength, src.MaxLength)
	dst.Pattern = cmp.Or(dst.Pattern, src.Pattern)
	dst.PatternMessage = cmp.Or(dst.PatternMessage, src.PatternMessage)
	dst.MinItems = cmp.Or(dst.MinItems, src.MinItems)
	dst.MaxItems = cmp.Or(dst.MaxItems, src.MaxItems)
	dst.UniqueItems = cmp.Or(dst.UniqueItems, src.UniqueItems)
	dst.MinProps = cmp.Or(dst.MinProps, src.MinProps)
	dst.MaxProps = cmp.Or(dst.MaxProps, src.MaxProps)
	return dst
}

// constraintKeyword is one constraint keyword the merge compares across
// declarations: whether a constraint set sets it, and whether two sets that
// both do pin it to different values, described as "conflicting maxLength (10
// and 20)".
type constraintKeyword struct {
	set    func(c *ir.Constraints) bool
	differ func(a, b *ir.Constraints) (string, bool)
}

// constraintKeywords lists the keywords the merge compares, in the fixed order
// constraintsConflict tries them, so the one named when several conflict is
// deterministic. UniqueItems is absent: a plain bool cannot tell false from
// unset, and under intersection a true from either side is always right.
func constraintKeywords() []constraintKeyword {
	return []constraintKeyword{
		bigValKeyword("minimum", func(c *ir.Constraints) *ir.BigVal { return c.Min }),
		bigValKeyword("exclusiveMinimum", func(c *ir.Constraints) *ir.BigVal { return c.ExclusiveMin }),
		bigValKeyword("maximum", func(c *ir.Constraints) *ir.BigVal { return c.Max }),
		bigValKeyword("exclusiveMaximum", func(c *ir.Constraints) *ir.BigVal { return c.ExclusiveMax }),
		bigValKeyword("multipleOf", func(c *ir.Constraints) *ir.BigVal { return c.MultipleOf }),
		intKeyword("precision", func(c *ir.Constraints) *int64 { return c.Precision }),
		intKeyword("scale", func(c *ir.Constraints) *int64 { return c.Scale }),
		intKeyword("minLength", func(c *ir.Constraints) *int64 { return c.MinLength }),
		intKeyword("maxLength", func(c *ir.Constraints) *int64 { return c.MaxLength }),
		intKeyword("minItems", func(c *ir.Constraints) *int64 { return c.MinItems }),
		intKeyword("maxItems", func(c *ir.Constraints) *int64 { return c.MaxItems }),
		intKeyword("minProps", func(c *ir.Constraints) *int64 { return c.MinProps }),
		intKeyword("maxProps", func(c *ir.Constraints) *int64 { return c.MaxProps }),
		strKeyword("pattern", func(c *ir.Constraints) string { return c.Pattern }),
		strKeyword("patternMessage", func(c *ir.Constraints) string { return c.PatternMessage }),
	}
}

// bigValKeyword is the arbitrary-precision keyword name, read by of.
func bigValKeyword(name string, of func(*ir.Constraints) *ir.BigVal) constraintKeyword {
	return constraintKeyword{
		set:    func(c *ir.Constraints) bool { return of(c) != nil },
		differ: func(a, b *ir.Constraints) (string, bool) { return bigValConflictDetail(name, of(a), of(b)) },
	}
}

// intKeyword is the integer keyword name, read by of.
func intKeyword(name string, of func(*ir.Constraints) *int64) constraintKeyword {
	return constraintKeyword{
		set:    func(c *ir.Constraints) bool { return of(c) != nil },
		differ: func(a, b *ir.Constraints) (string, bool) { return intConflictDetail(name, of(a), of(b)) },
	}
}

// strKeyword is the string keyword name, read by of.
func strKeyword(name string, of func(*ir.Constraints) string) constraintKeyword {
	return constraintKeyword{
		set:    func(c *ir.Constraints) bool { return of(c) != "" },
		differ: func(a, b *ir.Constraints) (string, bool) { return strConflictDetail(name, of(a), of(b)) },
	}
}

// constraintsConflict reports the first keyword of constraintKeywords that two
// constraint sets pin to different values: its index and the disagreement. A
// keyword set on one side only is not a conflict, since mergeConstraints adopts
// it. Numeric bounds compare by magnitude, so 10 and 10.0 do not conflict.
func constraintsConflict(a, b *ir.Constraints) (keyword int, detail string, conflict bool) {
	if a == nil || b == nil {
		return 0, "", false
	}
	for i, kw := range constraintKeywords() {
		if detail, conflict := kw.differ(a, b); conflict {
			return i, detail, true
		}
	}
	return 0, "", false
}

// bigValConflictDetail reports whether both branches pin the same
// arbitrary-precision keyword to different magnitudes, formatting the
// disagreement when they do. Every arbitrary-precision keyword of
// ir.Constraints has this shape: each states its own restriction, with no
// exclusivity flag to carry beside it, so one comparison serves them all.
//
// A disagreement is usually still satisfiable (minimum 10 in one branch and 20
// in the other together mean 20), but it is diagnosed anyway: the merge keeps
// the first declaration's value over the true intersection, and the discarded
// one may be the stricter, so silence would loosen the validation the spec
// intended.
func bigValConflictDetail(keyword string, a, b *ir.BigVal) (string, bool) {
	if a == nil || b == nil || annotation.BigValEqual(*a, *b) {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%s and %s)", keyword, a.String(), b.String()), true
}

// intConflictDetail reports whether two optional integer bounds are both
// present and differ, formatting the disagreement when they do.
func intConflictDetail(keyword string, a, b *int64) (string, bool) {
	if a == nil || b == nil || *a == *b {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%d and %d)", keyword, *a, *b), true
}

// strConflictDetail reports whether two string keywords are both set and
// differ, formatting the disagreement when they do.
func strConflictDetail(keyword string, a, b string) (string, bool) {
	if a == "" || b == "" || a == b {
		return "", false
	}
	return fmt.Sprintf("conflicting %s (%q and %q)", keyword, a, b), true
}
