// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
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
	union, ok := kindNode(t, doc, id).(*ir.Union)
	require.True(t, ok, "%s is a union", id)
	got := make([]ir.TypeID, 0, len(union.Variants))
	for _, variant := range union.Variants {
		got = append(got, variant.Type.Target)
	}
	require.Equal(t, want, got, "the variants of %s", id)
	assert.Empty(t, keptOn(union), "%s keeps nothing its variants hold", id)
	return union
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
	const at = "type/anonymous/components/schemas/"
	docs := map[string]string{}
	inBothOrders(t, variantEntrySchemas, func(t *testing.T, schemas []string) {
		doc, diags := compileUnrefused(t, admissionSpec("3.1.0", "", schemas...))
		requireErrorsAndWarnings(t, diags, nil)
		assert.Empty(t, degradedInfoPointers(diags), "nothing is kept for want of a home")
		for _, diagnostic := range diags {
			assert.NotEqual(t, diag.DegradedConstruct, diagnostic.Code, "no report of a keyword kept: %s", diagnostic.Message)
		}

		requireVariants(t, doc, componentID("X"), at+"X/type/0", "type/primitive/integer")
		requireStringLengths(t, doc, at+"X/type/0", new(int64(1)), new(int64(5)))
		requireVariants(t, doc, at+"X/allOf/0", at+"X/allOf/0/type/0", "type/primitive/integer")
		requireStringLengths(t, doc, at+"X/allOf/0/type/0", nil, new(int64(5)))

		requireVariants(t, doc, componentID("Y"), "type/primitive/string", at+"Y/type/1")
		requireVariants(t, doc, at+"Y/allOf/1", "type/primitive/string", at+"Y/allOf/1/type/1")
		for _, id := range []ir.TypeID{at + "Y/type/1", at + "Y/allOf/1/type/1"} {
			model, ok := kindNode(t, doc, id).(*ir.Model)
			require.True(t, ok, "%s is the object variant", id)
			require.Len(t, model.Properties, 1, "%s declares the entry's member", id)
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

// whereBoundLands lists, sorted, where doc states the bound keyword names: each
// node or carrier holding it under field, as "<id> holds", and each keeping it
// verbatim, as "<id> keeps <reason>", the holder being the nearest object
// above it that has an ID.
func whereBoundLands(t *testing.T, doc *ir.Document, keyword, field string) []string {
	t.Helper()
	var out []string
	var walk func(v any, holder string)
	walk = func(v any, holder string) {
		switch v := v.(type) {
		case map[string]any:
			if id, ok := v["id"].(string); ok {
				holder = id
			}
			if cons, ok := v["constraints"].(map[string]any); ok && cons[field] != nil {
				out = append(out, holder+" holds")
			}
			if kept, ok := v["unmodeled"].(map[string]any); ok {
				if entry, ok := kept["openapi:"+keyword].(map[string]any); ok {
					out = append(out, fmt.Sprintf("%s keeps %v", holder, entry["reason"]))
				}
			}
			for _, x := range v {
				walk(x, holder)
			}
		case []any:
			for _, x := range v {
				walk(x, holder)
			}
		}
	}
	walk(documentTree(t, doc), "")
	slices.Sort(out)
	return out
}

// unionShapedBound is a principal that lowers to a Union and a bound written
// beside its allOf or on another entry, with where the bound lands below the
// position's ID, on the variant taking it, or kept on the Union where none
// does, and whether the flat spelling lands it there too.
type unionShapedBound struct {
	name, principal, keyword, field, value, at string
	kept, flatAlike                            bool
}

// unionShapedBounds are a type list's array variant and the kind-inference
// policy's taking a collection bound, as the flat spelling's do, and the bounds
// no node of the position takes kept on the Union: one a oneOf with an array
// branch writes beside it, as flat, and an object bound of a folded entry,
// which no object variant reads (#2062).
var unionShapedBounds = []unionShapedBound{
	{"type list's array", "type: [string, array], items: {type: string}", "minItems", "minItems", "13", "/type/1", false, true},
	{"inferred array", "items: {type: string}, properties: {a: {type: string}}", "minItems", "minItems", "13", "/type/1", false, true},
	{"union's array branch", "oneOf: [{type: array, items: {type: string}}, {type: string}]", "minItems", "minItems", "13", "", true, true},
	{"type list's object", "type: [string, object]", "minProperties", "minProperties", "19", "", true, false},
	{"inferred object", "items: {type: string}, properties: {a: {type: string}}", "minProperties", "minProperties", "19", "", true, false},
}

// TestInlinePrincipal_ABoundBesideAUnionShapedPrincipalLandsOnce holds a
// collection or object bound written beside an allOf whose entry lowers to a
// Union, or on another entry in either order, to one home at every site: the
// variant taking it, or the Union keeping it with a report, as the flat
// spelling lands it where it lands alike.
func TestInlinePrincipal_ABoundBesideAUnionShapedPrincipalLandsOnce(t *testing.T) {
	t.Parallel()
	for _, b := range unionShapedBounds {
		bound := b.keyword + ": " + b.value
		spellings := map[string]string{
			"beside":       "{allOf: [{" + b.principal + "}], " + bound + "}",
			"entry":        "{allOf: [{" + b.principal + "}, {" + bound + "}]}",
			"entry first":  "{allOf: [{" + bound + "}, {" + b.principal + "}]}",
			"flat":         "{" + b.principal + ", " + bound + "}",
			"allOf beside": "{" + b.principal + ", allOf: [{" + bound + "}]}",
		}
		for name, schema := range spellings {
			if !b.flatAlike && (name == "flat" || name == "allOf beside") {
				continue
			}
			for _, site := range conjSites {
				doc, diags := compileUnrefused(t, site.write("3.1.0", schema))
				at := fmt.Sprintf("%s %s at the %s", b.name, name, site.name)
				want := "type/anonymous" + site.pointer + b.at + " holds"
				if b.kept {
					want = string(entryPositionID(jsontext.Pointer(site.pointer))) + " keeps degraded_lowering"
					assert.NotEmpty(t, degradedInfoPointers(diags), "%s: the bound kept is reported", at)
				}
				assert.Equal(t, []string{want}, whereBoundLands(t, doc, b.keyword, b.field), "%s: where %s lands", at, bound)
			}
		}
	}
}
