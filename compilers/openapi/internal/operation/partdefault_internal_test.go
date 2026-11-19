// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
	"github.com/dexpace/morphic/ir"
)

// defaultsRegistry is a hand-built registry holding one node of each kind the
// default content type reads, an alias chain closing on itself, and a union
// naming a node no registry entry declares.
func defaultsRegistry() ir.TypeRegistry {
	ref := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }
	prim := func(k ir.PrimitiveKind) ir.TypeRef { return ref(ir.PrimitiveTypeID(k)) }
	reg := ir.TypeRegistry{
		"type/base64":    &ir.Scalar{ID: "type/base64", Base: new(prim(ir.PrimitiveBytes)), Encoding: &ir.Encoding{Name: "base64"}},
		"type/base64url": &ir.Scalar{ID: "type/base64url", Base: new(prim(ir.PrimitiveBytes)), Encoding: &ir.Encoding{Name: "base64url"}},
		"type/email":     &ir.Scalar{ID: "type/email", Base: new(prim(ir.PrimitiveString)), Encoding: &ir.Encoding{Name: "email"}},
		"type/opaque":    &ir.Scalar{ID: "type/opaque"},
		"type/enum":      &ir.Enum{ID: "type/enum", ValueType: ir.PrimitiveInt32},
		"type/lit/str":   &ir.Literal{ID: "type/lit/str", ValueType: ir.PrimitiveString},
		"type/lit/null":  &ir.Literal{ID: "type/lit/null", Value: ir.Value{Kind: ir.ValueNull}},
		"type/lit/obj":   &ir.Literal{ID: "type/lit/obj", Value: ir.Value{Kind: ir.ValueObject}},
		"type/lit/list":  &ir.Literal{ID: "type/lit/list", Value: ir.Value{Kind: ir.ValueList}},
		"type/model":     &ir.Model{ID: "type/model"},
		"type/map":       &ir.MapT{ID: "type/map", Value: prim(ir.PrimitiveString)},
		"type/list":      &ir.List{ID: "type/list", ElementType: prim(ir.PrimitiveString)},
		"type/matrix":    &ir.List{ID: "type/matrix", ElementType: ref("type/list")},
		"type/tuple":     &ir.Tuple{ID: "type/tuple", ElementTypes: []ir.TypeRef{prim(ir.PrimitiveInt64)}, Rest: new(prim(ir.PrimitiveBool))},
		"type/u/agree":   &ir.Union{ID: "type/u/agree", Variants: []ir.Variant{{Type: prim(ir.PrimitiveString)}, {Type: ref("type/lit/null")}, {Type: prim(ir.PrimitiveNumber)}}},
		"type/u/differ":  &ir.Union{ID: "type/u/differ", Variants: []ir.Variant{{Type: prim(ir.PrimitiveString)}, {Type: ref("type/model")}}},
		"type/u/missing": &ir.Union{ID: "type/u/missing", Variants: []ir.Variant{{Type: ref("type/absent")}}},
		"type/u/empty":   &ir.Union{ID: "type/u/empty"},
		"type/ext":       &ir.External{ID: "type/ext"},
		"type/loop/a":    &ir.Scalar{ID: "type/loop/a", Base: new(ref("type/loop/b"))},
		"type/loop/b":    &ir.Scalar{ID: "type/loop/b", Base: new(ref("type/loop/a"))},
	}
	for _, k := range []ir.PrimitiveKind{ir.PrimitiveString, ir.PrimitiveBytes, ir.PrimitiveAny, ir.PrimitiveInt64, ir.PrimitiveBool, ir.PrimitiveNumber, ir.PrimitiveDate} {
		reg[ir.PrimitiveTypeID(k)] = &ir.Primitive{ID: ir.PrimitiveTypeID(k), PrimitiveKind: k}
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
		{"a string", ir.PrimitiveTypeID(ir.PrimitiveString), unencoded, text, text, text},
		{"a date", ir.PrimitiveTypeID(ir.PrimitiveDate), unencoded, text, text, text},
		{"a number", ir.PrimitiveTypeID(ir.PrimitiveNumber), unencoded, text, text, text},
		{"a boolean", ir.PrimitiveTypeID(ir.PrimitiveBool), unencoded, text, text, text},
		{"raw bytes", ir.PrimitiveTypeID(ir.PrimitiveBytes), unencoded, octet, octet, octet},
		{"an untyped value", ir.PrimitiveTypeID(ir.PrimitiveAny), unencoded, "", octet, octet},
		{"format byte", "type/base64", unencoded, octet, text, text},
		{"format byte writing contentEncoding", "type/base64", encoded, octet, octet, octet},
		{"format byte, contentEncoding written by some declarations", "type/base64", partlyEncoded, "", "", ""},
		{"format base64url", "type/base64url", unencoded, text, text, text},
		{"format base64url writing contentEncoding", "type/base64url", encoded, text, octet, octet},
		{"a string a format names", "type/email", unencoded, text, text, text},
		{"a string writing contentEncoding", "type/email", encoded, text, octet, octet},
		{"a string, contentEncoding written by some declarations", "type/email", partlyEncoded, "", "", ""},
		{"an integer enum", "type/enum", unencoded, text, text, text},
		{"a string literal", "type/lit/str", unencoded, text, text, text},
		{"an object literal", "type/lit/obj", unencoded, json, json, json},
		{"a list literal", "type/lit/list", unencoded, "", "", ""},
		{"a null literal alone", "type/lit/null", unencoded, "", "", ""},
		{"a model", "type/model", unencoded, json, json, json},
		{"a map", "type/map", unencoded, json, json, json},
		{"an array inside an array", "type/list", unencoded, text, text, json},
		{"an array of arrays inside an array", "type/matrix", unencoded, text, text, json},
		{"a tuple inside an array", "type/tuple", unencoded, text, text, json},
		{"a union whose members agree, a null among them", "type/u/agree", unencoded, text, text, text},
		{"a union whose members differ", "type/u/differ", unencoded, "", "", ""},
		{"a union naming no node", "type/u/missing", unencoded, "", "", ""},
		{"a union of nothing", "type/u/empty", unencoded, "", "", ""},
		{"an opaque scalar", "type/opaque", unencoded, "", "", ""},
		{"an external type", "type/ext", unencoded, "", "", ""},
		{"an alias chain closing on itself", "type/loop/a", unencoded, "", "", ""},
		{"no node", "type/absent", unencoded, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for version, want := range map[string]string{"3.0.3": tc.v30, "3.1.0": tc.v31, "3.2.0": tc.v32} {
				got := defaultContentType(newPartUnions(), node, specrules.Table().EncodingRules(version), []ir.TypeRef{{Target: tc.value}}, tc.encodedBy)
				if want == "" {
					assert.Nil(t, got, version)
					continue
				}
				assert.Equal(t, []string{want}, got, version)
			}
		})
	}
	rules := specrules.Table().EncodingRules("3.1.0")
	assert.Equal(t, []string{"text/plain"}, defaultContentType(newPartUnions(), node, rules,
		[]ir.TypeRef{{Target: ir.PrimitiveTypeID(ir.PrimitiveString)}, {Target: ir.PrimitiveTypeID(ir.PrimitiveBool)}}, unencoded),
		"values of one row take it")
	assert.Nil(t, defaultContentType(newPartUnions(), node, rules,
		[]ir.TypeRef{{Target: ir.PrimitiveTypeID(ir.PrimitiveString)}, {Target: "type/model"}}, unencoded), "values of two rows take none")
	assert.Nil(t, defaultContentType(newPartUnions(), node, rules, nil, unencoded), "no value takes none")
}

