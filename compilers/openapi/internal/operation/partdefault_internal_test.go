// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// defaultsRegistry is a hand-built registry holding one node of each kind the
// default content type reads, an alias chain closing on itself, and a union
// naming a node no registry entry declares.
func defaultsRegistry() ir.TypeRegistry {
	ref := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }
	prim := func(k ir.PrimKind) ir.TypeRef { return ref(ir.PrimTypeID(k)) }
	reg := ir.TypeRegistry{
		"t/base64":    &ir.Scalar{ID: "t/base64", Base: new(prim(ir.PrimBytes)), Encoding: &ir.Encoding{Name: "base64"}},
		"t/base64url": &ir.Scalar{ID: "t/base64url", Base: new(prim(ir.PrimBytes)), Encoding: &ir.Encoding{Name: "base64url"}},
		"t/email":     &ir.Scalar{ID: "t/email", Base: new(prim(ir.PrimString)), Encoding: &ir.Encoding{Name: "email"}},
		"t/opaque":    &ir.Scalar{ID: "t/opaque"},
		"t/enum":      &ir.Enum{ID: "t/enum", ValueType: ir.PrimInt32},
		"t/lit/str":   &ir.Literal{ID: "t/lit/str", ValueType: ir.PrimString},
		"t/lit/null":  &ir.Literal{ID: "t/lit/null", Value: ir.Value{Kind: ir.ValueNull}},
		"t/lit/obj":   &ir.Literal{ID: "t/lit/obj", Value: ir.Value{Kind: ir.ValueObject}},
		"t/lit/list":  &ir.Literal{ID: "t/lit/list", Value: ir.Value{Kind: ir.ValueList}},
		"t/model":     &ir.Model{ID: "t/model"},
		"t/map":       &ir.MapT{ID: "t/map", Value: prim(ir.PrimString)},
		"t/list":      &ir.List{ID: "t/list", Elem: prim(ir.PrimString)},
		"t/matrix":    &ir.List{ID: "t/matrix", Elem: ref("t/list")},
		"t/tuple":     &ir.Tuple{ID: "t/tuple", Elems: []ir.TypeRef{prim(ir.PrimInt64)}, Rest: new(prim(ir.PrimBool))},
		"t/u/agree":   &ir.Union{ID: "t/u/agree", Variants: []ir.Variant{{Type: prim(ir.PrimString)}, {Type: ref("t/lit/null")}, {Type: prim(ir.PrimNumber)}}},
		"t/u/differ":  &ir.Union{ID: "t/u/differ", Variants: []ir.Variant{{Type: prim(ir.PrimString)}, {Type: ref("t/model")}}},
		"t/u/missing": &ir.Union{ID: "t/u/missing", Variants: []ir.Variant{{Type: ref("t/absent")}}},
		"t/u/empty":   &ir.Union{ID: "t/u/empty"},
		"t/ext":       &ir.External{ID: "t/ext"},
		"t/loop/a":    &ir.Scalar{ID: "t/loop/a", Base: new(ref("t/loop/b"))},
		"t/loop/b":    &ir.Scalar{ID: "t/loop/b", Base: new(ref("t/loop/a"))},
	}
	for _, k := range []ir.PrimKind{ir.PrimString, ir.PrimBytes, ir.PrimAny, ir.PrimInt64, ir.PrimBool, ir.PrimNumber, ir.PrimDate} {
		reg[ir.PrimTypeID(k)] = &ir.Primitive{ID: ir.PrimTypeID(k), Prim: k}
	}
	return reg
}

