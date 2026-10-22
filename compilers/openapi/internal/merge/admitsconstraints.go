package merge

import (
	"unicode/utf8"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/ir"
)

// admitsConstraints reports whether v meets every keyword of c that applies to
// a value of its kind, judged exactly. A keyword with no judge, a pattern or a
// decimal precision or scale, refuses every value it applies to, so a
// constrained node admits nothing it cannot be shown to fit. A keyword for
// another kind of value, maxLength on a number, constrains nothing.
func admitsConstraints(c *ir.Constraints, v ir.Value, depth int) bool {
	if c == nil {
		return true
	}
	switch v.Kind {
	case ir.ValueNumber:
		return admitsNumber(c, v.Num)
	case ir.ValueString:
		return c.Pattern == "" && within(int64(utf8.RuneCountInString(v.Str)), c.MinLength, c.MaxLength)
	case ir.ValueBytes:
		return within(int64(len(v.Bytes)), c.MinLength, c.MaxLength)
	case ir.ValueList:
		return within(int64(len(v.List)), c.MinItems, c.MaxItems) && (!c.UniqueItems || distinct(v.List, depth))
	case ir.ValueObject:
		return within(int64(len(v.Object)), c.MinProps, c.MaxProps)
	case ir.ValueNull, ir.ValueBool, ir.ValueSymbol, ir.ValueRefKind, ir.ValueCtor:
		// No keyword constrains a null or a bool, and the kind judge refuses the
		// rest before any keyword is read.
		return true
	default:
		return false
	}
}

// admitsNumber reports whether n meets c's numeric keywords: each bound, and
// multipleOf where annotation.IsMultipleOf can say. Precision and scale have no
// judge here.
func admitsNumber(c *ir.Constraints, n ir.BigVal) bool {
	if c.Precision != nil || c.Scale != nil {
		return false
	}
	bounds := []struct {
		bound *ir.BigVal
		holds func(order int) bool
	}{
		{c.Min, func(order int) bool { return order >= 0 }},
		{c.ExclusiveMin, func(order int) bool { return order > 0 }},
		{c.Max, func(order int) bool { return order <= 0 }},
		{c.ExclusiveMax, func(order int) bool { return order < 0 }},
	}
	for _, b := range bounds {
		if b.bound == nil {
			continue
		}
		if order, ok := annotation.CompareBigVal(n, *b.bound); !ok || !b.holds(order) {
			return false
		}
	}
	if c.MultipleOf == nil {
		return true
	}
	multiple, ok := annotation.IsMultipleOf(n, *c.MultipleOf)
	return ok && multiple
}

// within reports whether n lies between the inclusive counts lo and hi, either
// of which may be absent.
func within(n int64, lo, hi *int64) bool {
	return (lo == nil || n >= *lo) && (hi == nil || n <= *hi)
}

// distinct reports whether no two of vs are one JSON value (canonicalValue). A
// value it cannot spell canonically is judged not distinct, since its
// uniqueness cannot be shown.
func distinct(vs []ir.Value, depth int) bool {
	seen := make(map[string]bool, len(vs))
	for _, v := range vs {
		key, ok := canonicalValue(v, depth+1)
		if !ok || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}
