// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestPartMembers_DeadEnds covers the walk's dead ends on a hand-built
// registry: an ID nothing declares, an opaque scalar, a kind that is no model,
// an alias chain that closes on itself, and a Base chain that closes on
// itself. None may invent a property, which the encoding map would carry as a
// dangling reference, and a cyclic composition still yields the members it
// declares. No compile builds the alias cycle, but the walk reads whatever
// registry it is handed, which is what its bound is for.
func TestPartMembers_DeadEnds(t *testing.T) {
	t.Parallel()
	file := ir.Property{ID: "property/model/cycle/file", WireName: "file"}
	reg := ir.TypeRegistry{
		"type/opaque":      &ir.Scalar{ID: "type/opaque"},
		"type/cycle/a":     &ir.Scalar{ID: "type/cycle/a", Base: &ir.TypeRef{Target: "type/cycle/b"}},
		"type/cycle/b":     &ir.Scalar{ID: "type/cycle/b", Base: &ir.TypeRef{Target: "type/cycle/a"}},
		"type/string":      &ir.Primitive{ID: "type/string", PrimitiveKind: ir.PrimitiveString},
		"type/model/cycle": &ir.Model{ID: "type/model/cycle", Base: &ir.TypeRef{Target: "type/model/cycle"}, Properties: []ir.Property{file}},
	}
	for _, tc := range []struct {
		name string
		body ir.TypeID
	}{
		{"an ID no registry entry declares", "type/absent"},
		{"an opaque scalar standing for nothing", "type/opaque"},
		{"a kind that declares no properties", "type/string"},
		{"an alias chain that closes on itself", "type/cycle/a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			members, complete, _ := partMembers(reg, tc.body)
			assert.Empty(t, members, "no property stands behind this body, and none is invented")
			assert.True(t, complete)
		})
	}
	members, complete, _ := partMembers(reg, "type/model/cycle")
	require.Len(t, members["file"], 1, "a model whose base closes on itself still has its own members")
	assert.Equal(t, file.ID, members["file"][0].ID)
	assert.True(t, complete)
}

