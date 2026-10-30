// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package budget_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
)

// TestBudget_SpendsToItsLimit pins where a budget crosses: a charge to the
// limit is within it, one more is past it, and past it stays past.
func TestBudget_SpendsToItsLimit(t *testing.T) {
	t.Parallel()
	b := budget.New(budget.AtMost(3))
	assert.True(t, b.Spend(2))
	assert.True(t, b.Spend(1), "a charge that reaches the limit is within it")
	assert.False(t, b.Exhausted())
	assert.Equal(t, budget.AtMost(0), b.Remaining())

	assert.False(t, b.Spend(1), "one past the limit crosses it")
	assert.True(t, b.Exhausted())
	assert.False(t, b.Spend(0), "a crossed budget stays crossed")
	assert.Equal(t, 4, b.Spent())
	assert.Equal(t, budget.AtMost(0), b.Remaining())
	assert.Equal(t, budget.AtMost(3), b.Limit())
}

// TestBudget_RemainingIsWhatIsLeft pins Remaining under a bounded and an
// unbounded limit.
func TestBudget_RemainingIsWhatIsLeft(t *testing.T) {
	t.Parallel()
	b := budget.New(budget.AtMost(10))
	b.Spend(4)
	assert.Equal(t, budget.AtMost(6), b.Remaining())

	var free budget.Budget
	free.Spend(math.MaxInt)
	assert.Equal(t, budget.Unbounded(), free.Remaining(), "the zero Budget is unbounded")
	assert.Equal(t, budget.Unbounded(), free.Limit())
}

// TestBudget_SaturatesAtMaxInt pins that no charge wraps the count: it is held
// at math.MaxInt, which an unbounded budget and one of math.MaxInt admit.
func TestBudget_SaturatesAtMaxInt(t *testing.T) {
	t.Parallel()
	for name, limit := range map[string]budget.Limit{
		"unbounded":      budget.Unbounded(),
		"at math.MaxInt": budget.AtMost(math.MaxInt),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := budget.New(limit)
			assert.True(t, b.Spend(math.MaxInt-1))
			assert.True(t, b.Spend(math.MaxInt), "the sum is held, not wrapped negative")
			assert.Equal(t, math.MaxInt, b.Spent())
			assert.False(t, b.Exhausted())
		})
	}

	b := budget.New(budget.AtMost(math.MaxInt - 1))
	assert.False(t, b.Spend(math.MaxInt))
	assert.False(t, b.Spend(math.MaxInt), "a held count stays past its limit")
	assert.Equal(t, math.MaxInt, b.Spent())
}

// TestBudget_ANegativeChargeExhausts pins the safe direction for a charge no
// count makes, such as a product that wrapped negative.
func TestBudget_ANegativeChargeExhausts(t *testing.T) {
	t.Parallel()
	for name, limit := range map[string]budget.Limit{"bounded": budget.AtMost(10), "unbounded": budget.Unbounded()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := budget.New(limit)
			b.Spend(3)
			assert.False(t, b.Spend(-1))
			assert.True(t, b.Exhausted())
			assert.Equal(t, 3, b.Spent(), "nothing is charged")
			assert.Equal(t, budget.AtMost(0), b.Remaining())
		})
	}
}

// TestBudget_ExhaustLeavesNothing pins Exhaust: past the limit by one, an
// unbounded budget stopped too, and nothing spent taken back.
func TestBudget_ExhaustLeavesNothing(t *testing.T) {
	t.Parallel()
	b := budget.New(budget.AtMost(5))
	b.Spend(2)
	b.Exhaust()
	assert.True(t, b.Exhausted())
	assert.Equal(t, 6, b.Spent(), "one past the limit")
	assert.False(t, b.Spend(0))

	over := budget.New(budget.AtMost(5))
	over.Spend(9)
	over.Exhaust()
	assert.Equal(t, 9, over.Spent(), "what was spent stays spent")

	var free budget.Budget
	free.Exhaust()
	assert.True(t, free.Exhausted(), "an unbounded budget can be stopped")
	assert.Equal(t, math.MaxInt, free.Spent())
	assert.False(t, free.Spend(1))
}

// TestBudget_ACopyCountsOnItsOwn pins a Budget as a value: a charge on a copy
// leaves the original as it was.
func TestBudget_ACopyCountsOnItsOwn(t *testing.T) {
	t.Parallel()
	b := budget.New(budget.AtMost(3))
	b.Spend(2)
	trial := b
	assert.False(t, trial.Spend(2))
	assert.False(t, b.Exhausted())
	assert.Equal(t, 2, b.Spent())
}
