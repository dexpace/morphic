// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// scalarPairSites are a component and a property, each in a document writing
// the component Kstr, a string, beside the others the site writes.
var scalarPairSites = []bothKeywordsSite{
	{"component", "/components/schemas/P", func(version, schema string, reversed bool) string {
		return bothKeywordsDoc(version, "", reversed, "Kstr: {type: string}", "P: "+schema)
	}},
	{"property", "/components/schemas/C/properties/p", func(version, schema string, reversed bool) string {
		return bothKeywordsDoc(version, "", reversed, "Kstr: {type: string}", "C: {type: object, properties: {p: "+schema+"}}")
	}},
}

// stringOrNullBodies are a string or null as each version spells it.
var stringOrNullBodies = map[string]string{
	"3.0.3": "{type: string, nullable: true}",
	"3.1.0": "{type: [string, 'null']}",
	"3.2.0": "{type: [string, 'null']}",
}

// TestElection_TwoUnionsOfNullAloneBesideABodyKeepTheirReasonEverywhere holds
// that a oneOf and an anyOf each of null alone, beside a string or null, lower
// alike beside the allOf, on one entry, on two in either order and one allOf
// deeper, in either order of the components: null alone, keeping both unions
// as degraded_lowering, as beside the allOf (GitHub #2011).
func TestElection_TwoUnionsOfNullAloneBesideABodyKeepTheirReasonEverywhere(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, site := range scalarPairSites {
			t.Run(version+"/"+site.name, func(t *testing.T) {
				t.Parallel()
				null := "[{type: 'null'}]"
				if version == "3.0.3" {
					null = "[{type: object, enum: [null], nullable: true}]"
				}
				doc, _ := compileSpellingsAlike(t, site, version, unionPairSpellings(stringOrNullBodies[version], null, null))
				assert.Equal(t, map[string]ir.UnmodeledReason{
					"openapi:oneOf": ir.ReasonDegradedLowering, "openapi:anyOf": ir.ReasonDegradedLowering,
				}, keptOn(kindNode(t, doc, entryPositionID(site.at))), "the position keeps both unions")
			})
		}
	}
}

// TestElection_APrincipalUnionBesideAPairAndABodyCollapsesAsBesideTheAllOf
// holds that an anyOf of null and a $ref to a string, elected over a oneOf
// declaring no shape, beside a string or null, lowers alike beside the allOf,
// on one entry, on two in either order and one allOf deeper, in either order of
// the branches and the components: a Union of the one string, as beside the
// allOf, not collapsed to a reference on two entries, keeping the oneOf as
// validation_only (GitHub #2011).
func TestElection_APrincipalUnionBesideAPairAndABodyCollapsesAsBesideTheAllOf(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, site := range scalarPairSites {
			for _, reversed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", version, site.name, reversed), func(t *testing.T) {
					t.Parallel()
					anyOf := "[{type: 'null'}, {$ref: '#/components/schemas/Kstr'}]"
					if version == "3.0.3" {
						anyOf = "[{type: object, enum: [null], nullable: true}, {$ref: '#/components/schemas/Kstr'}]"
					}
					if reversed {
						anyOf = reversedBranches(t, anyOf)
					}
					doc, _ := compileSpellingsAlike(t, site, version,
						unionPairSpellings(stringOrNullBodies[version], "[{required: [a]}]", anyOf))
					union, ok := kindNode(t, doc, entryPositionID(site.at)).(*ir.Union)
					require.True(t, ok, "the position is a Union")
					require.Len(t, union.Variants, 1)
					assert.Equal(t, componentID("Kstr"), union.Variants[0].Type.Target)
					assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:oneOf": ir.ReasonValidationOnly}, keptOn(union))
				})
			}
		}
	}
}

