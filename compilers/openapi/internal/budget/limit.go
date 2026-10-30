// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package budget

import "math"

// Limit is how far a count may go: at most some number, or without bound. The
// zero Limit is unbounded, so an options field a caller leaves unset bounds
// nothing, as every internal options type the compiler projects its public
// budgets onto has it.
type Limit struct {
	most    int
	bounded bool
}

// Unbounded returns the Limit no count crosses.
func Unbounded() Limit { return Limit{} }

// AtMost returns the Limit a count crosses once it passes n. A negative n is
// taken as zero, which admits nothing: the safe direction, where the public
// spelling would read a negative budget as none at all.
func AtMost(n int) Limit { return Limit{most: max(n, 0), bounded: true} }

// FromOption returns the Limit a budget set as openapi.Limits spells one:
// value itself, def where value is zero, and unbounded where it is negative.
// It is the one place that spelling is read.
func FromOption(value, def int) Limit {
	switch {
	case value < 0:
		return Unbounded()
	case value == 0:
		return AtMost(def)
	default:
		return AtMost(value)
	}
}

// Max returns the most a count may reach under l, and false where l is
// unbounded.
func (l Limit) Max() (int, bool) { return l.most, l.bounded }

// Admits reports whether a count of n is within l.
func (l Limit) Admits(n int) bool { return !l.bounded || n <= l.most }

// Cap returns l as the bound of a loop: its most, or math.MaxInt where l is
// unbounded, which no count of what memory holds reaches.
func (l Limit) Cap() int {
	if !l.bounded {
		return math.MaxInt
	}
	return l.most
}

// Past returns the least count l does not admit, which is as far as a count
// need go to tell that it crosses l, or math.MaxInt where no int is past l.
func (l Limit) Past() int { return sum(l.Cap(), 1) }

// Plus returns the Limit on two counts taken together, one within l and one
// within o: their sum, held at math.MaxInt, or unbounded where either is.
func (l Limit) Plus(o Limit) Limit {
	if !l.bounded || !o.bounded {
		return Unbounded()
	}
	return AtMost(sum(l.most, o.most))
}

// sum returns a+b for counts a and b, neither negative, held at math.MaxInt
// rather than wrapping.
func sum(a, b int) int {
	if b > math.MaxInt-a {
		return math.MaxInt
	}
	return a + b
}