// TestPartMembers_AUnionBodyHasItsVariantsMembers pins a body standing for a
// union, as one a composition is distributed across does, through an alias
// too: its members are its variants', through a union nested in a variant,
// each variant's declaration of a name in source order, and a declaration
// several variants share listed once.
func TestPartMembers_AUnionBodyHasItsVariantsMembers(t *testing.T) {
	t.Parallel()
	shared := ir.Property{ID: "property/body/file", WireName: "file"}
	first := ir.Property{ID: "property/one/x", WireName: "x"}
	second := ir.Property{ID: "property/two/x", WireName: "x"}
	deep := ir.Property{ID: "property/deep/y", WireName: "y"}
	reg := ir.TypeRegistry{
		"type/one":    &ir.Model{ID: "type/one", Properties: []ir.Property{shared, first}},
		"type/two":    &ir.Model{ID: "type/two", Properties: []ir.Property{shared, second}},
		"type/deep":   &ir.Model{ID: "type/deep", Properties: []ir.Property{deep, {ID: "property/deep/x", WireName: "x"}}},
		"type/inner":  &ir.Union{ID: "type/inner", Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/deep"}}, {Type: ir.TypeRef{Target: "type/u"}}}},
		"type/innera": &ir.Scalar{ID: "type/innera", Base: &ir.TypeRef{Target: "type/inner"}},
		"type/u": &ir.Union{ID: "type/u", Variants: []ir.Variant{
			{Type: ir.TypeRef{Target: "type/one"}}, {Type: ir.TypeRef{Target: "type/innera"}}, {Type: ir.TypeRef{Target: "type/two"}},
		}},
		"type/alias": &ir.Scalar{ID: "type/alias", Base: &ir.TypeRef{Target: "type/u"}},
		"type/nil":   (*ir.Union)(nil),
	}
	ids := func(props []*ir.Property) []ir.PropertyID {
		out := make([]ir.PropertyID, 0, len(props))
		for _, p := range props {
			out = append(out, p.ID)
		}
		return out
	}
	for _, body := range []ir.TypeID{"type/u", "type/alias"} {
		members, complete, _ := partMembers(reg, body)
		assert.True(t, complete, "%s", body)
		require.Len(t, members, 3, "%s", body)
		assert.Equal(t, []ir.PropertyID{shared.ID}, ids(members["file"]), "a shared declaration is listed once: %s", body)
		assert.Equal(t, []ir.PropertyID{first.ID, "property/deep/x", second.ID}, ids(members["x"]),
			"each variant's declaration, in source order, the nested union's in its place: %s", body)
		assert.Equal(t, []ir.PropertyID{deep.ID}, ids(members["y"]), "a nested union's variant's member: %s", body)
	}
	members, complete, _ := partMembers(reg, "type/nil")
	assert.Empty(t, members, "a typed nil union stands for no members")
	assert.True(t, complete)
}

// narrowingRegistry holds t/n, a model declaring note and composing t/u, a
// union of t/a, declaring file, and t/b, declaring url; t/two, composing t/u
// and t/v, two unions; and t/plain, which composes no union.
func narrowingRegistry() ir.TypeRegistry {
	str := ir.TypeRef{Target: "type/primitive/string"}
	model := func(id ir.TypeID, props []ir.Property, mixins ...ir.TypeID) *ir.Model {
		m := &ir.Model{ID: id, Properties: props}
		for _, mixin := range mixins {
			m.Mixins = append(m.Mixins, ir.TypeRef{Target: mixin})
		}
		return m
	}
	union := func(id ir.TypeID, variants ...ir.TypeID) *ir.Union {
		u := &ir.Union{ID: id}
		for _, v := range variants {
			u.Variants = append(u.Variants, ir.Variant{Type: ir.TypeRef{Target: v}})
		}
		return u
	}
	return ir.TypeRegistry{
		"type/primitive/string": &ir.Primitive{ID: "type/primitive/string", PrimitiveKind: ir.PrimitiveString},
		"type/a":                model("type/a", []ir.Property{{ID: "property/a/file", WireName: "file", Type: str}}),
		"type/b":                model("type/b", []ir.Property{{ID: "property/b/url", WireName: "url", Type: str}}),
		"type/c":                model("type/c", []ir.Property{{ID: "property/c/id", WireName: "id", Type: str}}),
		"type/u":                union("type/u", "type/a", "type/b"),
		"type/v":                union("type/v", "type/c"),
		"type/n":                model("type/n", []ir.Property{{ID: "property/n/note", WireName: "note", Type: str}}, "type/u"),
		"type/two":              model("type/two", nil, "type/u", "type/v"),
		"type/plain":            model("type/plain", []ir.Property{{ID: "property/plain/x", WireName: "x", Type: str}}),
	}
}

// TestPartMembers_AModelNarrowingAUnionHasItsShapesMembers pins a body that is
// a model narrowing a union to its own members and each shape's, so a part
// only a variant declares has a declaration to key, and a model narrowing two
// unions, whose shapes ir.NarrowedShapes does not list, to its own alone,
// named as unread.
func TestPartMembers_AModelNarrowingAUnionHasItsShapesMembers(t *testing.T) {
	t.Parallel()
	reg := narrowingRegistry()
	members, complete, unread := partMembers(reg, "type/n")
	assert.True(t, complete)
	assert.Empty(t, unread)
	got := map[string][]ir.PropertyID{}
	for name, decls := range members {
		for _, d := range decls {
			got[name] = append(got[name], d.ID)
		}
	}
	assert.Equal(t, map[string][]ir.PropertyID{"note": {"property/n/note"}, "file": {"property/a/file"}, "url": {"property/b/url"}}, got)

	members, _, unread = partMembers(reg, "type/two")
	assert.Empty(t, members)
	assert.Equal(t, []unreadNarrowing{{model: "type/two", why: "several"}}, unread)
}

// TestShapeFlattenings_ReadsShapesWithinItsLimit pins the shapes read to a
// model narrowing a union, none to one composing no union, and a narrowing
// whose walk passes the limit to none, named as unread past it.
func TestShapeFlattenings_ReadsShapesWithinItsLimit(t *testing.T) {
	t.Parallel()
	reg := narrowingRegistry()
	shapes, why := shapeFlattenings(reg, "type/n", ir.FlattenedProperties(reg, "type/n"), maxNarrowedSteps)
	assert.Nil(t, why)
	assert.Len(t, shapes, 2, "one shape for each variant")
	shapes, why = shapeFlattenings(reg, "type/plain", ir.FlattenedProperties(reg, "type/plain"), maxNarrowedSteps)
	assert.Nil(t, why)
	assert.Empty(t, shapes)
	shapes, why = shapeFlattenings(reg, "type/n", ir.FlattenedProperties(reg, "type/n"), 1)
	assert.Empty(t, shapes)
	assert.Equal(t, &unreadNarrowing{model: "type/n", why: "past 1 steps"}, why)
}

// TestDeclaredProp_PlacesNothingTheBindingsDoNot pins that a schema the
// bindings never read names no property, so a part's reading keeps it.
func TestDeclaredProp_PlacesNothingTheBindingsDoNot(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	_, ok := declaredProp(l.ctx, oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{}))
	assert.False(t, ok)
}

