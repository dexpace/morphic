// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"cmp"
	"fmt"

	"github.com/dexpace/morphic/ir"
)

// mergeConstraints folds src's constraint keywords into dst under allOf
// intersection semantics: each keyword takes the value that says what both
// sides say (constraintKeyword.meet), so a keyword only one side sets is adopted
// and a bound both set takes the tighter value.
//
// The four numeric bounds are separate keywords, so each meets on its own: a
// branch declaring only exclusiveMinimum contributes it beside a minimum from
// elsewhere, and both apply. UniqueItems has no absent state, but under
// intersection a true from either side always holds.
func mergeConstraints(dst, src *ir.Constraints) *ir.Constraints {
	if dst == nil {
		return src
	}
	if src == nil {
		return dst
	}
	for _, kw := range constraintKeywords() {
		kw.meet(dst, src)
	}
	dst.UniqueItems = dst.UniqueItems || src.UniqueItems
	return dst
}

// constraintKeyword is one constraint keyword the merge folds across
// declarations.
type constraintKeyword struct {
	// set reports whether a constraint set states the keyword.
	set func(c *ir.Constraints) bool
	// same reports whether two sets say one thing of it: neither states it, or
	// both state one value.
	same func(a, b *ir.Constraints) bool
	// meet writes to dst the value saying what dst and src say, and leaves dst's
	// where no one value does.
	meet func(dst, src *ir.Constraints)
	// differ describes two sets no one value holds, as "conflicting pattern
	// ("a" and "b")".
	differ func(a, b *ir.Constraints) (string, bool)
	// clear removes the keyword from a set.
	clear func(c *ir.Constraints)
}

// constraintKeywords lists the keywords the merge folds, in the fixed order
// constraintsConflict tries them, so the one named when several conflict is
// deterministic. A bound takes its tighter value, a step the common multiple of
// both (commonStep), and a keyword with no order keeps one value and conflicts
// with any other. UniqueItems is absent: a plain bool cannot tell false from
// unset, and under intersection a true from either side always holds.
func constraintKeywords() []constraintKeyword {
	return []constraintKeyword{
		numberKeyword("minimum", func(c *ir.Constraints) **ir.BigVal { return &c.Min }, largerNumber),
		numberKeyword("exclusiveMinimum", func(c *ir.Constraints) **ir.BigVal { return &c.ExclusiveMin }, largerNumber),
		numberKeyword("maximum", func(c *ir.Constraints) **ir.BigVal { return &c.Max }, smallerNumber),
		numberKeyword("exclusiveMaximum", func(c *ir.Constraints) **ir.BigVal { return &c.ExclusiveMax }, smallerNumber),
		numberKeyword("multipleOf", func(c *ir.Constraints) **ir.BigVal { return &c.MultipleOf }, commonStep),
		intKeyword("precision", func(c *ir.Constraints) **int64 { return &c.Precision }, onlyEqual),
		intKeyword("scale", func(c *ir.Constraints) **int64 { return &c.Scale }, onlyEqual),
		intKeyword("minLength", func(c *ir.Constraints) **int64 { return &c.MinLength }, largerCount),
		intKeyword("maxLength", func(c *ir.Constraints) **int64 { return &c.MaxLength }, smallerCount),
		intKeyword("minItems", func(c *ir.Constraints) **int64 { return &c.MinItems }, largerCount),
		intKeyword("maxItems", func(c *ir.Constraints) **int64 { return &c.MaxItems }, smallerCount),
		intKeyword("minProps", func(c *ir.Constraints) **int64 { return &c.MinProps }, largerCount),
		intKeyword("maxProps", func(c *ir.Constraints) **int64 { return &c.MaxProps }, smallerCount),
		strKeyword("pattern", func(c *ir.Constraints) *string { return &c.Pattern }),
		strKeyword("patternMessage", func(c *ir.Constraints) *string { return &c.PatternMessage }),
	}
}

// numberKeyword is the arbitrary-precision keyword name, stored at slot, which
// meet folds. Two spellings of one number are one value (sameNumber).
func numberKeyword(name string, slot func(*ir.Constraints) **ir.BigVal, meet func(x, y ir.BigVal) (ir.BigVal, bool)) constraintKeyword {
	return slotKeyword(name, slot, sameNumber, meet)
}

// intKeyword is the integer keyword name, stored at slot, which meet folds.
func intKeyword(name string, slot func(*ir.Constraints) **int64, meet func(x, y int64) (int64, bool)) constraintKeyword {
	return slotKeyword(name, slot, func(x, y int64) bool { return x == y }, meet)
}

