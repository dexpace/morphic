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
	reg["t/E"] = &ir.Enum{ID: "t/E", Anonymous: true, Provenance: ptrAt("/E"), ValueType: ir.PrimString, Sensitive: true, Closed: true,
		Docs: ir.Docs{Description: "e"}, Unmodeled: kept("openapi:x-e", "1"),
		Members: []ir.EnumMember{{ID: "e/E/s:a", Value: str("a")}, {ID: "e/E/s:bb", Value: str("bb")}}}
	reg["t/C"] = &ir.Literal{ID: "t/C", Value: str("abc")}
	reg["t/OE"] = &ir.Enum{ID: "t/OE", ValueType: ir.PrimString, Members: []ir.EnumMember{{ID: "e/OE/s:a", Value: str("a")}}}
	reg["t/S"] = &ir.Scalar{ID: "t/S", Base: &ir.TypeRef{Target: "t/prim/string"}, Docs: ir.Docs{Description: "s"},
		Constraints: &ir.Constraints{MaxLength: new(int64(5))}}
	reg["t/L"] = &ir.List{ID: "t/L", Elem: primRef(ir.PrimInt32), Constraints: &ir.Constraints{MinItems: new(int64(1))}}
	reg["t/M"] = &ir.MapT{ID: "t/M", Value: primRef(ir.PrimInt32)}
	reg["t/T"] = &ir.Tuple{ID: "t/T", Elems: []ir.TypeRef{primRef(ir.PrimInt32)}}
	reg["t/Model"] = declaring("t/Model")
	reg["t/Ref"] = &ir.List{ID: "t/Ref", Elem: primRef(ir.PrimInt32),
		Unmodeled: kept("openapi:not", `{"$ref":"n.json"}`)}
	alias("t/E-docs", "t/E", ir.TypeCommon{Docs: ir.Docs{Description: "near"}, Unmodeled: kept("openapi:x-near", "2")}, nil)
	alias("t/E-same-key", "t/E", ir.TypeCommon{Unmodeled: kept("openapi:x-e", "3")}, nil)
	alias("t/E-over-docs", "t/E-docs", described("nearest"), nil)
	alias("t/E-max", "t/E", ir.TypeCommon{}, &ir.Constraints{MaxLength: new(int64(2))})
	alias("t/E-cut", "t/E", ir.TypeCommon{}, &ir.Constraints{MaxLength: new(int64(1))})
	alias("t/E-pattern", "t/E", ir.TypeCommon{}, &ir.Constraints{Pattern: "^a"})
	alias("t/OE-max", "t/OE", ir.TypeCommon{}, &ir.Constraints{MaxLength: new(int64(2))})
	alias("t/OE-docs", "t/OE", described("near"), nil)
	alias("t/E-docs-title", "t/E-docs", ir.TypeCommon{Docs: ir.Docs{Summary: "title", ExternalDocs: []ir.Link{{URL: "u"}}}}, nil)
	alias("t/C-docs", "t/C", described("near"), nil)
	alias("t/C-max", "t/C", ir.TypeCommon{}, &ir.Constraints{MaxLength: new(int64(3))})
	alias("t/C-cut", "t/C", ir.TypeCommon{}, &ir.Constraints{MaxLength: new(int64(2))})
	alias("t/S-min", "t/S", ir.TypeCommon{}, &ir.Constraints{MinLength: new(int64(2))})
	alias("t/S-pattern", "t/S", ir.TypeCommon{}, &ir.Constraints{Pattern: "^a"})
	alias("t/S-pattern-b", "t/S-pattern", ir.TypeCommon{}, &ir.Constraints{Pattern: "^b"})
	alias("t/L-max", "t/L", ir.TypeCommon{}, &ir.Constraints{MaxItems: new(int64(3))})
	alias("t/L-max-docs", "t/L-max", described("near"), &ir.Constraints{MinItems: new(int64(2))})
	alias("t/L-kept", "t/L", ir.TypeCommon{Unmodeled: kept("openapi:x-k", "1")}, nil)
	alias("t/M-docs", "t/M", described("near"), &ir.Constraints{MinProps: new(int64(1))})
	alias("t/T-docs", "t/T", described("near"), nil)
	alias("t/Model-docs", "t/Model", described("near"), nil)
	alias("t/Ref-docs", "t/Ref", described("near"), nil)
	alias("t/L-ref", "t/L", ir.TypeCommon{Unmodeled: kept("openapi:not", `{"$ref":"n.json"}`)}, nil)
	alias("t/gone", "t/absent", ir.TypeCommon{}, nil)
	alias("t/gone-deep", "t/gone", ir.TypeCommon{}, nil)
	reg["t/L-encoded"] = &ir.Scalar{ID: "t/L-encoded", Base: &ir.TypeRef{Target: "t/L"}, Encoding: &ir.Encoding{Name: "csv"}}
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

	e, ok := read("t/E-docs").(*ir.Enum)
	require.True(t, ok, "an alias of an enum reads as the enum")
	assert.Equal(t, ir.TypeID("t/E"), e.ID, "named as the enum its members are named beneath")
	assert.Equal(t, ptrAt("/alias/t/E-docs"), e.Provenance, "written where the alias is")
	assert.False(t, e.Anonymous, "as the alias is, not its end")
	assert.Equal(t, "near", e.Docs.Description, "the nearest description")
	assert.True(t, e.Sensitive, "a redaction the end asks for")
	assert.Equal(t, ir.Unmodeled{"openapi:x-e": {Value: ir.RawValue("1")}, "openapi:x-near": {Value: ir.RawValue("2")}},
		e.Unmodeled, "every key either keeps")
	assert.Equal(t, viewRegistry()["t/E"].(*ir.Enum).Members, e.Members)
	assert.Equal(t, ir.RawValue("1"), viewRegistry()["t/E"].Common().Unmodeled["openapi:x-e"].Value, "the end is as it was")

	same, ok := read("t/E-same-key").(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, ir.RawValue("3"), same.Unmodeled["openapi:x-e"].Value, "a key both keep is the nearest's")
	nearest, ok := read("t/E-over-docs").(*ir.Enum)
	require.True(t, ok)
	assert.Equal(t, "nearest", nearest.Docs.Description, "over a chain of two, the nearest alias's")
	assert.Equal(t, ir.Docs{Summary: "title", Description: "near", ExternalDocs: []ir.Link{{URL: "u"}}},
		read("t/E-docs-title").Common().Docs, "docs read field by field: a title over an alias's description keeps it")

	_, ok = read("t/OE-docs").(*ir.Enum)
	assert.True(t, ok, "an open enum's alias bounding nothing reads as the enum")

	_, ok = read("t/E-max").(*ir.Enum)
	assert.True(t, ok, "bounds every member meets say nothing of an enum")
	lit, ok := read("t/C-max").(*ir.Literal)
	require.True(t, ok, "nor of a const its value meets")
	assert.Equal(t, str("abc"), lit.Value)
	assert.Equal(t, "near", read("t/C-docs").Common().Docs.Description)

	s, ok := read("t/S-min").(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinLength: new(int64(2)), MaxLength: new(int64(5))}, s.Constraints, "bounds meet")
	assert.Equal(t, ir.TypeRef{Target: "t/prim/string"}, *s.Base, "an alias of a scalar reads as the scalar over its kind")
	l, ok := read("t/L-max-docs").(*ir.List)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinItems: new(int64(2)), MaxItems: new(int64(3))}, l.Constraints,
		"bounds meet down the chain")
	assert.Equal(t, "near", l.Docs.Description)
	assert.Equal(t, ir.Unmodeled{"openapi:x-k": {Value: ir.RawValue("1")}}, read("t/L-kept").Common().Unmodeled,
		"a key only the alias keeps")
	m, ok := read("t/M-docs").(*ir.MapT)
	require.True(t, ok)
	assert.Equal(t, &ir.Constraints{MinProps: new(int64(1))}, m.Constraints)
	tuple, ok := read("t/T-docs").(*ir.Tuple)
	require.True(t, ok)
	assert.Nil(t, tuple.Constraints, "a chain bounding nothing adds no bounds")

	for _, id := range []ir.TypeID{
		"t/E-cut", "t/E-pattern", "t/OE-max", "t/C-cut", "t/S-pattern-b", "t/L-encoded", "t/L-ref", "t/Ref-docs",
		"t/Model-docs", "t/gone", "t/gone-deep", "t/S", "t/E", "t/prim/string",
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
	maxDocs, _ := r.resolve("t/L-max-docs")
	maxOnly, _ := r.resolve("t/L-max")

	assert.Same(t, maxOnly, r.view(maxOnly, "t/L"), "an alias naming the stop is the node")
	v, ok := r.view(maxDocs, "t/L").(*ir.Scalar)
	require.True(t, ok, "a chain read down to the stop is an alias of it")
	assert.Equal(t, ir.TypeRef{Target: "t/L"}, *v.Base)
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
		{"two aliases of one list", []ir.TypeID{"t/L-max", "t/L-ref"}, "t/L"},
		{"a chain and an alias of its end", []ir.TypeID{"t/L-max-docs", "t/L-ref"}, "t/L"},
		{"a chain and an alias of its link", []ir.TypeID{"t/L-max-docs", "t/L-max-docs"}, "t/L-max"},
		{"three aliases of one enum", []ir.TypeID{"t/E-docs", "t/E-max", "t/E-over-docs"}, "t/E"},
		{"one alias among three of another node", []ir.TypeID{"t/E-docs", "t/E-max", "t/L-max"}, ""},
		{"aliases of two nodes", []ir.TypeID{"t/E-docs", "t/L-max"}, ""},
		{"an alias and its own end", []ir.TypeID{"t/E-docs", "t/E"}, ""},
		{"a scalar over a kind names no node", []ir.TypeID{"t/S", "t/S"}, ""},
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
		next := ir.TypeID("t/L")
		for i := range tc.aliases {
			id := ir.TypeID(fmt.Sprintf("t/link-%d", i))
			reg[id] = &ir.Scalar{ID: id, Base: &ir.TypeRef{Target: next}}
			next = id
		}
		r := registry{resolve: stubResolve(reg)}

		_, isList := r.view(reg[next], "").(*ir.List)

		assert.Equal(t, tc.read, isList, tc.name)
		assert.Equal(t, tc.read, r.standsFor(reg[next], "t/L", 0) == relANarrower, "%s: standsFor reads as far", tc.name)
	}
}