// TestMemberHolders_StopsPastItsBound pins that the union walk reads
// maxPartUnions unions and says it stopped past them, keeping the holders it
// reached, and that a chain at the bound is read whole.
func TestMemberHolders_StopsPastItsBound(t *testing.T) {
	t.Parallel()
	chain := func(unions int) (ir.TypeRegistry, ir.TypeID) {
		reg := ir.TypeRegistry{}
		for i := range unions {
			variants := []ir.Variant{{Type: ir.TypeRef{Target: ir.TypeID(fmt.Sprintf("type/m/%d", i))}}}
			if i+1 < unions {
				variants = append(variants, ir.Variant{Type: ir.TypeRef{Target: ir.TypeID(fmt.Sprintf("type/u/%d", i+1))}})
			}
			reg[ir.TypeID(fmt.Sprintf("type/u/%d", i))] = &ir.Union{ID: ir.TypeID(fmt.Sprintf("type/u/%d", i)), Variants: variants}
			reg[ir.TypeID(fmt.Sprintf("type/m/%d", i))] = &ir.Model{ID: ir.TypeID(fmt.Sprintf("type/m/%d", i))}
		}
		return reg, "type/u/0"
	}
	node := func(reg ir.TypeRegistry) func(ir.TypeID) (ir.TypeDef, bool) {
		return func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok }
	}
	reg, body := chain(maxPartUnions)
	holders, complete := memberHolders(node(reg), body)
	assert.True(t, complete, "a chain of maxPartUnions unions is read whole")
	assert.Len(t, holders, maxPartUnions)

	reg, body = chain(maxPartUnions + 1)
	holders, complete = memberHolders(node(reg), body)
	assert.False(t, complete, "one union more is past the bound")
	assert.Len(t, holders, maxPartUnions, "the holders the walk reached are kept")
}

// TestBodyParts_RefHopBound covers the walk's $ref hop bound: past it the walk
// stops and says so.
func TestBodyParts_RefHopBound(t *testing.T) {
	t.Parallel()
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
	v := partVisits{c: newRawLowerer(&soa.OpenAPI{}).ctx.Ctx, entered: map[*oas3.Schema]int{}, cut: map[*oas3.Schema]bool{}}
	w := bodyParts(js, maxPartRefHops+1, v)
	assert.Nil(t, w.parts,
		"past the bound the walk stops rather than descending further")
	assert.Len(t, v.cut, 1, "and records the target it stopped at")
}