// TestDefaultContentType_ReadsUpToItsBound holds the values one part's
// default reads to maxDefaultValues: a union of as many strings is read, and
// one of a string more takes no default.
func TestDefaultContentType_ReadsUpToItsBound(t *testing.T) {
	t.Parallel()
	union := func(n int) ir.TypeRegistry {
		u := &ir.Union{ID: "type/u"}
		for range n {
			u.Variants = append(u.Variants, ir.Variant{Type: ir.TypeRef{Target: ir.PrimitiveTypeID(ir.PrimitiveString)}})
		}
		return ir.TypeRegistry{"type/u": u, ir.PrimitiveTypeID(ir.PrimitiveString): &ir.Primitive{ID: ir.PrimitiveTypeID(ir.PrimitiveString), PrimitiveKind: ir.PrimitiveString}}
	}
	rules := specrules.Table().EncodingRules("3.1.0")
	for n, want := range map[int][]string{maxDefaultValues - 1: {"text/plain"}, maxDefaultValues: nil} {
		reg := union(n)
		node := func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok }
		assert.Equal(t, want, defaultContentType(newPartUnions(), node, rules, []ir.TypeRef{{Target: "type/u"}}, unencoded),
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
	reg["type/u/lists"] = &ir.Union{ID: "type/u/lists", Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/list"}}, {Type: ir.TypeRef{Target: "type/model"}}}}
	node := func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok }
	decl := func(id ir.TypeID) *ir.Property { return &ir.Property{Type: ir.TypeRef{Target: id}} }

	values, ok := partValues(newPartUnions(), node, []*ir.Property{decl("type/tuple"), decl("type/u/lists"), decl("type/model")})
	require.True(t, ok)
	assert.Equal(t, []ir.TypeRef{
		{Target: ir.PrimitiveTypeID(ir.PrimitiveInt64)}, {Target: ir.PrimitiveTypeID(ir.PrimitiveBool)},
		{Target: ir.PrimitiveTypeID(ir.PrimitiveString)}, {Target: "type/model"}, {Target: "type/model"},
	}, values)

	values, ok = partValues(newPartUnions(), node, []*ir.Property{decl("type/model"), decl("type/u/missing")})
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
	assert.Equal(t, specrules.DefaultScalar, primitiveCase(ir.PrimitiveInt64, "base64", partlyEncoded),
		"a number takes its row whatever contentEncoding says")
	assert.Equal(t, specrules.DefaultRawBinary, primitiveCase(ir.PrimitiveBytes, "", partlyEncoded),
		"raw bytes take theirs too")
	assert.Equal(t, specrules.DefaultUntyped, primitiveCase(ir.PrimitiveAny, "base64", encoded), "and an untyped value its")
}