// TestElection_TwoUnionEntriesElectByCompetingKeywords holds that a oneOf of
// an object and a $ref to a string and an anyOf of that $ref alone, beside a
// string, lower alike beside the allOf, on one entry, on two in either order
// and one allOf deeper, in either order of the branches and the components:
// the oneOf is elected, both declaring shape, though the anyOf's kinds are the
// body's, and the anyOf is kept as degraded_lowering (GitHub #2011).
func TestElection_TwoUnionEntriesElectByCompetingKeywords(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		for _, site := range scalarPairSites {
			for _, reversed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", version, site.name, reversed), func(t *testing.T) {
					t.Parallel()
					one := "[{type: object, properties: {x: {type: string}}}, {$ref: '#/components/schemas/Kstr'}]"
					if reversed {
						one = reversedBranches(t, one)
					}
					doc, _ := compileSpellingsAlike(t, site, version,
						unionPairSpellings("{type: string}", one, "[{$ref: '#/components/schemas/Kstr'}]"))
					union, ok := kindNode(t, doc, entryPositionID(site.at)).(*ir.Union)
					require.True(t, ok, "the position is a Union")
					assert.True(t, union.Exclusive, "the oneOf is elected")
					assert.Len(t, union.Variants, 2)
					assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:anyOf": ir.ReasonDegradedLowering}, keptOn(union))
				})
			}
		}
	}
}

// TestElection_AnEntryWritingBothElectsAgainstThePosition holds that a oneOf
// and an anyOf beside a string or null lower alike beside the allOf, on one
// entry, on two in either order and one allOf deeper, in either order of the
// branches and the components, each union judged against all the position
// conjoins, whichever entry writes it (GitHub #2011): an anyOf with an object
// branch is elected over a oneOf restating the body, its null branch included,
// which is kept whole as validation_only, and a oneOf of null alone over an
// anyOf restating the body, the position null alone keeping both.
func TestElection_AnEntryWritingBothElectsAgainstThePosition(t *testing.T) {
	t.Parallel()
	anyOfElected := func(t *testing.T, td ir.TypeDef) {
		t.Helper()
		union, ok := td.(*ir.Union)
		require.True(t, ok, "the position is a Union")
		assert.False(t, union.Exclusive, "the anyOf is elected")
		assert.Len(t, union.Variants, 2)
		assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:oneOf": ir.ReasonValidationOnly}, keptOn(union))
	}
	cases := []struct {
		name, oneOf, anyOf string
		want               func(*testing.T, ir.TypeDef)
	}{
		{"an anyOf declaring shape", "[{type: string}]", "[{type: object, properties: {x: {type: string}}}, {type: string}]",
			anyOfElected},
		{"a oneOf with a null branch saying more", "[{type: 'null', title: n}, {type: string}]",
			"[{type: object, properties: {x: {type: string}}}, {type: string}]", anyOfElected},
		{"a oneOf of null alone", "[{type: 'null'}]", "[{type: 'null'}, {type: string}]", func(t *testing.T, td ir.TypeDef) {
			t.Helper()
			_, ok := td.(*ir.Enum)
			require.True(t, ok, "the position is null alone")
			assert.Equal(t, map[string]ir.UnmodeledReason{
				"openapi:oneOf": ir.ReasonDegradedLowering, "openapi:anyOf": ir.ReasonDegradedLowering,
			}, keptOn(td))
		}},
	}
	for _, version := range []string{"3.1.0", "3.2.0"} {
		for _, site := range scalarPairSites {
			for _, tc := range cases {
				for _, reversed := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/%t", version, site.name, tc.name, reversed), func(t *testing.T) {
						t.Parallel()
						oneOf, anyOf := tc.oneOf, tc.anyOf
						if reversed {
							oneOf, anyOf = reversedBranches(t, oneOf), reversedBranches(t, anyOf)
						}
						doc, _ := compileSpellingsAlike(t, site, version, unionPairSpellings(stringOrNullBodies[version], oneOf, anyOf))
						tc.want(t, kindNode(t, doc, entryPositionID(site.at)))
					})
				}
			}
		}
	}
}