// TestPartTypeFlags_DecidesOnlyTheKindsItReads holds the type walk to what it
// decides at each end: a tuple repeats; a string primitive, an opaque scalar,
// an alias cycle that exhausts the bound and a union of no variant decide
// neither flag; an ID nothing declares and a model are left to the caller's
// schema reading.
func TestPartTypeFlags_DecidesOnlyTheKindsItReads(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.types.Register("type/opaque", &ir.Scalar{ID: "type/opaque"})
	l.types.Register("type/cycle/a", &ir.Scalar{
		ID: "type/cycle/a", Base: &ir.TypeRef{Target: "type/cycle/b"},
	})
	l.types.Register("type/cycle/b", &ir.Scalar{
		ID: "type/cycle/b", Base: &ir.TypeRef{Target: "type/cycle/a"},
	})
	l.types.Register("type/model", &ir.Model{ID: "type/model"})
	l.types.Register("type/tuple", &ir.Tuple{ID: "type/tuple"})
	l.types.Register("type/union", &ir.Union{ID: "type/union"})

	cases := []struct {
		target  ir.TypeID
		multi   bool
		decided bool
	}{
		{"type/absent", false, false},
		{"type/opaque", false, true},
		{"type/model", false, false},
		{"type/union", false, true},
		{"type/tuple", true, true},
		{"type/cycle/a", false, true},
		{l.types.PrimID(ir.PrimitiveString), false, true},
	}
	for _, tc := range cases {
		multi, file, decided := partTypeFlags(newPartUnions(), l.types.Node, ir.TypeRef{Target: tc.target}, true /* multipart */)
		assert.Equal(t, tc.multi, multi, "%s multi", tc.target)
		assert.False(t, file, "%s is not bytes", tc.target)
		assert.Equal(t, tc.decided, decided, "%s decided", tc.target)
	}
}

// TestPartTypeFlags_AListOrTupleOfRawBytesIsFiles holds a repeated part's
// Filename to its elements: bytes, through the aliases a hoisted position puts
// over them, one naming only a media type among them, are files in a multipart
// body, a tuple's only where every element it admits is, and neither a form
// body's nor a base64 element, nor a model, nor an element that names no node,
// is read as one.
func TestPartTypeFlags_AListOrTupleOfRawBytesIsFiles(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	bytes := l.types.PrimRef(ir.PrimitiveBytes)
	str := l.types.PrimRef(ir.PrimitiveString)
	l.types.Register("type/alias", &ir.Scalar{ID: "type/alias", Base: &bytes})
	l.types.Register("type/base64", &ir.Scalar{ID: "type/base64", Base: &bytes, Encoding: &ir.Encoding{Name: "base64"}})
	l.types.Register("type/png", &ir.Scalar{ID: "type/png", Base: &bytes, Encoding: &ir.Encoding{MediaType: "image/png"}})
	l.types.Register("type/opaque", &ir.Scalar{ID: "type/opaque"})
	l.types.Register("type/cycle", &ir.Scalar{ID: "type/cycle", Base: &ir.TypeRef{Target: "type/cycle"}})
	l.types.Register("type/model", &ir.Model{ID: "type/model"})
	for _, tc := range []struct {
		node      ir.TypeDef
		multipart bool
		file      bool
	}{
		{&ir.List{ID: "type/bytes", ElementType: bytes}, true, true},
		{&ir.List{ID: "type/aliases", ElementType: ir.TypeRef{Target: "type/alias"}}, true, true},
		{&ir.List{ID: "type/form", ElementType: bytes}, false, false},
		{&ir.List{ID: "type/encoded", ElementType: ir.TypeRef{Target: "type/base64"}}, true, false},
		{&ir.List{ID: "type/pngs", ElementType: ir.TypeRef{Target: "type/png"}}, true, true},
		{&ir.List{ID: "type/opaques", ElementType: ir.TypeRef{Target: "type/opaque"}}, true, false},
		{&ir.List{ID: "type/cycles", ElementType: ir.TypeRef{Target: "type/cycle"}}, true, false},
		{&ir.List{ID: "type/strings", ElementType: str}, true, false},
		{&ir.List{ID: "type/absent", ElementType: ir.TypeRef{Target: "type/nothing"}}, true, false},
		{&ir.List{ID: "type/models", ElementType: ir.TypeRef{Target: "type/model"}}, true, false},
		{&ir.Tuple{ID: "type/closed", ElementTypes: []ir.TypeRef{bytes, {Target: "type/alias"}}}, true, true},
		{&ir.Tuple{ID: "type/rest", ElementTypes: []ir.TypeRef{bytes}, Rest: &bytes}, true, true},
		{&ir.Tuple{ID: "type/restonly", Rest: &bytes}, true, true},
		{&ir.Tuple{ID: "type/stringrest", ElementTypes: []ir.TypeRef{bytes}, Rest: &str}, true, false},
		{&ir.Tuple{ID: "type/mixed", ElementTypes: []ir.TypeRef{bytes, str}}, true, false},
		{&ir.Tuple{ID: "type/empty"}, true, false},
		{&ir.Tuple{ID: "type/formtuple", ElementTypes: []ir.TypeRef{bytes}}, false, false},
	} {
		id := tc.node.Common().ID
		l.types.Register(id, tc.node)
		multi, file, decided := partTypeFlags(newPartUnions(), l.types.Node, ir.TypeRef{Target: id}, tc.multipart)
		assert.True(t, multi && decided, "%s repeats", id)
		assert.Equal(t, tc.file, file, "%s file", id)
	}
}

