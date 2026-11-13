// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// typeArrayVariant is what one variant of a type array lowers to: the shared
// primitive prim names, or, where prim is empty, a node of its own holding held.
type typeArrayVariant struct {
	prim ir.TypeID
	held *ir.Constraints
}

// typeArrayCase is a type array and what it lowers to: each variant in its
// listed order, and the keywords its Union keeps.
type typeArrayCase struct {
	name, schema string
	variants     []typeArrayVariant
	kept         []string
}

// typeArrayCases are the type arrays whose bounds apply to some listed types and
// not to others, with the bounds each variant holds.
func typeArrayCases() []typeArrayCase {
	node := func(c *ir.Constraints) typeArrayVariant { return typeArrayVariant{held: c} }
	prim := func(k ir.PrimKind) typeArrayVariant { return typeArrayVariant{prim: ir.PrimTypeID(k)} }
	one := new(int64(1))
	return []typeArrayCase{
		{"a numeric bound beside an object", "{type: [integer, object], properties: {a: {type: string}}, minimum: 1}",
			[]typeArrayVariant{node(&ir.Constraints{Min: new(ir.BigVal("1"))}), node(nil)}, nil},
		{"a string bound beside an object", "{type: [string, object], properties: {a: {type: string}}, minLength: 2}",
			[]typeArrayVariant{node(&ir.Constraints{MinLength: new(int64(2))}), node(nil)}, nil},
		{"a bound of each group beside a paired format", "{type: [string, integer], format: date, minimum: 1, minLength: 2}",
			[]typeArrayVariant{node(&ir.Constraints{MinLength: new(int64(2))}), node(&ir.Constraints{Min: new(ir.BigVal("1"))})}, nil},
		{"a string bound alone", "{type: [string, integer], maxLength: 3}",
			[]typeArrayVariant{node(&ir.Constraints{MaxLength: new(int64(3))}), prim(ir.PrimInteger)}, nil},
		{"a numeric bound both numbers take", "{type: [integer, number], minimum: 1}",
			[]typeArrayVariant{node(&ir.Constraints{Min: new(ir.BigVal("1"))}), node(&ir.Constraints{Min: new(ir.BigVal("1"))})}, nil},
		{"a bound no listed type takes", "{type: [string, boolean], minimum: 1}",
			[]typeArrayVariant{prim(ir.PrimString), prim(ir.PrimBool)}, []string{"openapi:minimum"}},
		{"a collection bound and a string bound", "{type: [array, string], items: {type: string}, minItems: 1, maxLength: 4}",
			[]typeArrayVariant{node(&ir.Constraints{MinItems: one}), node(&ir.Constraints{MaxLength: new(int64(4))})}, nil},
		{"an empty pattern, which bounds nothing", "{type: [string, integer], pattern: ''}",
			[]typeArrayVariant{prim(ir.PrimString), prim(ir.PrimInteger)}, []string{"openapi:pattern"}},
	}
}

// TestLower_ATypeArraysBoundLandsOnTheVariantItConstrains pins a value bound
// beside a type array at a component, a property, items and a parameter: each
// variant whose type takes it holds it, a Scalar of its own at
// <pointer>/type/<index> where it would otherwise be the shared primitive, a
// variant whose type takes none holds none, the Union keeps the bound no listed
// type takes, and a property or parameter holds none
// (docs/compilers/openapi.md#type-arrays).
func TestLower_ATypeArraysBoundLandsOnTheVariantItConstrains(t *testing.T) {
	t.Parallel()
	for _, tc := range typeArrayCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, atEverySite("3.1.0", tc.schema))
			openapitest.RequireNoErrorDiags(t, diags)
			for _, site := range sitesOf(t, doc) {
				assert.Nil(t, site.held, "%s: a carrier holds no bound of a type array", site.pointer)
				assert.Empty(t, site.kept, site.pointer)
				u, ok := site.node.(*ir.Union)
				require.True(t, ok, "%s: got %T", site.pointer, site.node)
				assert.Equal(t, tc.kept, unmodeledKeys(u.Unmodeled), site.pointer)
				require.Len(t, u.Variants, len(tc.variants), site.pointer)
				for i, want := range tc.variants {
					got := u.Variants[i].Type.Target
					if want.prim != "" {
						assert.Equal(t, want.prim, got, "%s variant %d", site.pointer, i)
						continue
					}
					assert.Equal(t, ir.TypeID(fmt.Sprintf("t/anon%s/type/%d", site.pointer, i)), got, "%s variant %d", site.pointer, i)
					assert.Equal(t, want.held, heldBounds(doc.Types[got]), "%s variant %d", site.pointer, i)
					if sc, isScalar := doc.Types[got].(*ir.Scalar); isScalar {
						assert.Nil(t, sc.Encoding, "%s variant %d: hoisted for its bounds, it names no encoding", site.pointer, i)
					}
				}
			}
		})
	}
}

