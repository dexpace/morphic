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