// TestPartTypeFlags_AUnionReadsThroughItsMembers holds a union part's flags to
// its members, through the unions nested in it: it repeats where any member is
// a list or tuple, file or not, and is a file where every member is raw bytes
// or a list of them in a multipart body, a model, a base64 value and a form's
// bytes being none. A union naming itself ends; one naming no node, or reaching
// more unions than the bound, is left to the caller's schema reading.
func TestPartTypeFlags_AUnionReadsThroughItsMembers(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	bytes := l.types.PrimRef(ir.PrimitiveBytes)
	str := l.types.PrimRef(ir.PrimitiveString)
	files, strs := ir.TypeRef{Target: "type/files"}, ir.TypeRef{Target: "type/strings"}
	l.types.Register("type/files", &ir.List{ID: "type/files", ElementType: bytes})
	l.types.Register("type/strings", &ir.List{ID: "type/strings", ElementType: str})
	l.types.Register("type/tuple", &ir.Tuple{ID: "type/tuple", ElementTypes: []ir.TypeRef{bytes}})
	l.types.Register("type/model", &ir.Model{ID: "type/model"})
	l.types.Register("type/base64", &ir.Scalar{ID: "type/base64", Base: &bytes, Encoding: &ir.Encoding{Name: "base64"}})
	l.types.Register("type/png", &ir.Scalar{ID: "type/png", Base: &bytes, Encoding: &ir.Encoding{MediaType: "image/png"}})
	l.types.Register("type/inner", unionOf("type/inner", bytes, files))
	l.types.Register("type/self", unionOf("type/self", bytes, ir.TypeRef{Target: "type/self"}))
	l.types.Register("type/diamond", unionOf("type/diamond", ir.TypeRef{Target: "type/inner"}, ir.TypeRef{Target: "type/inner"}))
	chain := func(id string, n int) ir.TypeID {
		for i := n - 1; i >= 0; i-- {
			variants := []ir.TypeRef{bytes}
			if i < n-1 {
				variants = append(variants, ir.TypeRef{Target: ir.TypeID(fmt.Sprintf("%s/%d", id, i+1))})
			}
			at := ir.TypeID(fmt.Sprintf("%s/%d", id, i))
			l.types.Register(at, unionOf(at, variants...))
		}
		return ir.TypeID(id + "/0")
	}
	for _, tc := range []struct {
		name                 string
		target               ir.TypeID
		multipart            bool
		multi, file, decided bool
	}{
		{"a file or a list of files", "type/inner", true, true, true, true},
		{"a form's file or list of files", "type/inner", false, true, false, true},
		{"a string or a list of strings", registered(l, unionOf("type/texts", str, strs)), true, true, false, true},
		{"a file or a list of strings", registered(l, unionOf("type/filetexts", bytes, strs)), true, true, false, true},
		{"a model or a list", registered(l, unionOf("type/modellist", ir.TypeRef{Target: "type/model"}, files)), true, true, false, true},
		{"files alone", registered(l, unionOf("type/bytes", bytes, bytes)), true, false, true, true},
		{"a form's files alone", "type/bytes", false, false, false, true},
		{"base64 values", registered(l, unionOf("type/base64s", ir.TypeRef{Target: "type/base64"}, bytes)), true, false, false, true},
		{"files naming their media type", registered(l, unionOf("type/pngs", ir.TypeRef{Target: "type/png"}, bytes)),
			true, false, true, true},
		{"a nested union and a file", registered(l, unionOf("type/nested", ir.TypeRef{Target: "type/inner"}, bytes)),
			true, true, true, true},
		{"a file or a tuple of files", registered(l, unionOf("type/filetuple", bytes, ir.TypeRef{Target: "type/tuple"})),
			true, true, true, true},
		{"a union naming itself", "type/self", true, false, true, true},
		{"a union nesting one naming itself", registered(l, unionOf("type/outer", ir.TypeRef{Target: "type/self"}, files)),
			true, true, true, true},
		{"a union reached twice", "type/diamond", true, true, true, true},
		{"a variant naming no node", registered(l, unionOf("type/absent", bytes, ir.TypeRef{Target: "type/nothing"})), true, false, false, false},
		{"unions up to the bound", chain("type/chain", maxPartUnions), true, false, true, true},
		{"unions past the bound", chain("type/long", maxPartUnions+1), true, false, false, false},
	} {
		multi, file, decided := partTypeFlags(newPartUnions(), l.types.Node, ir.TypeRef{Target: tc.target}, tc.multipart)
		assert.Equal(t, tc.multi, multi, "%s: multi", tc.name)
		assert.Equal(t, tc.file, file, "%s: file", tc.name)
		assert.Equal(t, tc.decided, decided, "%s: decided", tc.name)
	}
}

