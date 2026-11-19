// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"unicode/utf8"

	"github.com/dexpace/morphic/ir"
)

// admitsConstraints reports whether value meets every keyword of constraints that applies to
// a value of its kind, judged exactly. A keyword with no judge, a pattern or a
// decimal precision or scale, refuses every value it applies to, so a
// constrained node admits nothing it cannot be shown to fit. A keyword for
// another kind of value, maxLength on a number, constrains nothing.
func admitsConstraints(constraints *ir.Constraints, value ir.Value, depth int) bool {
	if constraints == nil {
		return true
	}
	switch value.Kind {
	case ir.ValueNumber:
		return admitsNumber(constraints, value.Number)
	case ir.ValueString:
		return constraints.Pattern == "" && within(int64(utf8.RuneCountInString(value.String)), constraints.MinLength, constraints.MaxLength)
	case ir.ValueBytes:
		return within(int64(len(value.Bytes)), constraints.MinLength, constraints.MaxLength)
	case ir.ValueList:
		return within(int64(len(value.List)), constraints.MinItems, constraints.MaxItems) && (!constraints.UniqueItems || distinct(value.List, depth))
	case ir.ValueObject:
		return within(int64(len(value.Object)), constraints.MinProperties, constraints.MaxProperties)
	case ir.ValueNull, ir.ValueBool, ir.ValueSymbol, ir.ValueRefKind, ir.ValueConstructor,
		ir.ValueInfinity, ir.ValueNegativeInfinity, ir.ValueNaN:
		// No keyword constrains a null or a bool, and the kind judge refuses the
		// rest before any keyword is read; no OpenAPI value is non-finite.
		return true
	default:
		return false
	}
}

// admitsNumber reports whether bigVal meets constraints's numeric keywords: each bound, and
// multipleOf where isMultipleOf can say. A number or a bound that reads as no
// number admits nothing. Precision and scale have no judge here.
func admitsNumber(constraints *ir.Constraints, bigVal ir.BigVal) bool {
	if constraints.Precision != nil || constraints.Scale != nil {
		return false
	}
	decimal, err := bigVal.Decimal()
	if err != nil {
		return false
	}
	bounds := []struct {
		bound *ir.BigVal
		holds func(order int) bool
	}{
		{constraints.Min, func(order int) bool { return order >= 0 }},
		{constraints.ExclusiveMin, func(order int) bool { return order > 0 }},
		{constraints.Max, func(order int) bool { return order <= 0 }},
		{constraints.ExclusiveMax, func(order int) bool { return order < 0 }},
	}
	for _, b := range bounds {
		if b.bound == nil {
			continue
		}
		bound, err := b.bound.Decimal()
		if err != nil || !b.holds(decimal.Cmp(bound)) {
			return false
		}
	}
	if constraints.MultipleOf == nil {
		return true
	}
	step, err := constraints.MultipleOf.Decimal()
	if err != nil {
		return false
	}
	multiple, ok := isMultipleOf(decimal, step)
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
	for _, value := range vs {
		key, ok := canonicalValue(value, depth+1)
		if !ok || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}
