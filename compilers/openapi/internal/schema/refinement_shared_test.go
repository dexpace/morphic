// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestRefinementUndecided_ValueSetsAreTypedByTheMembersTheyShare holds the
// one reading of value sets of an allOf electing no entry: an instance meets
// every set only as a member of each, so the position is the kinds of the
// members they share that every other conjunct's kinds leave, the top type
// with the warning that they share no value where none is left, in either
// order of the entries.
func TestRefinementUndecided_ValueSetsAreTypedByTheMembersTheyShare(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		entries []string
		want    ir.TypeID
		warns   int
	}{
		"no shared member": {[]string{"{enum: [a, b]}", "{enum: [c, d]}"}, ir.PrimTypeID(ir.PrimAny), 1},
		"a shared flag":    {[]string{"{enum: [true, a]}", "{enum: [true, b]}"}, ir.PrimTypeID(ir.PrimBool), 0},
		"a kind beside leaves": {[]string{"{enum: [1, true, x]}", "{enum: [1, true, y]}", "{type: [integer, string]}"},
			ir.PrimTypeID(ir.PrimInteger), 0},
		"a kind beside leaves none": {[]string{"{enum: [1, true, x]}", "{enum: [1, true, y]}", "{type: string}"},
			ir.PrimTypeID(ir.PrimAny), 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, entries := range [][]string{tc.entries, reversedEntries(tc.entries)} {
				schema := "{allOf: ["
				for i, e := range entries {
					if i > 0 {
						schema += ", "
					}
					schema += e
				}
				doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: "+schema+"]}\n"))
				openapitest.RequireNoErrorDiags(t, diags)
				scalar, ok := typeByName(doc, "S").(*ir.Scalar)
				require.True(t, ok, "%s: got %T", schema, typeByName(doc, "S"))
				require.NotNil(t, scalar.Base, schema)
				assert.Equal(t, tc.want, scalar.Base.Target, schema)
				assert.Len(t, disjointReports(diags), tc.warns, schema)
			}
		})
	}
}

// reversedEntries returns entries in the other order.
func reversedEntries(entries []string) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[len(entries)-1-i] = e
	}
	return out
}
