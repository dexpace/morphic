// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package promotion_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
)

// TestTargets_ListsEveryDeclaredTarget holds the vocabulary Check refuses
// against to the constants the package declares, sorted, so a target declared
// without joining it is refused by every policy naming it.
func TestTargets_ListsEveryDeclaredTarget(t *testing.T) {
	t.Parallel()
	declared := declaredTargets(t)
	slices.Sort(declared)
	assert.Equal(t, declared, promotion.Targets())
}

// TestExtensionPromotionsCheck_Refusals pins each policy Check refuses: each
// would read less than it says, in silence, were it compiled.
func TestExtensionPromotionsCheck_Refusals(t *testing.T) {
	t.Parallel()
	why := []promotion.ExtensionSource{{Key: "x-why"}}
	for _, tc := range []struct {
		name    string
		policy  promotion.ExtensionPromotions
		wantErr string
	}{
		{"a target outside the vocabulary", mapping("deprecation.mesage", why...),
			`promotions: target "deprecation.mesage" is none the compiler reads (known: deprecation, deprecation.message,`},
		{"a source with no key", mapping(promotion.TargetDeprecationMessage, promotion.ExtensionSource{}),
			`target "deprecation.message": source key "" names no extension, which starts "x-"`},
		{"a key that is no extension", mapping(promotion.TargetDeprecationMessage, promotion.ExtensionSource{Key: "why"}),
			`source key "why" names no extension`},
		{"values on a target reading text", mapping(promotion.TargetDeprecationMessage,
			promotion.ExtensionSource{Key: "x-why", Values: []string{"a"}}),
			`target "deprecation.message" reads no listed values, so source "x-why" may list none`},
		{"a prefix that is no extension's", promotion.ExtensionPromotions{Prefixes: []string{"acme-"}},
			`prefix "acme-": want "x-" followed by the start of an extension name`},
		{"the prefix of every extension", promotion.ExtensionPromotions{Prefixes: []string{"x-"}},
			`prefix "x-": want "x-" followed by`},
		{"targets beside the policy off", promotion.ExtensionPromotions{Disabled: true,
			Targets: map[promotion.ExtensionTarget][]promotion.ExtensionSource{promotion.TargetDeprecationMessage: why}},
			"promotions: disabled, so the targets and prefixes named would read nothing"},
		{"prefixes beside the policy off", promotion.ExtensionPromotions{Disabled: true, Prefixes: []string{"x-a"}},
			"promotions: disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.policy.Check()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestExtensionPromotionsCheck_AcceptsWhatReadsAsWritten pins the policies Check
// lets through: the default, the policy off, a mapping listing values for each
// target that reads them, and an empty list, which maps the target to nothing
// as leaving it out does.
func TestExtensionPromotionsCheck_AcceptsWhatReadsAsWritten(t *testing.T) {
	t.Parallel()
	listed := map[promotion.ExtensionTarget][]promotion.ExtensionSource{promotion.TargetStability: nil}
	for _, target := range promotion.Targets() {
		if promotion.ListsValues(target) {
			listed[target] = []promotion.ExtensionSource{{Key: "x-k", Values: []string{"v"}}}
		}
	}
	for name, policy := range map[string]promotion.ExtensionPromotions{
		"the default":   {},
		"the default's": {Targets: promotion.DefaultExtensionPromotions(), Prefixes: []string{"x-a", "x-a"}},
		"off":           {Disabled: true},
		"values listed": {Targets: listed},
	} {
		assert.NoError(t, policy.Check(), name)
	}
}

// TestListsValues_NamesTheTargetsStatingAFact pins which targets a source may
// list values for: those whose value says that something is so, and none
// reading a text, a name or a tier.
func TestListsValues_NamesTheTargetsStatingAFact(t *testing.T) {
	t.Parallel()
	var listing []promotion.ExtensionTarget
	for _, target := range promotion.Targets() {
		if promotion.ListsValues(target) {
			listing = append(listing, target)
		}
	}
	assert.Equal(t, []promotion.ExtensionTarget{
		promotion.TargetDeprecation, promotion.TargetEnumOpen,
		promotion.TargetPaginationCursor, promotion.TargetPaginationHasMore, promotion.TargetPaginationItems,
		promotion.TargetPaginationLimit,
		promotion.TargetPaginationNextCursor, promotion.TargetPaginationNextLink, promotion.TargetPaginationOffset,
		promotion.TargetPaginationPage, promotion.TargetPaginationTotalCount, promotion.TargetPaginationTotalPages,
		promotion.TargetAllowReserved, promotion.TargetRedaction, promotion.TargetUnionOpen,
	}, listing)
}
