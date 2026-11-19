// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable/typed"
)

func TestLower_DepthCapExceeded(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("    Deep:\n")
	indent := "      "
	for range maxSchemaDepth + 4 {
		b.WriteString(indent + "type: array\n")
		b.WriteString(indent + "items:\n")
		indent += "  "
	}
	b.WriteString(indent + "type: string\n")
	doc, diags := LowerSpec(t, openapitest.ComponentSpec(b.String()))
	require.NotNil(t, doc)
	var sawCap bool
	for _, diagnostic := range diags {
		if diagnostic.Code == diag.DegradedConstruct && strings.Contains(diagnostic.Message, "nesting exceeds") {
			sawCap = true
		}
	}
	assert.True(t, sawCap, "schema depth-cap diagnostic emitted")
}

// TestLower_ASchemaPastTheMendsIsTheTopType pins what the load phase leaves
// read as false past typed.MaxMends schemas one below another, each an items
// above a draft's list of items: the innermost left is the top type, reported
// as a degraded-construct error at it, and every one above it is the array it
// writes. One schema fewer lowers whole.
func TestLower_ASchemaPastTheMendsIsTheTopType(t *testing.T) {
	t.Parallel()
	chain := func(depth int) string {
		return "jsonSchemaDialect: 'http://json-schema.org/draft-07/schema#'\n" + openapitest.ComponentSpec("    C: "+
			strings.Repeat("{type: array, items: ", depth)+"[{}]"+strings.Repeat("}", depth)+"\n")
	}
	past := "/components/schemas/C" + strings.Repeat("/items", typed.MaxMends+1)
	doc, diags := LowerSpec(t, chain(typed.MaxMends+2))
	require.NotNil(t, doc)
	assert.Equal(t, []jsontext.Pointer{jsontext.Pointer(past)}, openapitest.PointersOf(diags, diag.DegradedConstruct),
		"the innermost schema left false")
	assert.False(t, openapitest.HasDiag(diags, diag.FalseSchema), "and no false the document writes")
	for _, diagnostic := range diags {
		if diagnostic.Code == diag.DegradedConstruct {
			assert.True(t, strings.HasSuffix(diagnostic.Message, "moving the schemas below it into a component reads them"),
				"it says what the writer can do: %s", diagnostic.Message)
		}
	}
	_, diags = LowerSpec(t, chain(typed.MaxMends+1))
	assert.Empty(t, openapitest.PointersOf(diags, diag.DegradedConstruct), "one fewer is mended whole")
}

// TestDeclaresFamily_AnUnknownNameDeclaresNothing reaches the arm familyOrder
// cannot reach today, the way TestKeywordHome_AnUnknownKeywordIsHomedNowhere
// reaches its own: every name dispatchOf passes in is one of the three with a
// case here.
//
// It guards the direction the default falls. Answering an unknown name from
// another family's guard would let a familyOrder entry added without a case be
// picked on schemas that never wrote it — and a winner lowerDispatched has no arm
// for is dropped in silence (GitHub #35). Declaring nothing keeps such a name out
// of the dispatch entirely.
func TestDeclaresFamily_AnUnknownNameDeclaresNothing(t *testing.T) {
	t.Parallel()
	withAllOf := &oas3.Schema{AllOf: []*oas3.JSONSchema[oas3.Referenceable]{
		oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{}),
	}}

	require.True(t, declaresFamily(StandaloneSchema(withAllOf), "allOf"), "allOf is named and declared")
	assert.False(t, declaresFamily(StandaloneSchema(withAllOf), "oneOf"),
		"an unknown name must not inherit allOf's guard")
	assert.False(t, declaresFamily(StandaloneSchema(&oas3.Schema{}), "nosuchfamily"))
}

// TestNullableTrue_PastTheMergeBoundOnlyAListedKeyStates pins what states a
// null in a schema merging past nodeview.MergeDepthLimit, read from its keys
// alone: a `nullable` merged in past the bound states none, a truth word's or
// a boolean, nor does one a later source writes within the bound, which the
// deeper one outranks, while one the schema merges within the bound does.
func TestNullableTrue_PastTheMergeBoundOnlyAListedKeyStates(t *testing.T) {
	t.Parallel()
	past := openapitest.NestedMerges(nodeview.MergeDepthLimit+1, "{nullable: true}")
	word := openapitest.NestedMerges(nodeview.MergeDepthLimit+1, "{nullable: 'yes'}")
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    Past: {<<: "+past+", type: string}\n"+
		"    PastWord: {<<: "+word+", type: string}\n"+
		"    Outranks: {<<: ["+past+", {nullable: false}], type: string}\n"+
		"    OutranksWord: {<<: ["+past+", {nullable: 'yes'}], type: string}\n"+
		"    Within: {<<: {nullable: true}, type: string}\n    Yes: {type: string, nullable: 'yes'}\n"))

	schema := StatedIn(lowerer, componentSchema(t, lowerer, "Past"))
	_, written := schema.Key("nullable")
	require.False(t, written, "the keyword lies past the bound")
	require.True(t, schema.Truncated)
	assert.False(t, nullableTrue(schema))
	assert.False(t, nullableTrue(StatedIn(lowerer, componentSchema(t, lowerer, "PastWord"))))
	outranks := StatedIn(lowerer, componentSchema(t, lowerer, "Outranks"))
	_, listed := outranks.Key("nullable")
	require.False(t, listed, "the key a source past the bound outranks is left out")
	assert.False(t, nullableTrue(outranks))
	assert.False(t, nullableTrue(StatedIn(lowerer, componentSchema(t, lowerer, "OutranksWord"))))
	assert.True(t, nullableTrue(StatedIn(lowerer, componentSchema(t, lowerer, "Within"))))
	assert.False(t, nullableTrue(StatedIn(lowerer, componentSchema(t, lowerer, "Yes"))))
}

// TestRef_ASchemaMergingPastTheBoundIsReportedOnce lowers, twice each, a
// schema whose `<<` merges run past nodeview.MergeDepthLimit and one whose
// inline allOf entry's do: each schema object draws one warning a run, at its
// own pointer, however many times a position lowers it.
func TestRef_ASchemaMergingPastTheBoundIsReportedOnce(t *testing.T) {
	t.Parallel()
	past := openapitest.NestedMerges(nodeview.MergeDepthLimit+1, "{minLength: 1}")
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    P: {<<: "+past+", type: string}\n"+
		"    A: {allOf: [{<<: "+past+", type: string}]}\n"))
	var reports []string
	for range 2 {
		for _, name := range []string{"P", "A"} {
			st := lowerer.Ctx.Statements().At(lowerer.Ctx.Position(jsontext.Pointer("/components/schemas/" + name)))
			_, diags := Ref(lowerer.Ctx, lowerer.Types, 0, st, jsontext.Pointer("/components/schemas/"+name))
			for _, diagnostic := range diags {
				if diagnostic.Code == diag.DegradedConstruct && strings.Contains(diagnostic.Message, "deeper than") {
					reports = append(reports, string(diagnostic.Provenance.Pointer))
				}
			}
		}
	}
	assert.Equal(t, []string{"/components/schemas/P", "/components/schemas/A/allOf/0"}, reports)
}
