// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// viewRegistry holds the nodes the view tests read through: an enum, a const,
// a scalar, a list, a map, a tuple and a model, each with aliases over it.
func viewRegistry() map[ir.TypeID]ir.TypeDef {
	reg := narrowingRegistry()
	alias := func(id, base ir.TypeID, c ir.TypeCommon, bounds *ir.Constraints) {
		c.ID, c.Provenance = id, ptrAt("/alias/"+jsontext.Pointer(id))
		reg[id] = &ir.Scalar{TypeCommon: c, Base: &ir.TypeRef{Target: base}, Constraints: bounds}
	}
	described := func(d string) ir.TypeCommon { return ir.TypeCommon{Docs: ir.Docs{Description: d}} }
	kept := func(key, v string) ir.Unmodeled { return ir.Unmodeled{key: {Value: ir.RawValue(v)}} }
	reg["type/E"] = &ir.Enum{ID: "type/E", Anonymous: true, Provenance: ptrAt("/E"), ValueType: ir.PrimitiveString, Sensitive: true, Closed: true,
		Docs: ir.Docs{Description: "e"}, Unmodeled: kept("openapi:x-e", "1"),
		Members: []ir.EnumMember{{ID: "enum-member/E/string:a", Value: str("a")}, {ID: "enum-member/E/string:bb", Value: str("bb")}}}
	reg["type/C"] = &ir.Literal{ID: "type/C", Value: str("abc")}
	reg["type/OE"] = &ir.Enum{ID: "type/OE", ValueType: ir.PrimitiveString, Members: []ir.EnumMember{{ID: "enum-member/OE/string:a", Value: str("a")}}}
	reg["type/S"] = &ir.Scalar{ID: "type/S", Base: &ir.TypeRef{Target: "type/primitive/string"}, Docs: ir.Docs{Description: "s"},
		Constraints: &ir.Constraints{MaxLength: new(int64(5))}}
	reg["type/L"] = &ir.List{ID: "type/L", ElementType: primRef(ir.PrimitiveInt32), Constraints: &ir.Constraints{MinItems: new(int64(1))}}
	reg["type/M"] = &ir.MapT{ID: "type/M", Value: primRef(ir.PrimitiveInt32)}
	reg["type/T"] = &ir.Tuple{ID: "type/T", ElementTypes: []ir.TypeRef{primRef(ir.PrimitiveInt32)}}
	reg["type/Model"] = declaring("type/Model")
	reg["type/Ref"] = &ir.List{ID: "type/Ref", ElementType: primRef(ir.PrimitiveInt32),
		Unmodeled: kept("openapi:not", `{"$ref":"n.json"}`)}
	alias("type/E-docs", "type/E", ir.TypeCommon{Docs: ir.Docs{Description: "near"}, Unmodeled: kept("openapi:x-near", "2")}, nil)
	alias("type/E-same-key", "type/E", ir.TypeCommon{Unmodeled: kept("openapi:x-e", "3")}, nil)
	alias("type/E-over-docs", "type/E-docs", described("nearest"), nil)
	alias("type/E-max", "type/E", ir.TypeCommon{}, &ir.Constraints{MaxLength: new(int64(2))})
	alias("type/E-cut", "type/E", ir.TypeCommon{}, &ir.Constraints{MaxLength: new(int64(1))})
	alias("type/E-pattern", "type/E", ir.TypeCommon{}, &ir.Constraints{Pattern: "^a"})
	alias("type/OE-max", "type/OE", ir.TypeCommon{}, &ir.Constraints{MaxLength: new(int64(2))})
	alias("type/OE-docs", "type/OE", described("near"), nil)
	alias("type/E-docs-title", "type/E-docs", ir.TypeCommon{Docs: ir.Docs{Summary: "title", ExternalDocs: []ir.Link{{URL: "u"}}}}, nil)
	alias("type/C-docs", "type/C", described("near"), nil)
	alias("type/C-max", "type/C", ir.TypeCommon{}, &ir.Constraints{MaxLength: new(int64(3))})
	alias("type/C-cut", "type/C", ir.TypeCommon{}, &ir.Constraints{MaxLength: new(int64(2))})
	alias("type/S-min", "type/S", ir.TypeCommon{}, &ir.Constraints{MinLength: new(int64(2))})
	alias("type/S-pattern", "type/S", ir.TypeCommon{}, &ir.Constraints{Pattern: "^a"})
	alias("type/S-pattern-b", "type/S-pattern", ir.TypeCommon{}, &ir.Constraints{Pattern: "^b"})
	alias("type/L-max", "type/L", ir.TypeCommon{}, &ir.Constraints{MaxItems: new(int64(3))})
	alias("type/L-max-docs", "type/L-max", described("near"), &ir.Constraints{MinItems: new(int64(2))})
	alias("type/L-kept", "type/L", ir.TypeCommon{Unmodeled: kept("openapi:x-k", "1")}, nil)
	alias("type/M-docs", "type/M", described("near"), &ir.Constraints{MinProperties: new(int64(1))})
	alias("type/T-docs", "type/T", described("near"), nil)
	alias("type/Model-docs", "type/Model", described("near"), nil)
	alias("type/Ref-docs", "type/Ref", described("near"), nil)
	alias("type/L-ref", "type/L", ir.TypeCommon{Unmodeled: kept("openapi:not", `{"$ref":"n.json"}`)}, nil)
	alias("type/gone", "type/absent", ir.TypeCommon{}, nil)
	alias("type/gone-deep", "type/gone", ir.TypeCommon{}, nil)
	reg["type/L-encoded"] = &ir.Scalar{ID: "type/L-encoded", Base: &ir.TypeRef{Target: "type/L"}, Encoding: &ir.Encoding{Name: "csv"}}
	return reg
}