// unionOf returns the union id of variants.
func unionOf(id ir.TypeID, variants ...ir.TypeRef) *ir.Union {
	u := &ir.Union{ID: id}
	for _, v := range variants {
		u.Variants = append(u.Variants, ir.Variant{Type: v})
	}
	return u
}

// registered registers node with l and returns its ID.
func registered(l *lowerer, node ir.TypeDef) ir.TypeID {
	id := node.Common().ID
	l.types.Register(id, node)
	return id
}

// TestPartEncodingEmpty_ReadsEveryField holds partEncodingEmpty to every field
// ir.PartEncoding declares: each, set alone, makes the encoding one to keep. A
// field the check skipped would leave a part declaring only that field out of
// the map, and what it declared with it; reflecting over the type makes a field
// the IR adds later fail here rather than drop in silence.
func TestPartEncodingEmpty_ReadsEveryField(t *testing.T) {
	t.Parallel()
	require.True(t, partEncodingEmpty(ir.PartEncoding{}), "the zero value carries nothing")
	typ := reflect.TypeFor[ir.PartEncoding]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()
			var pe ir.PartEncoding
			v := reflect.ValueOf(&pe).Elem().Field(i)
			switch v.Kind() {
			case reflect.Bool:
				v.SetBool(true)
			case reflect.String:
				v.SetString("x")
			case reflect.Slice:
				v.Set(reflect.MakeSlice(v.Type(), 1, 1))
			case reflect.Pointer:
				v.Set(reflect.New(v.Type().Elem())) // a declared false is information too
			default:
				require.Failf(t, "unhandled field kind", "%s is a %s; teach this test to set it", f.Name, v.Kind())
			}
			assert.False(t, partEncodingEmpty(pe), "a part declaring only %s keeps its encoding", f.Name)
		})
	}
}