// slotKeyword is the keyword name stored as a pointer at slot, nil where unset.
// same says whether two values are one, and meet returns the value saying what
// two say, and false where no one value does.
func slotKeyword[T any](name string, slot func(*ir.Constraints) **T, same func(x, y T) bool, meet func(x, y T) (T, bool)) constraintKeyword {
	return constraintKeyword{
		set: func(c *ir.Constraints) bool { return *slot(c) != nil },
		same: func(a, b *ir.Constraints) bool {
			x, y := *slot(a), *slot(b)
			if x == nil || y == nil {
				return x == y
			}
			return same(*x, *y)
		},
		meet: func(dst, src *ir.Constraints) {
			x, y := *slot(dst), *slot(src)
			if x == nil {
				*slot(dst) = y
				return
			}
			if y == nil {
				return
			}
			if met, ok := meet(*x, *y); ok {
				*slot(dst) = &met
			}
		},
		differ: func(a, b *ir.Constraints) (string, bool) {
			x, y := *slot(a), *slot(b)
			if x == nil || y == nil {
				return "", false
			}
			if _, ok := meet(*x, *y); ok {
				return "", false
			}
			return fmt.Sprintf("conflicting %s (%v and %v)", name, *x, *y), true
		},
		clear: func(c *ir.Constraints) { *slot(c) = nil },
	}
}

// strKeyword is the string keyword name, stored at slot, "" where unset. Two
// values of it have no order and no form saying both, so it keeps the first.
func strKeyword(name string, slot func(*ir.Constraints) *string) constraintKeyword {
	return constraintKeyword{
		set:  func(c *ir.Constraints) bool { return *slot(c) != "" },
		same: func(a, b *ir.Constraints) bool { return *slot(a) == *slot(b) },
		meet: func(dst, src *ir.Constraints) { *slot(dst) = cmp.Or(*slot(dst), *slot(src)) },
		differ: func(a, b *ir.Constraints) (string, bool) {
			x, y := *slot(a), *slot(b)
			if x == "" || y == "" || x == y {
				return "", false
			}
			return fmt.Sprintf("conflicting %s (%q and %q)", name, x, y), true
		},
		clear: func(c *ir.Constraints) { *slot(c) = "" },
	}
}

// constraintsConflict reports the first keyword of constraintKeywords that two
// constraint sets pin to values no one value holds: its index and the
// disagreement. A keyword set on one side only is no conflict, and neither is a
// bound both set, since mergeConstraints takes what holds both.
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

// largerCount returns the larger of x and y.
func largerCount(x, y int64) (int64, bool) { return max(x, y), true }

// smallerCount returns the smaller of x and y.
func smallerCount(x, y int64) (int64, bool) { return min(x, y), true }

// onlyEqual returns x where y is the same, and false otherwise: a keyword with
// no order.
func onlyEqual(x, y int64) (int64, bool) { return x, x == y }

// largerNumber returns the larger of x and y, and false where they have no
// order (compareNumbers). It keeps x's spelling of one number written twice.
func largerNumber(x, y ir.BigVal) (ir.BigVal, bool) {
	order, ok := compareNumbers(x, y)
	if order < 0 {
		return y, ok
	}
	return x, ok
}

// smallerNumber returns the smaller of x and y, and false where they have no
// order (compareNumbers). It keeps x's spelling of one number written twice.
func smallerNumber(x, y ir.BigVal) (ir.BigVal, bool) {
	order, ok := compareNumbers(x, y)
	if order > 0 {
		return y, ok
	}
	return x, ok
}

// compareNumbers orders a and b by magnitude, and returns false where either is
// a literal no Decimal holds, unless they are one text. Reading such a literal
// as zero would fold two bounds into one the source never wrote.
func compareNumbers(a, b ir.BigVal) (int, bool) {
	if a == b {
		return 0, true
	}
	da, errA := a.Decimal()
	db, errB := b.Decimal()
	if errA != nil || errB != nil {
		return 0, false
	}
	return da.Cmp(db), true
}

// sameNumber reports whether a and b denote the same value, so one magnitude
// spelled two ways, 10, 10.0 and 1e1, is one value. A pair with a literal no
// Decimal holds is judged by its text alone.
func sameNumber(a, b ir.BigVal) bool {
	order, ok := compareNumbers(a, b)
	return ok && order == 0
}