// TestView_ReadsAnAliasAsTheNodeItStandsFor pins what the judgment reads an
// alias chain as: the node it ends in, holding the bounds the chain and the node
// say together and, field by field, the detail the nearest node sets, at the
// first alias's site but for the ID, the end's, beneath which an enum's members
// are named. A chain the view cannot read so stands for itself.
func TestView_ReadsAnAliasAsTheNodeItStandsFor(t *testing.T) {
	t.Parallel()
	r := registry{resolve: stubResolve(viewRegistry())}
	read := func(id ir.TypeID) ir.TypeDef {
		td, ok := r.resolve(id)
		require.True(t, ok, id)
		return r.view(td, "")
	}

	e, ok := read("type/E-docs").(*ir.Enum)
	require.True(t, ok, "an alias of an enum reads as the enum")
	assert.Equal(t, ir.TypeID("type/E"), e.ID, "named as the enum its members are named beneath")
	assert.Equal(t, ptrAt("/alias/type/E-docs"), e.Provenance, "written where the alias is")
	assert.False(t, e.Anonymous, "as the alias is, not its end")
	assert.Equal(t, "near", e.Docs.Description, "the nearest description")
	assert.True(t, e.Sensitive, "a redaction the end asks for")
	assert.Equal(t, ir.Unmodeled{"openapi:x-e": {Value: ir.RawValue("1")}, "openapi:x-near": {Value: ir.RawValue("2")}},
		e.Unmodeled, "every key either keeps")
	assert.Equal(t, viewRegistry()["type/E"].(*ir.Enum).Members, e.Members)
	assert.Equal(t, ir.RawValue("1"), viewRegistry()["type/E"].Common().Unmodeled["openapi:x-e"].Value, "the end is as it was")

	same, ok := read("type/E-same-key").(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.RawValue("3"), same.Unmodeled["openapi:x-e"].Value, "a key both keep is the nearest's")
	nearest, ok := read("type/E-over-docs").(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, "nearest", nearest.Docs.Description, "over a chain of two, the nearest alias's")
	assert.Equal(t, ir.Docs{Summary: "title", Description: "near", ExternalDocs: []ir.Link{{URL: "u"}}},
		read("type/E-docs-title").Common().Docs, "docs read field by field: a title over an alias's description keeps it")

	_, ok = read("type/OE-docs").(*ir.Enum)
	assert.True(t, ok, "an open enum's alias bounding nothing reads as the enum")

	_, ok = read("type/E-max").(*ir.Enum)
	assert.True(t, ok, "bounds every member meets say nothing of an enum")
	lit, ok := read("type/C-max").(*ir.Literal)
	require.True(t, ok, "nor of a const its value meets")
	assert.Equal(t, str("abc"), lit.Value)
	assert.Equal(t, "near", read("type/C-docs").Common().Docs.Description)

	s, ok := read("type/S-min").(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinLength: new(int64(2)), MaxLength: new(int64(5))}, s.Constraints, "bounds meet")
	assert.Equal(t, ir.TypeRef{Target: "type/primitive/string"}, *s.Base, "an alias of a scalar reads as the scalar over its kind")
	l, ok := read("type/L-max-docs").(*ir.List)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinItems: new(int64(2)), MaxItems: new(int64(3))}, l.Constraints,
		"bounds meet down the chain")
	assert.Equal(t, "near", l.Docs.Description)
	assert.Equal(t, ir.Unmodeled{"openapi:x-k": {Value: ir.RawValue("1")}}, read("type/L-kept").Common().Unmodeled,
		"a key only the alias keeps")
	m, ok := read("type/M-docs").(*ir.MapT)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinProperties: new(int64(1))}, m.Constraints)
	tuple, ok := read("type/T-docs").(*ir.Tuple)
	require.True(t, ok)
	assert.Nil(t, tuple.Constraints, "a chain bounding nothing adds no bounds")

	for _, id := range []ir.TypeID{
		"type/E-cut", "type/E-pattern", "type/OE-max", "type/C-cut", "type/S-pattern-b", "type/L-encoded", "type/L-ref", "type/Ref-docs",
		"type/Model-docs", "type/gone", "type/gone-deep", "type/S", "type/E", "type/primitive/string",
	} {
		td, _ := r.resolve(id)
		assert.Same(t, td, read(id), "%s stands for itself", id)
	}
}

