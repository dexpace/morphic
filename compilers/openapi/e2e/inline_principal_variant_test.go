// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// variantEntrySchemas are three positions whose allOf lowers from an entry
// building a Union of variants, each beside a $ref naming that entry alone: a
// type list bounded on another entry, a type list with an object variant, and
// a schema the kind-inference policy reads as an object or an array.
var variantEntrySchemas = []string{
	"X: {allOf: [{type: [string, integer], maxLength: 5}, {minLength: 1}]}",
	"RE: {$ref: '#/components/schemas/X/allOf/0'}",
	"Y: {allOf: [{description: d}, {type: [string, object], properties: {a: {type: string}}}]}",
	"RY: {$ref: '#/components/schemas/Y/allOf/1'}",
	"I: {allOf: [{items: {type: string}, properties: {a: {type: string}}}, {description: d}]}",
	"RI: {$ref: '#/components/schemas/I/allOf/0'}",
}

// requireVariants returns the Union at id, which keeps nothing verbatim,
// holding its variants at the IDs want names in order.
func requireVariants(t *testing.T, doc *ir.Document, id ir.TypeID, want ...ir.TypeID) *ir.Union {
	t.Helper()
	u, ok := kindNode(t, doc, id).(*ir.Union)
	require.True(t, ok, "%s is a union", id)
	got := make([]ir.TypeID, 0, len(u.Variants))
	for _, v := range u.Variants {
		got = append(got, v.Type.Target)
	}
	require.Equal(t, want, got, "the variants of %s", id)
	assert.Empty(t, keptOn(u), "%s keeps nothing its variants hold", id)
	return u
}

// requireStringLengths holds the Scalar at id to the string lengths given, a
// nil bound being one it holds none of.
func requireStringLengths(t *testing.T, doc *ir.Document, id ir.TypeID, minLength, maxLength *int64) {
	t.Helper()
	sc, ok := kindNode(t, doc, id).(*ir.Scalar)
	require.True(t, ok, "%s is a scalar of its own", id)
	require.NotNil(t, sc.Constraints, "%s holds a bound", id)
	assert.Equal(t, minLength, sc.Constraints.MinLength, "the minLength %s holds", id)
	assert.Equal(t, maxLength, sc.Constraints.MaxLength, "the maxLength %s holds", id)
}

// TestInlinePrincipal_AReferenceToAVariantEntryNamesWhatTheEntryWrites holds
// the variants of the Union a position lowers from an allOf entry to the
// position's pointer, built from the whole conjunction, and those of the
// Union a $ref to the entry names to the entry's, built from the entry alone,
// whichever is declared first: the entry's string variant holds no bound the
// other entry writes, and neither Union keeps what a variant holds.
func TestInlinePrincipal_AReferenceToAVariantEntryNamesWhatTheEntryWrites(t *testing.T) {
	t.Parallel()
	const at = "t/anon/components/schemas/"
	docs := map[string]string{}
	inBothOrders(t, variantEntrySchemas, func(t *testing.T, schemas []string) {
		doc, diags := compileUnrefused(t, admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		assert.Empty(t, degradedInfoPointers(diags), "nothing is kept for want of a home")
		for _, d := range diags {
			assert.NotEqual(t, diag.DegradedConstruct, d.Code, "no report of a keyword kept: %s", d.Message)
		}

		requireVariants(t, doc, componentID("X"), at+"X/type/0", "t/prim/integer")
		requireStringLengths(t, doc, at+"X/type/0", new(int64(1)), new(int64(5)))
		requireVariants(t, doc, at+"X/allOf/0", at+"X/allOf/0/type/0", "t/prim/integer")
		requireStringLengths(t, doc, at+"X/allOf/0/type/0", nil, new(int64(5)))

		requireVariants(t, doc, componentID("Y"), "t/prim/string", at+"Y/type/1")
		requireVariants(t, doc, at+"Y/allOf/1", "t/prim/string", at+"Y/allOf/1/type/1")
		for _, id := range []ir.TypeID{at + "Y/type/1", at + "Y/allOf/1/type/1"} {
			m, ok := kindNode(t, doc, id).(*ir.Model)
			require.True(t, ok, "%s is the object variant", id)
			require.Len(t, m.Properties, 1, "%s declares the entry's member", id)
		}

		requireVariants(t, doc, componentID("I"), at+"I/type/0", at+"I/type/1")
		requireVariants(t, doc, at+"I/allOf/0", at+"I/allOf/0/type/0", at+"I/allOf/0/type/1")
		docs[t.Name()] = comparableDocument(t, doc)
	})
	declared, reversed := docs[t.Name()+"/declared"], docs[t.Name()+"/reversed"]
	require.NotEmpty(t, declared)
	assert.Empty(t, cmp.Diff(declared, reversed), "either order of declaration compiles one document")
}

// flatTwins are type lists and a schema the kind-inference policy reads as an
// object or an array, each written as an allOf whose entry the position lowers
// from, then flat, as one schema writing every keyword the conjunction does.
var flatTwins = []struct{ conjunction, flat string }{
	{
		"{allOf: [{type: [string, integer], maxLength: 5}, {minLength: 1}]}",
		"{type: [string, integer], maxLength: 5, minLength: 1}",
	},
	{
		"{allOf: [{minLength: 1}, {type: [string, integer], maxLength: 5}]}",
		"{type: [string, integer], maxLength: 5, minLength: 1}",
	},
	{"{allOf: [{type: [string, integer]}], minLength: 1}", "{type: [string, integer], minLength: 1}"},
	{
		"{allOf: [{type: [string, array], items: {type: object, properties: {a: {type: string}}}, maxLength: 5}, {minLength: 1}]}",
		"{type: [string, array], items: {type: object, properties: {a: {type: string}}}, maxLength: 5, minLength: 1}",
	},
	{
		"{allOf: [{type: [string, object], properties: {a: {type: string}}}, {description: d}]}",
		"{type: [string, object], properties: {a: {type: string}}, description: d}",
	},
	{
		"{allOf: [{items: {type: string}, properties: {a: {type: string}}}, {description: d}]}",
		"{items: {type: string}, properties: {a: {type: string}}, description: d}",
	},
}

// TestInlinePrincipal_AVariantEntryLowersAsItsFlatSpelling holds a position
// lowering a Union of variants from an allOf entry to the document the same
// keywords written flat at that position compile to, at every site a schema is
// written: the variants and what stands beneath them under the position's
// pointer, each holding what the flat spelling's does, and the same findings.
func TestInlinePrincipal_AVariantEntryLowersAsItsFlatSpelling(t *testing.T) {
	t.Parallel()
	for _, twin := range flatTwins {
		for _, site := range conjSites {
			want, _ := compileUnrefused(t, site.write("3.1.0", twin.flat, "S: {type: string}"))
			got, _ := compileUnrefused(t, site.write("3.1.0", twin.conjunction, "S: {type: string}"))
			assert.Empty(t, cmp.Diff(comparableDocument(t, want), comparableDocument(t, got)),
				"%s at the %s lowers as %s", twin.conjunction, site.name, twin.flat)
		}
	}
}
