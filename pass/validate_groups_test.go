// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestValidate_GroupWalkTruncationIsReported pins the depth bound itself: a walk
// that stopped early must say so. Without it every check reaching operations
// walks a subset while Validate still reports the document consistent.
func TestValidate_GroupWalkTruncationIsReported(t *testing.T) {
	t.Parallel()
	oneway := ir.Operation{ID: "deep", OneWay: true, Responses: []ir.Response{{}}}

	// Pinned at the boundary, not near it. Every leaf recurses once into its own
	// empty Groups, so a cap reported without checking whether anything was
	// actually skipped fires at exactly the depth where the last operation still
	// fits — visible only by testing ir.MaxGroupDepth itself.
	for _, depth := range []int{1, 127, 128} {
		within := irtest.DiagCodes(validated(t, nestGroups(depth, oneway)))
		assert.Contains(t, within, "pass/oneway-with-responses",
			"at depth %d the operation is still reached", depth)
		assert.NotContains(t, within, "ir/walk-truncated",
			"at depth %d nothing was skipped, so nothing may claim truncation", depth)
	}

	for _, depth := range []int{129, 200} {
		past := irtest.DiagCodes(validated(t, nestGroups(depth, oneway)))
		assert.Contains(t, past, "ir/walk-truncated",
			"at depth %d the walk stopped early and must say so", depth)
		assert.NotContains(t, past, "pass/oneway-with-responses",
			"and the operation past the cap genuinely went unvisited")
	}
}

// TestValidate_ASelfContainingGroupIsReachedOnceAndIsNotATruncation holds
// Validate to what a group that is its own subgroup costs an unguarded walk. It
// follows the cycle to the cap, so the operation's defect is reported once for
// every lap and the document is called too deeply nested, though nothing was
// skipped.
func TestValidate_ASelfContainingGroupIsReachedOnceAndIsNotATruncation(t *testing.T) {
	t.Parallel()
	groups := []ir.OperationGroup{{Operations: []ir.Operation{
		{ID: "loop", OneWay: true, Responses: []ir.Response{{}}},
	}}}
	groups[0].Groups = groups
	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s", Groups: groups}}

	diags := validated(t, doc)
	assert.NotContains(t, irtest.DiagCodes(diags), "ir/walk-truncated", "nothing was skipped")
	assert.Equal(t, 1, countCode(t, diags, "pass/oneway-with-responses"),
		"the operation is reached once, not once per lap")
}
