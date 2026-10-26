package budget_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
)

// TestLimit_ZeroIsUnbounded pins the zero Limit as the one spelling of no
// bound: an options field left unset bounds nothing.
func TestLimit_ZeroIsUnbounded(t *testing.T) {
	t.Parallel()
	assert.Equal(t, budget.Unbounded(), budget.Limit{})
	most, bounded := budget.Limit{}.Max()
	assert.False(t, bounded)
	assert.Zero(t, most)
	assert.True(t, budget.Limit{}.Admits(math.MaxInt))
}

// TestFromOption_ReadsThePublicSpelling pins the convention openapi.Limits
// states: zero takes the default and a negative value is unbounded.
func TestFromOption_ReadsThePublicSpelling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value int
		want  budget.Limit
	}{
		{"zero takes the default", 0, budget.AtMost(9)},
		{"a set value is kept", 4, budget.AtMost(4)},
		{"minus one is unbounded", -1, budget.Unbounded()},
		{"any negative is unbounded", math.MinInt, budget.Unbounded()},
		{"the largest value is kept", math.MaxInt, budget.AtMost(math.MaxInt)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, budget.FromOption(tc.value, 9))
		})
	}
}

// TestAtMost_ANegativeBoundAdmitsNothing pins the safe direction: a negative
// bound is no request for none, as it is in the public spelling.
func TestAtMost_ANegativeBoundAdmitsNothing(t *testing.T) {
	t.Parallel()
	assert.Equal(t, budget.AtMost(0), budget.AtMost(-1))
	assert.True(t, budget.AtMost(-1).Admits(0))
	assert.False(t, budget.AtMost(-1).Admits(1))
}

// TestLimit_ReadsItsBound pins each reading of a bounded and an unbounded
// Limit, at the boundary and past it.
func TestLimit_ReadsItsBound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		limit     budget.Limit
		most      int
		bounded   bool
		admitted  int
		refused   int
		cap, past int
	}{
		{"a bound of three", budget.AtMost(3), 3, true, 3, 4, 3, 4},
		{"a bound of zero", budget.AtMost(0), 0, true, 0, 1, 0, 1},
		{"the largest bound", budget.AtMost(math.MaxInt), math.MaxInt, true, math.MaxInt, -1, math.MaxInt, math.MaxInt},
		{"no bound", budget.Unbounded(), 0, false, math.MaxInt, -1, math.MaxInt, math.MaxInt},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			most, bounded := tc.limit.Max()
			assert.Equal(t, tc.most, most)
			assert.Equal(t, tc.bounded, bounded)
			assert.True(t, tc.limit.Admits(tc.admitted))
			if tc.refused >= 0 {
				assert.False(t, tc.limit.Admits(tc.refused))
			}
			assert.Equal(t, tc.cap, tc.limit.Cap())
			assert.Equal(t, tc.past, tc.limit.Past(), "the count past the bound, held at math.MaxInt")
		})
	}
}

// TestLimit_PlusHoldsTheSum pins the sum of two limits: exact, held at
// math.MaxInt rather than wrapping, and unbounded where either is.
func TestLimit_PlusHoldsTheSum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		l, o budget.Limit
		want budget.Limit
	}{
		{"two bounds add", budget.AtMost(2), budget.AtMost(5), budget.AtMost(7)},
		{"a sum past math.MaxInt is held there", budget.AtMost(math.MaxInt - 1), budget.AtMost(2), budget.AtMost(math.MaxInt)},
		{"the largest bounds are held", budget.AtMost(math.MaxInt), budget.AtMost(math.MaxInt), budget.AtMost(math.MaxInt)},
		{"an unbounded left is unbounded", budget.Unbounded(), budget.AtMost(2), budget.Unbounded()},
		{"an unbounded right is unbounded", budget.AtMost(2), budget.Unbounded(), budget.Unbounded()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.l.Plus(tc.o))
		})
	}
}

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
