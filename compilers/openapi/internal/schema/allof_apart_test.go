// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestAllOf_ASplitEntrysReferenceIsNamedWhateverIsBesideIt pins the name of
// the reference an entry writes beside other keys in the report of value sets
// sharing no member: its path and `$ref`, where the keys beside it hold an
// allOf whose entry the statement lists between the two, as where they hold
// none (docs/compilers/openapi.md#allof-disjoint-conjuncts).
func TestAllOf_ASplitEntrysReferenceIsNamedWhateverIsBesideIt(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ entries, names string }{
		"the keywords beside it follow": {"{$ref: '#/components/schemas/A', enum: [b]}, {enum: [c]}",
			"allOf/0/$ref: 1 value, allOf/0: 1 value, allOf/1: 1 value"},
		"an allOf beside it lies between": {"{$ref: '#/components/schemas/A', enum: [b], allOf: [{enum: [c]}]}",
			"allOf/0/$ref: 1 value, allOf/0/allOf/0: 1 value, allOf/0: 1 value"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    A: {enum: [a]}\n    S: {allOf: ["+tc.entries+"]}\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			warnings := disjointReports(diags)
			require.Len(t, warnings, 1, "%+v", diags)
			assert.Equal(t, jsontext.Pointer("/components/schemas/S"), warnings[0].Provenance.Pointer)
			assert.Contains(t, warnings[0].Message, "share no value ("+tc.names+")")
		})
	}
}

// TestAllOf_ValueSetsSharingNoMemberWarnOnce holds the one reading of conjuncts
// sharing no value, the election's: value sets sharing no member, a format
// the election reads as excluding a member, and an enum beside the allOf each
// draw one warning naming every set by its member count, as a contradiction
// in the kinds does. None is a Model unless its kinds are an object alone: the
// keywords beside the allOf decide, the top type where they name no kind, and
// a value set elected before the rest stays elected.
func TestAllOf_ValueSetsSharingNoMemberWarnOnce(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		schema, names, shape string
		kind                 ir.TypeKind
	}{
		"two enums": {"{allOf: [{enum: [a, b]}, {enum: [c, d]}]}",
			"allOf/0: 2 values, allOf/1: 2 values", "the top type it lowered as", ir.KindScalar},
		"two consts": {"{allOf: [{const: 1}, {const: 2}]}",
			"allOf/0: 1 value, allOf/1: 1 value", "the top type it lowered as", ir.KindScalar},
		"an enum beside": {"{enum: [a, b], allOf: [{enum: [c, d]}]}",
			"allOf/0: 2 values, enum: 2 values", "the enum it lowered as", ir.KindEnum},
		"a format excluding the member": {"{allOf: [{type: number, format: int32}, {type: number, enum: [1.5]}]}",
			"allOf/1: 1 value, allOf/0: integer of format int32", "the enum it lowered as", ir.KindEnum},
		"two typed sets": {"{allOf: [{type: string, enum: [a]}, {type: string, enum: [b]}]}",
			"allOf/0: 1 value, allOf/1: 1 value", "the top type it lowered as", ir.KindScalar},
		"a type beside": {"{type: string, allOf: [{enum: [a]}, {enum: [b]}]}",
			"allOf/0: 1 value, allOf/1: 1 value, type: string", "the string it lowered as", ir.KindScalar},
		"two object sets": {"{allOf: [{enum: [{a: 1}]}, {enum: [{b: 2}]}]}",
			"allOf/0: 1 value, allOf/1: 1 value", "a composition", ir.KindModel},
		"a format beside": {"{allOf: [{format: uuid}, {enum: [a]}, {enum: [b]}]}",
			"allOf/1: 1 value, allOf/2: 1 value, allOf/0: format uuid", "the top type it lowered as", ir.KindScalar},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: "+tc.schema+"\n"))
			openapitest.RequireNoErrorDiags(t, diags)
			warnings := disjointReports(diags)
			require.Len(t, warnings, 1, "%+v", diags)
			assert.Equal(t, jsontext.Pointer("/components/schemas/S"), warnings[0].Provenance.Pointer)
			assert.Contains(t, warnings[0].Message, "share no value ("+tc.names+")")
			assert.Contains(t, warnings[0].Message, "which "+tc.shape+" cannot state")
			assert.Len(t, warningsAt(diags), 1, "no other warning: %+v", diags)
			s := typeByName(doc, "S")
			require.NotNil(t, s)
			assert.Equal(t, tc.kind, s.Kind())
			if scalar, ok := s.(*ir.Scalar); ok && tc.shape == "the top type it lowered as" {
				require.NotNil(t, scalar.Base)
				assert.Equal(t, ir.PrimTypeID(ir.PrimAny), scalar.Base.Target, "no kind is claimed")
				for i := range strings.Count(tc.schema, "}, {") + 1 {
					entry := fmt.Sprintf("/components/schemas/S/allOf/%d", i)
					assert.True(t, slices.ContainsFunc(slices.Collect(maps.Values(scalar.Unmodeled)), func(e ir.UnmodeledEntry) bool {
						return strings.HasPrefix(string(e.Provenance.Pointer), entry)
					}), "the entry at %s is kept", entry)
				}
			}
		})
	}
}

