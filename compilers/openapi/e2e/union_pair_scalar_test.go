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
