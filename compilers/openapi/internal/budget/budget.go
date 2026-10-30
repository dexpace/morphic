// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package budget

// Budget counts what is spent against a Limit. The zero Budget is unbounded
// and has spent nothing. It is a value: a copy counts on its own.
type Budget struct {
	limit Limit
	spent int
	// cut marks a budget exhausted whatever its count: by Exhaust, or by a
	// charge no caller can make.
	cut bool
}

// New returns a Budget of l with nothing spent.
func New(l Limit) Budget { return Budget{limit: l} }

// Spend charges n and reports whether the budget is still within its limit.
// The count is held at math.MaxInt. A negative n is no charge a count makes,
// as an overflowed product would be, so it exhausts the budget: the safe
// direction.
func (b *Budget) Spend(n int) bool {
	if n < 0 {
		b.cut = true
		return false
	}
	b.spent = sum(b.spent, n)
	return !b.Exhausted()
}

// Exhaust spends all that is left and one more, so nothing fits, an
// unbounded budget included: for a count that has found it would never end.
func (b *Budget) Exhaust() {
	b.cut = true
	b.spent = max(b.spent, b.limit.Past())
}

// Exhausted reports whether the budget is past its limit, was exhausted, or
// was charged what no count is.
func (b *Budget) Exhausted() bool { return b.cut || !b.limit.Admits(b.spent) }

// Spent returns what has been charged, held at math.MaxInt.
func (b *Budget) Spent() int { return b.spent }

// Remaining returns the Limit on what may yet be spent: what is left of a
// bounded limit, nothing once exhausted, and no bound under an unbounded one.
func (b *Budget) Remaining() Limit {
	switch {
	case b.Exhausted():
		return AtMost(0)
	case !b.limit.bounded:
		return Unbounded()
	default:
		return AtMost(b.limit.most - b.spent)
	}
}

// Limit returns the Limit the budget counts against.
func (b *Budget) Limit() Limit { return b.limit }