// TestAllOf_ValueSetsSharingAValueDrawNoWarning holds the reading to what it
// says: sets sharing a member, a null among them, share a value, so nothing
// warns, and a set within the other is elected as before.
func TestAllOf_ValueSetsSharingAValueDrawNoWarning(t *testing.T) {
	t.Parallel()
	for name, schema := range map[string]string{
		"a shared member": "{allOf: [{enum: [a, b]}, {enum: [b, c]}]}",
		"a shared null":   "{allOf: [{enum: [a, null]}, {enum: [b, null]}]}",
		"a set within":    "{allOf: [{enum: [a]}, {enum: [a, b]}]}",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: "+schema+"\n"))
			assert.Empty(t, disjointReports(diags), "%+v", diags)
		})
	}
}

// TestAllOf_ValueSetsSharingNoMemberReportAlikeInEitherOrder holds the report
// and the lowering to one document whichever order the sets are written in:
// the report lists the sets by what each states, so swapping the entries
// swaps their names and nothing else.
func TestAllOf_ValueSetsSharingNoMemberReportAlikeInEitherOrder(t *testing.T) {
	t.Parallel()
	one, oneDiags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: {allOf: [{enum: [a, b]}, {enum: [c]}]}\n"))
	two, twoDiags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    S: {allOf: [{enum: [c]}, {enum: [a, b]}]}\n"))
	swap := strings.NewReplacer("allOf/0", "allOf/1", "allOf/1", "allOf/0")
	require.Len(t, disjointReports(oneDiags), 1)
	require.Len(t, disjointReports(twoDiags), 1)
	assert.Equal(t, disjointReports(oneDiags)[0].Message, swap.Replace(disjointReports(twoDiags)[0].Message))
	assert.Equal(t, typeByName(one, "S").Kind(), typeByName(two, "S").Kind())
}

// warningsAt returns the warnings among diags.
func warningsAt(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning {
			out = append(out, d)
		}
	}
	return out
}

// TestAllOf_ARefAmongValueSetsSharingNoMemberIsKept holds that a $ref beside
// value sets sharing no member is no principal, as none of the conjuncts is:
// the position is the top type, the $ref kept with an info saying why.
func TestAllOf_ARefAmongValueSetsSharingNoMemberIsKept(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.1.0", "    E: {enum: [a, b]}\n"+
		"    S: {allOf: [{$ref: '#/components/schemas/E'}, {enum: [c]}, {enum: [d]}]}\n"))
	require.Len(t, disjointReports(diags), 1, "%+v", diags)
	scalar, ok := typeByName(doc, "S").(*ir.Scalar)
	require.True(t, ok, "%T", typeByName(doc, "S"))
	require.NotNil(t, scalar.Base)
	assert.Equal(t, ir.PrimTypeID(ir.PrimAny), scalar.Base.Target)
	assert.Contains(t, scalar.Unmodeled, "openapi:$ref")
	var why []string
	for _, d := range diags {
		if d.Provenance.Pointer == "/components/schemas/S/allOf/0" {
			why = append(why, d.Message)
		}
	}
	require.Len(t, why, 1)
	assert.Contains(t, why[0], "the value sets of this allOf share no member, so no conjunct is the one it lowers from")
}