// TestStandsFor_ReadsOnlyAnAliasChain pins which types an alias stands for: each
// node its chain names, within it, but through a link admitting null, which
// adds null to it alone (the first alias's docs carried at depth 0) or more;
// and nothing for a node that is no alias, a chain naming nothing the registry
// holds, or a type the chain does not name.
func TestStandsFor_ReadsOnlyAnAliasChain(t *testing.T) {
	t.Parallel()
	reg := nullableAliases(viewRegistry())
	r := registry{resolve: stubResolve(reg)}
	for _, tc := range []struct {
		td    ir.TypeID
		id    ir.TypeID
		depth int
		want  relation
	}{
		{"t/L-max-docs", "t/L-max", 1, relANarrower},
		{"t/L-max-docs", "t/L", 1, relANarrower},
		{"t/L-max", "t/L-max-docs", 0, relUnrelated},
		{"t/L", "t/L", 0, relUnrelated},
		{"t/gone", "t/L", 0, relUnrelated},
		{"t/S", "t/prim/string", 0, relANarrower},
		{"t/L-null", "t/L", 1, relBNarrower},
		{"t/L-null-docs", "t/L", 0, relBNarrower},
		{"t/L-null-docs", "t/L", 1, relUnrelated},
		{"t/L-null-max", "t/L", 0, relUnrelated},
		{"t/L-over-null", "t/L", 0, relBNarrower},
		{"t/L-over-null", "t/L", 1, relUnrelated},
		{"t/L-over-null", "t/L-null", 1, relANarrower},
		{"t/L-over-null-docs", "t/L", 0, relUnrelated},
	} {
		assert.Equal(t, tc.want, r.standsFor(reg[tc.td], tc.id, tc.depth), "%s for %s at %d", tc.td, tc.id, tc.depth)
	}
	assert.True(t, r.nullThroughChain(reg["t/L-over-null"]), "a link past the first admits null")
	assert.False(t, r.nullThroughChain(reg["t/L-max-docs"]))
	assert.False(t, r.nullThroughChain(reg["t/L"]), "no alias")
	assert.Empty(t, cmp.Diff(nullableAliases(viewRegistry()), reg), "reading writes to no node")
}

// nullableAliases adds to reg aliases of t/L whose chains admit null: one adding
// null alone, one adding docs, one adding a bound, and one over each of the
// first two.
func nullableAliases(reg map[ir.TypeID]ir.TypeDef) map[ir.TypeID]ir.TypeDef {
	nullable := func(id ir.TypeID, c ir.TypeCommon, bounds *ir.Constraints) {
		c.ID = id
		reg[id] = &ir.Scalar{TypeCommon: c, Base: &ir.TypeRef{Target: "t/L", Nullable: true}, Constraints: bounds}
	}
	nullable("t/L-null", ir.TypeCommon{}, nil)
	nullable("t/L-null-docs", ir.TypeCommon{Docs: ir.Docs{Description: "near"}}, nil)
	nullable("t/L-null-max", ir.TypeCommon{}, &ir.Constraints{MaxItems: new(int64(3))})
	reg["t/L-over-null"] = &ir.Scalar{ID: "t/L-over-null", Base: &ir.TypeRef{Target: "t/L-null"},
		Docs: ir.Docs{Description: "over"}}
	reg["t/L-over-null-docs"] = &ir.Scalar{ID: "t/L-over-null-docs", Base: &ir.TypeRef{Target: "t/L-null-docs"}}
	return reg
}