// TestDefaultContentType_ReadsEachValueByItsRow holds the default content type
// to the row each kind of value takes at each minor, through its alias chain:
// one value per row, a value no row names, the rows contentEncoding and a
// scalar naming an encoding select, an array inside an array by its row or its
// items, a union by its members, a null passing over, and values whose rows
// differ taking none.
func TestDefaultContentType_ReadsEachValueByItsRow(t *testing.T) {
	t.Parallel()
	reg := defaultsRegistry()
	node := func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok }
	const (
		octet = "application/octet-stream"
		text  = "text/plain"
		json  = "application/json"
	)
	cases := []struct {
		name          string
		value         ir.TypeID
		encodedBy     contentEncoding
		v30, v31, v32 string
	}{
		{"a string", ir.PrimTypeID(ir.PrimString), unencoded, text, text, text},
		{"a date", ir.PrimTypeID(ir.PrimDate), unencoded, text, text, text},
		{"a number", ir.PrimTypeID(ir.PrimNumber), unencoded, text, text, text},
		{"a boolean", ir.PrimTypeID(ir.PrimBool), unencoded, text, text, text},
		{"raw bytes", ir.PrimTypeID(ir.PrimBytes), unencoded, octet, octet, octet},
		{"an untyped value", ir.PrimTypeID(ir.PrimAny), unencoded, "", octet, octet},
		{"format byte", "t/base64", unencoded, octet, text, text},
		{"format byte writing contentEncoding", "t/base64", encoded, octet, octet, octet},
		{"format byte, contentEncoding written by some declarations", "t/base64", partlyEncoded, "", "", ""},
		{"format base64url", "t/base64url", unencoded, text, text, text},
		{"format base64url writing contentEncoding", "t/base64url", encoded, text, octet, octet},
		{"a string a format names", "t/email", unencoded, text, text, text},
		{"a string writing contentEncoding", "t/email", encoded, text, octet, octet},
		{"a string, contentEncoding written by some declarations", "t/email", partlyEncoded, "", "", ""},
		{"an integer enum", "t/enum", unencoded, text, text, text},
		{"a string literal", "t/lit/str", unencoded, text, text, text},
		{"an object literal", "t/lit/obj", unencoded, json, json, json},
		{"a list literal", "t/lit/list", unencoded, "", "", ""},
		{"a null literal alone", "t/lit/null", unencoded, "", "", ""},
		{"a model", "t/model", unencoded, json, json, json},
		{"a map", "t/map", unencoded, json, json, json},
		{"an array inside an array", "t/list", unencoded, text, text, json},
		{"an array of arrays inside an array", "t/matrix", unencoded, text, text, json},
		{"a tuple inside an array", "t/tuple", unencoded, text, text, json},
		{"a union whose members agree, a null among them", "t/u/agree", unencoded, text, text, text},
		{"a union whose members differ", "t/u/differ", unencoded, "", "", ""},
		{"a union naming no node", "t/u/missing", unencoded, "", "", ""},
		{"a union of nothing", "t/u/empty", unencoded, "", "", ""},
		{"an opaque scalar", "t/opaque", unencoded, "", "", ""},
		{"an external type", "t/ext", unencoded, "", "", ""},
		{"an alias chain closing on itself", "t/loop/a", unencoded, "", "", ""},
		{"no node", "t/absent", unencoded, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for version, want := range map[string]string{"3.0.3": tc.v30, "3.1.0": tc.v31, "3.2.0": tc.v32} {
				got := defaultContentType(node, oas.Table().EncodingRules(version), []ir.TypeRef{{Target: tc.value}}, tc.encodedBy)
				if want == "" {
					assert.Nil(t, got, version)
					continue
				}
				assert.Equal(t, []string{want}, got, version)
			}
		})
	}
	rules := oas.Table().EncodingRules("3.1.0")
	assert.Equal(t, []string{"text/plain"}, defaultContentType(node, rules,
		[]ir.TypeRef{{Target: ir.PrimTypeID(ir.PrimString)}, {Target: ir.PrimTypeID(ir.PrimBool)}}, unencoded),
		"values of one row take it")
	assert.Nil(t, defaultContentType(node, rules,
		[]ir.TypeRef{{Target: ir.PrimTypeID(ir.PrimString)}, {Target: "t/model"}}, unencoded), "values of two rows take none")
	assert.Nil(t, defaultContentType(node, rules, nil, unencoded), "no value takes none")
}

// TestDefaultContentType_ReadsUpToItsBound holds the values one part's
// default reads to maxDefaultValues: a union of as many strings is read, and
// one of a string more takes no default.
func TestDefaultContentType_ReadsUpToItsBound(t *testing.T) {
	t.Parallel()
	union := func(n int) ir.TypeRegistry {
		u := &ir.Union{ID: "t/u"}
		for range n {
			u.Variants = append(u.Variants, ir.Variant{Type: ir.TypeRef{Target: ir.PrimTypeID(ir.PrimString)}})
		}
		return ir.TypeRegistry{"t/u": u, ir.PrimTypeID(ir.PrimString): &ir.Primitive{ID: ir.PrimTypeID(ir.PrimString), Prim: ir.PrimString}}
	}
	rules := oas.Table().EncodingRules("3.1.0")
	for n, want := range map[int][]string{maxDefaultValues - 1: {"text/plain"}, maxDefaultValues: nil} {
		reg := union(n)
		node := func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok }
		assert.Equal(t, want, defaultContentType(node, rules, []ir.TypeRef{{Target: "t/u"}}, unencoded),
			"%d strings beside the union read", n)
	}
}