// TestLower_ATypeArraysVariantsSwapWithItsTypes pins the order of a type list:
// each case of TestLower_ATypeArraysBoundLandsOnTheVariantItConstrains with its
// two types listed the other way lowers to the same variants at swapped indices,
// and to nothing else that differs.
func TestLower_ATypeArraysVariantsSwapWithItsTypes(t *testing.T) {
	t.Parallel()
	for _, tc := range typeArrayCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start := strings.Index(tc.schema, "[") + 1
			end := strings.Index(tc.schema, "]")
			types := strings.Split(tc.schema[start:end], ", ")
			require.Len(t, types, 2)
			reversed := tc.schema[:start] + types[1] + ", " + types[0] + tc.schema[end:]
			want, wantDiags := parseFull(t, atEverySite("3.1.0", tc.schema))
			got, gotDiags := parseFull(t, atEverySite("3.1.0", reversed))
			got, gotDiags = swappedTypeIndices(t, got, gotDiags)
			opts := []cmp.Option{cmpopts.IgnoreFields(ir.Document{}, "Sources"), cmpopts.EquateEmpty()}
			assert.Empty(t, cmp.Diff(want, got, opts...))
			assert.ElementsMatch(t, wantDiags, gotDiags)
		})
	}
}

// swappedTypeIndices returns doc and diags with what each holds at
// <pointer>/type/0 and <pointer>/type/1 swapped, and every Union's variants
// listed the other way, as the same schema listing its two types in the other
// order lowers where every Union is a type array's.
func swappedTypeIndices(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	swap := strings.NewReplacer("/type/0", "/type/1", "/type/1", "/type/0")
	encoded, err := json.Marshal(struct {
		Doc   *ir.Document
		Diags []ir.Diagnostic
	}{doc, diags})
	require.NoError(t, err)
	var out struct {
		Doc   *ir.Document
		Diags []ir.Diagnostic
	}
	require.NoError(t, json.Unmarshal([]byte(swap.Replace(string(encoded))), &out))
	for _, td := range out.Doc.Types {
		if u, ok := td.(*ir.Union); ok {
			slices.Reverse(u.Variants)
		}
	}
	return out.Doc, out.Diags
}