// TestView_StopsAtTheNodeTheChainsShare pins the view read down to a node:
// an alias naming it is the node, and a chain past it reads to its own end.
func TestView_StopsAtTheNodeTheChainsShare(t *testing.T) {
	t.Parallel()
	r := registry{resolve: stubResolve(viewRegistry())}
	maxDocs, _ := r.resolve("type/L-max-docs")
	maxOnly, _ := r.resolve("type/L-max")

	assert.Same(t, maxOnly, r.view(maxOnly, "type/L"), "an alias naming the stop is the node")
	v, ok := r.view(maxDocs, "type/L").(*ir.Scalar)
	require.True(t, ok, "a chain read down to the stop is an alias of it")
	assert.Equal(t, ir.TypeRef{Target: "type/L"}, *v.Base)
	assert.Equal(t, &ir.Constraints{MinItems: new(int64(2)), MaxItems: new(int64(3))}, v.Constraints)
	assert.Equal(t, "near", v.Docs.Description)
}

// TestSharedLink_NamesTheFirstNodeEveryChainNames pins where aliases stop being
// read through: at the first node every chain names, and nowhere where one names
// none, or no node all of them name.
func TestSharedLink_NamesTheFirstNodeEveryChainNames(t *testing.T) {
	t.Parallel()
	r := registry{resolve: stubResolve(viewRegistry())}
	node := func(id ir.TypeID) ir.TypeDef { td, _ := r.resolve(id); return td }
	for _, tc := range []struct {
		name string
		ids  []ir.TypeID
		want ir.TypeID
	}{
		{"two aliases of one list", []ir.TypeID{"type/L-max", "type/L-ref"}, "type/L"},
		{"a chain and an alias of its end", []ir.TypeID{"type/L-max-docs", "type/L-ref"}, "type/L"},
		{"a chain and an alias of its link", []ir.TypeID{"type/L-max-docs", "type/L-max-docs"}, "type/L-max"},
		{"three aliases of one enum", []ir.TypeID{"type/E-docs", "type/E-max", "type/E-over-docs"}, "type/E"},
		{"one alias among three of another node", []ir.TypeID{"type/E-docs", "type/E-max", "type/L-max"}, ""},
		{"aliases of two nodes", []ir.TypeID{"type/E-docs", "type/L-max"}, ""},
		{"an alias and its own end", []ir.TypeID{"type/E-docs", "type/E"}, ""},
		{"a scalar over a kind names no node", []ir.TypeID{"type/S", "type/S"}, ""},
	} {
		tds := make([]ir.TypeDef, len(tc.ids))
		for k, id := range tc.ids {
			tds[k] = node(id)
		}
		assert.Equal(t, tc.want, r.sharedLink(tds...), tc.name)
	}
}

