package irverify

import (
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStringLists_AreAllClassified fails when the ir package declares a
// []string field stringLists records no decision for, or when stringLists still
// classifies one ir no longer declares. An unclassified field is held to
// listPath, so a list whose blank entry is legal would report one as a defect.
func TestStringLists_AreAllClassified(t *testing.T) {
	t.Parallel()
	stringList := types.NewSlice(types.Typ[types.String])
	var found []string
	for _, f := range declaredFields(t) {
		if types.Identical(f.field.Type(), stringList) {
			found = append(found, f.key())
		}
	}
	require.NotEmpty(t, found, "the ir package must declare []string fields")
	for _, name := range found {
		assert.Contains(t, stringLists, name,
			"ir.%s is a []string field stringLists does not classify: "+
				"say what its entries are held to", name)
	}
	assert.Len(t, stringLists, len(found),
		"stringLists classifies a field the ir package no longer declares")
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
