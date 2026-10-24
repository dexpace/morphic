package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

func TestLimitsEnumMembersExceeded_TreatsAnUnsetBudgetAsUnbounded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		limits  lowering.Limits
		members int
		want    bool
	}{
		{"no budget set", lowering.Limits{}, 1 << 20, false},
		{"a budget the caller turned off", lowering.Limits{MaxEnumMembers: -1}, 1 << 20, false},
		{"at the budget", lowering.Limits{MaxEnumMembers: 4}, 4, false},
		{"one member past the budget", lowering.Limits{MaxEnumMembers: 4}, 5, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.limits.EnumMembersExceeded(tc.members))
		})
	}
}
