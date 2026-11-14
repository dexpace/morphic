// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
)

// TestTargets_ListsEveryDeclaredTarget holds the vocabulary Check refuses
// against to the constants the package declares, sorted, so a target declared
// without joining it is refused by every policy naming it.
func TestTargets_ListsEveryDeclaredTarget(t *testing.T) {
	t.Parallel()
	declared := declaredTargets(t)
	slices.Sort(declared)
	assert.Equal(t, declared, lowering.Targets())
}

// TestExtensionPromotionsCheck_Refusals pins each policy Check refuses: each
// would read less than it says, in silence, were it compiled.
func TestExtensionPromotionsCheck_Refusals(t *testing.T) {
	t.Parallel()
	why := []lowering.ExtensionSource{{Key: "x-why"}}
	for _, tc := range []struct {
		name    string
		policy  lowering.ExtensionPromotions
		wantErr string
	}{
		{"a target outside the vocabulary", mapping("deprecation.mesage", why...),
			`promotions: target "deprecation.mesage" is none the compiler reads (known: deprecation, deprecation.message,`},
		{"a source with no key", mapping(lowering.TargetDeprecationMessage, lowering.ExtensionSource{}),
			`target "deprecation.message": source key "" names no extension, which starts "x-"`},
		{"a key that is no extension", mapping(lowering.TargetDeprecationMessage, lowering.ExtensionSource{Key: "why"}),
			`source key "why" names no extension`},
		{"values on a target reading text", mapping(lowering.TargetDeprecationMessage,
			lowering.ExtensionSource{Key: "x-why", Values: []string{"a"}}),
			`target "deprecation.message" reads no listed values, so source "x-why" may list none`},
		{"a prefix that is no extension's", lowering.ExtensionPromotions{Prefixes: []string{"acme-"}},
			`prefix "acme-": want "x-" followed by the start of an extension name`},
		{"the prefix of every extension", lowering.ExtensionPromotions{Prefixes: []string{"x-"}},
			`prefix "x-": want "x-" followed by`},
		{"targets beside the policy off", lowering.ExtensionPromotions{Disabled: true,
			Targets: map[lowering.ExtensionTarget][]lowering.ExtensionSource{lowering.TargetDeprecationMessage: why}},
			"promotions: disabled, so the targets and prefixes named would read nothing"},
		{"prefixes beside the policy off", lowering.ExtensionPromotions{Disabled: true, Prefixes: []string{"x-a"}},
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
	listed := map[lowering.ExtensionTarget][]lowering.ExtensionSource{lowering.TargetStability: nil}
	for _, target := range lowering.Targets() {
		if lowering.ListsValues(target) {
			listed[target] = []lowering.ExtensionSource{{Key: "x-k", Values: []string{"v"}}}
		}
	}
	for name, policy := range map[string]lowering.ExtensionPromotions{
		"the default":   {},
		"the default's": {Targets: lowering.DefaultExtensionPromotions(), Prefixes: []string{"x-a", "x-a"}},
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
	var listing []lowering.ExtensionTarget
	for _, target := range lowering.Targets() {
		if lowering.ListsValues(target) {
			listing = append(listing, target)
		}
	}
	assert.Equal(t, []lowering.ExtensionTarget{
		lowering.TargetDeprecation, lowering.TargetEnumOpen,
		lowering.TargetPaginationCursor, lowering.TargetPaginationHasMore, lowering.TargetPaginationItems,
		lowering.TargetPaginationLimit,
		lowering.TargetPaginationNextCursor, lowering.TargetPaginationNextLink, lowering.TargetPaginationOffset,
		lowering.TargetPaginationPage, lowering.TargetPaginationTotalCount, lowering.TargetPaginationTotalPages,
		lowering.TargetAllowReserved, lowering.TargetRedaction, lowering.TargetUnionOpen,
	}, listing)
}
