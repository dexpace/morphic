// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// nullListSites are a component and a property, each document declaring a
// schema of null alone and TN, an object or null.
var nullListSites = []bothKeywordsSite{
	{"component", "/components/schemas/P", func(version, schema string, reversed bool) string {
		return bothKeywordsDoc(version, "", reversed, "Null: {type: 'null'}",
			"TN: {type: [object, 'null'], properties: {a: {type: string}}}", "P: "+schema)
	}},
	{"property", "/components/schemas/C/properties/p", func(version, schema string, reversed bool) string {
		return bothKeywordsDoc(version, "", reversed, "Null: {type: 'null'}",
			"TN: {type: [object, 'null'], properties: {a: {type: string}}}", "C: {type: object, properties: {p: "+schema+"}}")
	}},
}

// TestElection_ANullBranchIsReadAgainstTheUnionPassedOver holds that a branch
// of null alone of the union a position elects is read against the union it
// passes over too, wherever the two are written: beside a body admitting an
// object or null, a oneOf admitting objects alone leaves the elected anyOf's
// branch naming null no value, so every spelling, the reference's own
// keywords beside the two included, is a Union of the anyOf's object branch
// keeping the null one, with one warning naming it (GitHub #2011).
func TestElection_ANullBranchIsReadAgainstTheUnionPassedOver(t *testing.T) {
	t.Parallel()
	const inline = "{type: [object, 'null'], properties: {a: {type: string}}}"
	for _, version := range []string{"3.1.0", "3.2.0"} {
		for _, site := range nullListSites {
			for _, body := range []string{inline, "{$ref: '#/components/schemas/TN'}"} {
				for _, reversed := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%t/%t", version, site.name, body == inline, reversed), func(t *testing.T) {
						t.Parallel()
						one, anyOf := "[{type: object}]", "[{type: object, properties: {x: {type: string}}}, "+nullRef+"]"
						null := 1
						if reversed {
							anyOf, null = reversedBranches(t, anyOf), 0
						}
						spellings := unionPairSpellings(body, one, anyOf)
						if body != inline {
							// An inline list keeps its type on each variant where an
							// entry writes it, as beside one union.
							spellings["beside the body"] = strings.TrimSuffix(body, "}") + ", oneOf: " + one + ", anyOf: " + anyOf + "}"
						}
						doc, diags := compileSpellingsAlike(t, site, version, spellings)
						requireErrorsAndWarnings(t, diags, []wantFinding{{code: diag.DegradedConstruct, pointer: string(site.at),
							contains: []string{"so no instance satisfies it"}}})
						union, ok := kindNode(t, doc, entryPositionID(site.at)).(*ir.Union)
						require.True(t, ok, "the anyOf distributes")
						assert.Len(t, union.Variants, 1, "the null branch is no variant")
						assert.Equal(t, ir.ReasonDegradedLowering, keptOn(union)[fmt.Sprintf("openapi:anyOf/%d", null)],
							"the Union keeps the null branch")
					})
				}
			}
		}
	}
}
