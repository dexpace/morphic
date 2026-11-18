// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
