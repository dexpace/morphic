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

// TestElection_AUnionSharingNoKindWithABodyThatIsNoModelLowersAsTheBody
// holds that a oneOf or an anyOf of objects, which shares no kind with a
// string, a string or null, an integer or an array body, lowers as the body
// keeping the union, with the one warning that the conjuncts share no value:
// beside the body's keywords, beside an allOf of it and on an entry in either
// order, at a component, in either order of the components.
func TestElection_AUnionSharingNoKindWithABodyThatIsNoModelLowersAsTheBody(t *testing.T) {
	t.Parallel()
	bodies := []struct {
		body string
		kind ir.TypeKind
	}{
		{"type: string", ir.KindScalar}, {"type: [string, 'null']", ir.KindScalar},
		{"type: integer", ir.KindScalar}, {"type: array, items: {type: string}", ir.KindList},
	}
	const union = "[{type: object, properties: {x: {type: string}}}]"
	for _, b := range bodies {
		for _, keyword := range []string{"oneOf", "anyOf"} {
			spellings := map[string]string{
				"beside the body":              "{" + b.body + ", " + keyword + ": " + union + "}",
				"beside the allOf":             "{allOf: [{" + b.body + "}], " + keyword + ": " + union + "}",
				"on an entry":                  "{allOf: [{" + b.body + "}, {" + keyword + ": " + union + "}]}",
				"on an entry, the union first": "{allOf: [{" + keyword + ": " + union + "}, {" + b.body + "}]}",
			}
			for name, schema := range spellings {
				for _, site := range scalarPairSites[:1] {
					for _, reversed := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/%s/%s/%t", b.body, keyword, name, site.name, reversed), func(t *testing.T) {
							t.Parallel()
							doc, diags := compileAnnotationSpec(t, "apart", site.doc("3.1.0", schema, reversed))
							td := kindNode(t, doc, entryPositionID(site.at))
							assert.Equal(t, b.kind, td.Kind(), "the position is the body's node")
							assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:" + keyword: ir.ReasonDegradedLowering}, keptOn(td))
							var warned []string
							for _, d := range diags {
								if d.Severity == ir.SeverityWarning {
									warned = append(warned, d.Message)
								}
							}
							require.Len(t, warned, 1)
							assert.Contains(t, warned[0], "share no value")
						})
					}
				}
			}
		}
	}
}

// TestElection_AUnionSharingAKindWithAScalarBodyLowersAsBefore pins a oneOf of
// a string beside a string body, which #2108 decides: the body's Scalar
// keeping it as validation logic beside the body's keywords, a Union of the
// branch beside an allOf of it and on an entry, keeping nothing, none warned.
func TestElection_AUnionSharingAKindWithAScalarBodyLowersAsBefore(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "shared", bothKeywordsDoc("3.1.0", "", false,
		"Flat: {type: string, oneOf: [{type: string}]}", "Beside: {allOf: [{type: string}], oneOf: [{type: string}]}",
		"Entry: {allOf: [{type: string}, {oneOf: [{type: string}]}]}"))
	requireErrorsAndWarnings(t, diags, nil)
	assert.Equal(t, map[string]ir.UnmodeledReason{"openapi:oneOf": ir.ReasonValidationOnly}, keptOn(kindNode(t, doc, componentID("Flat"))))
	assert.Equal(t, ir.KindScalar, kindNode(t, doc, componentID("Flat")).Kind())
	assert.Equal(t, ir.KindUnion, kindNode(t, doc, componentID("Beside")).Kind())
	entry := kindNode(t, doc, componentID("Entry"))
	assert.Equal(t, ir.KindUnion, entry.Kind())
	assert.Empty(t, keptOn(entry))
}

// TestElection_AnEntryTypeSharingNoKindWithItsUnionLowersAsBesideTheAllOf
// holds that a type and a oneOf sharing no kind, beside a $ref, beside an
// allOf of it and on an inline entry of that allOf, in either order of the
// components, are each a use of the $ref's target keeping both keywords, with
// the one warning that the conjuncts share no value (GitHub #2063).
func TestElection_AnEntryTypeSharingNoKindWithItsUnionLowersAsBesideTheAllOf(t *testing.T) {
	t.Parallel()
	for _, reversed := range []bool{false, true} {
		t.Run(fmt.Sprint(reversed), func(t *testing.T) {
			t.Parallel()
			doc, diags := compileAnnotationSpec(t, "entry", bothKeywordsDoc("3.1.0", "", reversed,
				"U: {oneOf: [{type: string}, {type: integer}]}",
				"Beside: {$ref: '#/components/schemas/U', type: integer, oneOf: [{type: string}, {type: boolean}]}",
				"Wrap: {allOf: [{$ref: '#/components/schemas/U'}], type: integer, oneOf: [{type: string}, {type: boolean}]}",
				"Entry: {allOf: [{$ref: '#/components/schemas/U'}, {type: integer, oneOf: [{type: string}, {type: boolean}]}]}"))
			for _, name := range []string{"Beside", "Wrap", "Entry"} {
				scalar, ok := kindNode(t, doc, componentID(name)).(*ir.Scalar)
				require.True(t, ok, "%s is a use of U", name)
				require.NotNil(t, scalar.Base, name)
				assert.Equal(t, componentID("U"), scalar.Base.Target, name)
				assert.Equal(t, map[string]ir.UnmodeledReason{
					"openapi:oneOf": ir.ReasonDegradedLowering, "openapi:type": ir.ReasonDegradedLowering,
				}, keptOn(scalar), name)
				var warned int
				for _, d := range diags {
					if d.Severity == ir.SeverityWarning && string(d.Provenance.Pointer) == "/components/schemas/"+name {
						warned++
					}
				}
				assert.Equal(t, 1, warned, name)
			}
		})
	}
}
