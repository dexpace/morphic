// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestRoutesOf_AddingToATagLeavesTheOthers holds each tag of a Mapping's
// routes to a slice capped at its one entry, whatever order the map ranges in,
// and adding a type to a tag to leave every other tag its own. The routes
// share one array, so a slice reaching past its entry is one that adding to
// would write over the next.
func TestRoutesOf_AddingToATagLeavesTheOthers(t *testing.T) {
	t.Parallel()
	mapping := map[string]ir.TypeID{"cat": "type/cat", "dog": "type/dog", "eel": "type/eel"}
	for tag := range mapping {
		routes := routesOf(mapping)
		for each, targets := range routes {
			assert.Equal(t, 1, cap(targets), "the routes of %s", each)
		}

		routes.add(tag, "type/added")

		want := map[string][]ir.TypeID{"cat": {"type/cat"}, "dog": {"type/dog"}, "eel": {"type/eel"}}
		want[tag] = append(want[tag], "type/added")
		assert.Empty(t, cmp.Diff(want, map[string][]ir.TypeID(routes)), "adding to %s", tag)
	}
}