// TestPartValues_ReadsEachItemOfEachDeclaration holds the values a part's
// encoding applies to: each declaration's type, a list's or a tuple's items in
// its place, and a union's members, and none at all where a union's members
// are not read.
func TestPartValues_ReadsEachItemOfEachDeclaration(t *testing.T) {
	t.Parallel()
	reg := defaultsRegistry()
	reg["t/u/lists"] = &ir.Union{ID: "t/u/lists", Variants: []ir.Variant{{Type: ir.TypeRef{Target: "t/list"}}, {Type: ir.TypeRef{Target: "t/model"}}}}
	node := func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok }
	decl := func(id ir.TypeID) *ir.Property { return &ir.Property{Type: ir.TypeRef{Target: id}} }

	values, ok := partValues(node, []*ir.Property{decl("t/tuple"), decl("t/u/lists"), decl("t/model")})
	require.True(t, ok)
	assert.Equal(t, []ir.TypeRef{
		{Target: ir.PrimTypeID(ir.PrimInt64)}, {Target: ir.PrimTypeID(ir.PrimBool)},
		{Target: ir.PrimTypeID(ir.PrimString)}, {Target: "t/model"}, {Target: "t/model"},
	}, values)

	values, ok = partValues(node, []*ir.Property{decl("t/model"), decl("t/u/missing")})
	assert.False(t, ok, "a union whose members are not read")
	assert.Nil(t, values)
}

// TestReadContentEncoding_ReadsEveryDeclaration holds what a part's
// declarations say of contentEncoding to each of them, through a $ref, a
// boolean schema saying nothing: none, each, or some writing it.
func TestReadContentEncoding_ReadsEveryDeclaration(t *testing.T) {
	t.Parallel()
	l, _ := loweredFor(t, openapitest.ComponentSpec(
		"    Plain: {type: string}\n    Encoded: {type: string, contentEncoding: base64}\n    Ref: {$ref: '#/components/schemas/Encoded'}\n"))
	get := func(name string) *oas3.JSONSchema[oas3.Referenceable] {
		js, ok := l.ctx.Doc.Components.Schemas.Get(name)
		require.True(t, ok, name)
		return js
	}
	boolean := openapitest.SchemaFromYAML(t.Context(), t, "true")
	for _, tc := range []struct {
		name    string
		schemas []*oas3.JSONSchema[oas3.Referenceable]
		want    contentEncoding
	}{
		{"none", []*oas3.JSONSchema[oas3.Referenceable]{get("Plain"), boolean}, unencoded},
		{"each, one through a $ref", []*oas3.JSONSchema[oas3.Referenceable]{get("Encoded"), get("Ref"), boolean}, encoded},
		{"some", []*oas3.JSONSchema[oas3.Referenceable]{get("Plain"), get("Ref")}, partlyEncoded},
		{"no declaration", nil, unencoded},
	} {
		assert.Equal(t, tc.want, readContentEncoding(l.ctx, tc.schemas), tc.name)
	}
}

// TestPrimitiveCase_KeysEachKind holds the case of a kind no row names to the
// zero case, which takes no default.
func TestPrimitiveCase_KeysEachKind(t *testing.T) {
	t.Parallel()
	assert.Zero(t, primitiveCase("no-such-kind", "", unencoded))
	assert.Equal(t, oas.DefaultScalar, primitiveCase(ir.PrimInt64, "base64", partlyEncoded),
		"a number takes its row whatever contentEncoding says")
	assert.Equal(t, oas.DefaultRawBinary, primitiveCase(ir.PrimBytes, "", partlyEncoded),
		"raw bytes take theirs too")
	assert.Equal(t, oas.DefaultUntyped, primitiveCase(ir.PrimAny, "base64", encoded), "and an untyped value its")
}