// TestLower_ATypeArraysFormatIsTheVariantsItPairsWith pins a format beside a
// type array: the variant of the type the table pairs it with takes it, the
// others none, and one paired with no listed type is every scalar variant's
// encoding name, at every site.
func TestLower_ATypeArraysFormatIsTheVariantsItPairsWith(t *testing.T) {
	t.Parallel()
	byteScalar := &ir.Scalar{Base: new(ir.TypeRef{Target: ir.PrimTypeID(ir.PrimBytes)}),
		Encoding: &ir.Encoding{Name: "base64", WireType: new(ir.TypeRef{Target: ir.PrimTypeID(ir.PrimString)})}}
	mine := func(k ir.PrimKind) *ir.Scalar {
		return &ir.Scalar{Base: new(ir.TypeRef{Target: ir.PrimTypeID(k)}), Encoding: &ir.Encoding{Name: "x-mine"}}
	}
	for _, tc := range []struct {
		name, schema string
		variants     []any // an ir.PrimKind for a shared primitive, an *ir.Scalar for a node
	}{
		{"date pairs with a string", "{type: [string, integer], format: date}", []any{ir.PrimDate, ir.PrimInteger}},
		{"int32 pairs with an integer", "{type: [string, integer], format: int32}", []any{ir.PrimString, ir.PrimInt32}},
		{"byte pairs with a string", "{type: [string, integer], format: byte}", []any{byteScalar, ir.PrimInteger}},
		{"a format pairing with neither", "{type: [string, integer], format: x-mine}",
			[]any{mine(ir.PrimString), mine(ir.PrimInteger)}},
		{"int32 pairs with both numbers", "{type: [integer, number], format: int32}", []any{ir.PrimInt32, ir.PrimInt32}},
		{"a redaction names no shape", "{type: [string, integer], format: password}", []any{ir.PrimString, ir.PrimInteger}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, atEverySite("3.1.0", tc.schema))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Empty(t, diags)
			for _, site := range sitesOf(t, doc) {
				u, ok := site.node.(*ir.Union)
				require.True(t, ok, "%s: got %T", site.pointer, site.node)
				assert.Empty(t, u.Unmodeled, site.pointer)
				for i, want := range tc.variants {
					got := u.Variants[i].Type.Target
					if prim, shared := want.(ir.PrimKind); shared {
						assert.Equal(t, ir.PrimTypeID(prim), got, "%s variant %d", site.pointer, i)
						continue
					}
					sc, isScalar := doc.Types[got].(*ir.Scalar)
					require.True(t, isScalar, "%s variant %d: got %T", site.pointer, i, doc.Types[got])
					wantScalar, _ := want.(*ir.Scalar)
					assert.Equal(t, wantScalar.Base, sc.Base, "%s variant %d", site.pointer, i)
					assert.Equal(t, wantScalar.Encoding, sc.Encoding, "%s variant %d", site.pointer, i)
				}
			}
		})
	}
}

// TestLower_ATypeArraysContentIsTheStringVariants pins the content vocabulary
// beside a type array: the string variant holds it, as a Scalar whose Encoding
// names it, and the union keeps nothing; where no listed type is a string, the
// union keeps it, reported, as any position with no Encoding does.
func TestLower_ATypeArraysContentIsTheStringVariants(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, atEverySite("3.1.0", "{type: [string, integer], contentEncoding: base64}"))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, diags)
	for _, site := range sitesOf(t, doc) {
		u, ok := site.node.(*ir.Union)
		require.True(t, ok, "%s: got %T", site.pointer, site.node)
		assert.Empty(t, u.Unmodeled, site.pointer)
		sc, isScalar := doc.Types[u.Variants[0].Type.Target].(*ir.Scalar)
		require.True(t, isScalar, site.pointer)
		assert.Equal(t, &ir.Encoding{Name: "base64"}, sc.Encoding, site.pointer)
		assert.Equal(t, ir.PrimTypeID(ir.PrimInteger), u.Variants[1].Type.Target, site.pointer)
	}

	doc, diags = parseFull(t, atEverySite("3.1.0", "{type: [integer, boolean], contentEncoding: base64}"))
	openapitest.RequireNoErrorDiags(t, diags)
	for _, site := range sitesOf(t, doc) {
		u, ok := site.node.(*ir.Union)
		require.True(t, ok, "%s: got %T", site.pointer, site.node)
		assert.Equal(t, []string{"openapi:contentEncoding"}, unmodeledKeys(u.Unmodeled), site.pointer)
		assert.Equal(t, []ir.TypeID{ir.PrimTypeID(ir.PrimInteger), ir.PrimTypeID(ir.PrimBool)},
			[]ir.TypeID{u.Variants[0].Type.Target, u.Variants[1].Type.Target}, site.pointer)
		openapitest.AssertInfoDiagAt(t, diags, site.pointer+"/contentEncoding")
	}
	assert.Len(t, diags, 4)
}