// TestDeclaredMulti_ReadsEveryDeclaration holds whether a part repeats, read
// off its declarations where its type does not decide it, to each
// declaration: an array repeats, and so does a union writing no type of its
// own with an array branch, and nothing else does.
func TestDeclaredMulti_ReadsEveryDeclaration(t *testing.T) {
	t.Parallel()
	l, _ := loweredFor(t, openapitest.ComponentSpec("    Bin: {type: string, format: binary}\n"+
		"    List: {type: array, items: {type: string}}\n"+
		"    Either: {oneOf: [{type: string}, {type: array, items: {type: string}}]}\n"+
		"    Typed: {type: string, oneOf: [{type: array, items: {type: string}}]}\n"))
	get := func(name string) *oas3.JSONSchema[oas3.Referenceable] {
		js, ok := l.ctx.Doc.Components.Schemas.Get(name)
		require.True(t, ok, name)
		return js
	}
	for _, tc := range []struct {
		name    string
		schemas []string
		multi   bool
	}{
		{"binary", []string{"Bin"}, false},
		{"an array beside binary", []string{"Bin", "List"}, true},
		{"a union with an array branch", []string{"Either"}, true},
		{"a union beside a type of its own", []string{"Typed"}, false},
	} {
		var schemas []*oas3.JSONSchema[oas3.Referenceable]
		for _, name := range tc.schemas {
			schemas = append(schemas, get(name))
		}
		assert.Equal(t, tc.multi, declaredMulti(l.ctx.Bindings(), schemas), tc.name)
	}
}

// TestPartWalk_DecidesKeysOnlyWhereItReadsEveryDeclaration holds whether a key
// naming none of a body's parts names no property to each keyword that can
// admit a member or declare a property the walk does not read, written alone
// beside a property, and to a body writing none of them.
func TestPartWalk_DecidesKeysOnlyWhereItReadsEveryDeclaration(t *testing.T) {
	t.Parallel()
	const base = "type: object, properties: {a: {type: string}}"
	for _, tc := range []struct {
		name, schema string
		decides      bool
	}{
		{"nothing beside", "{" + base + "}", true},
		{"an annotation, an assertion and an extension beside", "{" + base + ", description: d, minProperties: 1, x-k: 1}", true},
		{"an annotation beside a $ref", "{$ref: '#/components/schemas/Form', description: d}", true},
		{"additionalProperties false", "{" + base + ", additionalProperties: false}", true},
		{"additionalProperties", "{" + base + ", additionalProperties: {type: string}}", false},
		{"additionalProperties true", "{" + base + ", additionalProperties: true}", false},
		{"unevaluatedProperties", "{" + base + ", unevaluatedProperties: {type: string}}", false},
		{"unevaluatedProperties false", "{" + base + ", unevaluatedProperties: false}", true},
		{"patternProperties", "{" + base + ", patternProperties: {'^x': {type: string}}}", false},
		{"if", "{" + base + ", if: {required: [a]}}", false},
		{"then", "{" + base + ", then: {properties: {b: {type: string}}}}", false},
		{"else", "{" + base + ", else: {properties: {b: {type: string}}}}", false},
		{"dependentSchemas", "{" + base + ", dependentSchemas: {a: {properties: {b: {type: string}}}}}", false},
		{"$dynamicRef", "{" + base + ", $dynamicRef: '#/components/schemas/Form'}", false},
		{"properties beside a $ref", "{$ref: '#/components/schemas/Form', properties: {b: {type: string}}}", false},
		{"allOf beside a $ref", "{$ref: '#/components/schemas/Form', allOf: [{properties: {b: {type: string}}}]}", false},
		{"oneOf beside a $ref", "{$ref: '#/components/schemas/Form', oneOf: [{properties: {b: {type: string}}}]}", false},
		{"anyOf beside a $ref", "{$ref: '#/components/schemas/Form', anyOf: [{properties: {b: {type: string}}}]}", false},
		{"a catch-all beside a $ref", "{$ref: '#/components/schemas/Form', additionalProperties: true}", false},
		{"then beside a $ref", "{$ref: '#/components/schemas/Form', then: {properties: {b: {type: string}}}}", false},
		{"a bare $ref chain", "{$ref: '#/components/schemas/Hop'}", true},
		{"a $ref resolving to nothing", "{$ref: '#/components/schemas/Missing'}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, _ := loweredFor(t, openapitest.ComponentSpec("    Form: {"+base+"}\n    Hop: {$ref: '#/components/schemas/Form'}\n    Body: "+tc.schema+"\n"))
			js, ok := l.ctx.Doc.Components.Schemas.Get("Body")
			require.True(t, ok)
			assert.Equal(t, tc.decides, walkBodyParts(l.ctx.Ctx, js).decidesKeys())
		})
	}
}

