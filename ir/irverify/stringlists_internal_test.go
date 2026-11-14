// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"go/types"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestStringLists_AreAllClassified fails when the ir package declares a
// []string field stringLists records no decision for, or when stringLists still
// classifies one ir no longer declares. An unclassified field is held to
// listPath, so a list whose blank entry is legal would report one as a defect.
func TestStringLists_AreAllClassified(t *testing.T) {
	t.Parallel()
	found := stringListFieldKeys(t)
	require.NotEmpty(t, found, "the ir package must declare []string fields")
	for _, problem := range classificationProblems(slices.Collect(maps.Keys(stringLists)), found,
		"ir.%s is a []string field stringLists does not classify: say what its entries are held to") {
		t.Error(problem)
	}
}

// stringListFieldKeys returns the "Type.Field" key of every []string field the
// ir package declares.
func stringListFieldKeys(t *testing.T) map[string]bool {
	t.Helper()
	stringList := types.NewSlice(types.Typ[types.String])
	found := map[string]bool{}
	for _, f := range declaredFields(t) {
		if types.Identical(f.field.Type(), stringList) {
			found[f.key()] = true
		}
	}
	return found
}

// TestHeldListsOf_HoldsAnUnlistedFieldAsAPath pins what listPath being the zero
// value promises: a []string field stringLists does not list is held to it
// rather than let through, while a field of another type beside it is not.
func TestHeldListsOf_HoldsAnUnlistedFieldAsAPath(t *testing.T) {
	t.Parallel()
	type unlisted struct {
		Count    int
		Segments []string
	}
	assert.Equal(t, []heldList{{index: 1, name: "Segments", rule: listPath}},
		heldListsOf(reflect.TypeFor[unlisted]()))
}

// TestCheckStringLists_VisitingCostsNoAllocation holds the check's cost for a
// list it has nothing to report on at nothing, however long: every list is
// read into the one map the check keeps, emptied for each, where a map of its
// own allocates once a list holds more than eight entries. A path and a set,
// on two nodes, share entries, which only a map left unemptied would report.
// It is not run in parallel: AllocsPerRun measures the process.
func TestCheckStringLists_VisitingCostsNoAllocation(t *testing.T) {
	entries := make([]string, 0, 12)
	for i := range 12 {
		entries = append(entries, "entry"+strconv.Itoa(i))
	}
	doc := &ir.Document{
		Types: ir.TypeRegistry{"t/x/M": &ir.Model{ID: "t/x/M", Namespace: entries}},
		Auth: map[ir.AuthID]ir.AuthScheme{"auth/x/S": {ID: "auth/x/S", Flows: []ir.OAuthFlow{
			{TokenEndpointAuthMethods: entries},
		}}},
	}
	check := checkStringLists(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, check.report(declarations{}), "no entry is blank, and no set entry repeats within its list")
}