// TestAliasedNode_ReadsAChainUpToItsBound pins maxTypeResolveDepth on the view:
// a chain of that many aliases is read to the node it ends in, and one more
// reads as no chain, so the alias stands for itself.
func TestAliasedNode_ReadsAChainUpToItsBound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		aliases int
		read    bool
	}{
		{"at the bound", maxTypeResolveDepth, true},
		{"past the bound", maxTypeResolveDepth + 1, false},
	} {
		reg := viewRegistry()
		next := ir.TypeID("type/L")
		for i := range tc.aliases {
			id := ir.TypeID(fmt.Sprintf("type/link-%d", i))
			reg[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: next}}
			next = id
		}
		r := registry{resolve: stubResolve(reg)}

		_, isList := r.view(reg[next], "").(*ir.List)

		assert.Equal(t, tc.read, isList, tc.name)
		assert.Equal(t, tc.read, r.standsFor(reg[next], "type/L") == firstTypeIsNarrower, "%s: standsFor reads as far", tc.name)
	}
}

// TestStandsFor_ReadsOnlyAnAliasChain pins which types an alias stands for: each
// node its chain names, within it, but through a link admitting null, which
// adds null to it alone or more, docs counted; and nothing for a node that is
// no alias, a chain naming nothing the registry holds, or a type the chain does
// not name.
func TestStandsFor_ReadsOnlyAnAliasChain(t *testing.T) {
	t.Parallel()
	reg := nullableAliases(viewRegistry())
	r := registry{resolve: stubResolve(reg)}
	for _, tc := range []struct {
		td   ir.TypeID
		id   ir.TypeID
		want relation
	}{
		{"type/L-max-docs", "type/L-max", firstTypeIsNarrower},
		{"type/L-max-docs", "type/L", firstTypeIsNarrower},
		{"type/L-max", "type/L-max-docs", relUnrelated},
		{"type/L", "type/L", relUnrelated},
		{"type/gone", "type/L", relUnrelated},
		{"type/S", "type/primitive/string", firstTypeIsNarrower},
		{"type/L-null", "type/L", secondTypeIsNarrower},
		{"type/L-null-docs", "type/L", relUnrelated},
		{"type/L-null-max", "type/L", relUnrelated},
		{"type/L-over-null", "type/L", relUnrelated},
		{"type/L-over-null", "type/L-null", firstTypeIsNarrower},
		{"type/L-over-null-docs", "type/L", relUnrelated},
		{"type/L-over-null-bare", "type/L", secondTypeIsNarrower},
	} {
		assert.Equal(t, tc.want, r.standsFor(reg[tc.td], tc.id), "%s for %s", tc.td, tc.id)
	}
	assert.True(t, r.nullThroughChain(reg["type/L-over-null"]), "a link past the first admits null")
	assert.False(t, r.nullThroughChain(reg["type/L-max-docs"]))
	assert.False(t, r.nullThroughChain(reg["type/L"]), "no alias")
	assert.Empty(t, cmp.Diff(nullableAliases(viewRegistry()), reg), "reading writes to no node")
}

// nullableAliases adds to reg aliases of t/L whose chains admit null: one adding
// null alone, one adding docs, one adding a bound, and over the first two, one
// holding docs and one holding nothing.
func nullableAliases(reg map[ir.TypeID]ir.TypeDef) map[ir.TypeID]ir.TypeDef {
	nullable := func(id ir.TypeID, c ir.TypeCommon, bounds *ir.Constraints) {
		c.ID = id
		reg[id] = &ir.Scalar{TypeCommon: c, Base: &ir.TypeRef{Target: "type/L", Nullable: true}, Constraints: bounds}
	}
	nullable("type/L-null", ir.TypeCommon{}, nil)
	nullable("type/L-null-docs", ir.TypeCommon{Docs: ir.Docs{Description: "near"}}, nil)
	nullable("type/L-null-max", ir.TypeCommon{}, &ir.Constraints{MaxItems: new(int64(3))})
	reg["type/L-over-null"] = &ir.Scalar{ID: "type/L-over-null", Base: &ir.TypeRef{Target: "type/L-null"},
		Docs: ir.Docs{Description: "over"}}
	reg["type/L-over-null-docs"] = &ir.Scalar{ID: "type/L-over-null-docs", Base: &ir.TypeRef{Target: "type/L-null-docs"}}
	reg["type/L-over-null-bare"] = &ir.Scalar{ID: "type/L-over-null-bare", Base: &ir.TypeRef{Target: "type/L-null"}}
	return reg
}