// TestPartWalk_ReadsARefChainToItsBound holds the $ref chain whose sites the
// walk reads for properties written beside each $ref to maxBodyAliasHops
// sites: a chain of one fewer is read whole and decides a key, and a chain of
// as many leaves a key undecided, as a site past the bound may write one.
func TestPartWalk_ReadsARefChainToItsBound(t *testing.T) {
	t.Parallel()
	chain := func(sites int) string {
		var b strings.Builder
		for i := range sites {
			fmt.Fprintf(&b, "    L%d: {$ref: '#/components/schemas/L%d'}\n", i, i+1)
		}
		fmt.Fprintf(&b, "    L%d: {type: object, properties: {a: {type: string}}}\n", sites)
		return b.String()
	}
	for sites, decides := range map[int]bool{maxBodyAliasHops - 1: true, maxBodyAliasHops: false} {
		l, _ := loweredFor(t, openapitest.ComponentSpec(chain(sites)))
		js, ok := l.ctx.Doc.Components.Schemas.Get("L0")
		require.True(t, ok)
		w := walkBodyParts(l.ctx.Ctx, js)
		require.Len(t, w.parts, 1, "the walk reads the chain's end at %d sites", sites)
		assert.Equal(t, decides, w.decidesKeys(), "%d sites", sites)
	}
}

// TestDeclarationSchemas_PlacesEachSchemaOnce holds the schemas of a part n
// union branches declare to one for each branch's declaration, a schema
// declaring none of them to every one, and their reading to a cost linear in
// n: placing every schema for every declaration costs n², so doubling n would
// quadruple it. It is not run in parallel: AllocsPerRun measures the process.
func TestDeclarationSchemas_PlacesEachSchemaOnce(t *testing.T) {
	unplaced := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
	read := func(n int) (map[ir.PropertyID][]*oas3.JSONSchema[oas3.Referenceable], []*ir.Property, float64) {
		var b strings.Builder
		b.WriteString("    U:\n      oneOf:\n")
		for range n {
			b.WriteString("        - {type: object, properties: {file: {type: string}}}\n")
		}
		l, _ := loweredFor(t, openapitest.ComponentSpec(b.String()))
		js, ok := l.ctx.Doc.Components.Schemas.Get("U")
		require.True(t, ok)
		w := walkBodyParts(l.ctx.Ctx, js)
		require.Len(t, w.parts, 1)
		part := w.parts[0]
		require.Len(t, part.schemas, n)
		decls := make([]*ir.Property, 0, n)
		for _, s := range part.schemas {
			id, placed := declaredProp(l.ctx, s)
			require.True(t, placed)
			decls = append(decls, &ir.Property{ID: id})
		}
		cost := testing.AllocsPerRun(5, func() { declarationSchemas(l.ctx, part, decls) })
		part.schemas = append(part.schemas, unplaced)
		return declarationSchemas(l.ctx, part, decls), decls, cost
	}
	schemas, decls, few := read(50)
	for i, d := range decls {
		require.Len(t, schemas[d.ID], 2, "declaration %d", i)
		assert.NotSame(t, unplaced, schemas[d.ID][0], "declaration %d reads its own schema", i)
		assert.Same(t, unplaced, schemas[d.ID][1], "and the one no declaration is written as, after it: %d", i)
	}
	_, _, many := read(100)
	assert.Less(t, many, 3*few, "doubling the branches costs %v allocations against %v", many, few)
}
