package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/ir"
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

// TestNew_CarriesTheLimits pins that the budgets reach the walk through the
// context, beside the other policy the caller chose.
func TestNew_CarriesTheLimits(t *testing.T) {
	t.Parallel()
	limits := lowering.Limits{MaxEnumMembers: 12}

	c := lowering.New(0, openapitest.DocDeclaring(), ir.SourceInfo{}, lowering.GroupByTags, limits, lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, overlay.Origin{})

	assert.Equal(t, limits, c.Limits)
}
