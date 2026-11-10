// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// ptrAt is the provenance a declaration at pointer carries. MergeProperty reads
// the position off the property rather than taking it alongside, so a test that
// merges one has to say where it was written.
func ptrAt(pointer jsontext.Pointer) ir.Provenance { return ir.Provenance{Pointer: pointer} }

// unread is the source of a redeclaration the merge has no reason to keep: it
// fails the test if rendered, which is how a case asserts that nothing of the
// redeclaration was lost.
func unread(t *testing.T) func() (ir.RawValue, error) {
	return func() (ir.RawValue, error) {
		t.Helper()
		t.Fatal("the redeclaration's source was rendered, so the merge believes it lost something")
		return nil, nil
	}
}

// written is the source of a redeclaration spelled as the given JSON.
func written(source string) func() (ir.RawValue, error) {
	return func() (ir.RawValue, error) { return ir.RawValue(source), nil }
}

// declaring returns a model declaring one property, named for it, which a test
// takes where any model will do: a model declaring nothing says nothing but
// object (saysOnlyObject), and two declaring the same are alike
// (typeJudge.models).
func declaring(id ir.TypeID) *ir.Model {
	return &ir.Model{ID: id, Properties: []ir.Property{{WireName: string(id)}}}
}

// judge is the type judgment over the registry g reads, its declarations read
// against one base.
func judge(g *stubbed) typeJudge {
	return typeJudge{resolve: g.resolve, sites: declarationSites{walk: g.walk}}
}

// untypedAt is Walk.Untyped for a registry whose nodes written under /untyped
// state no instance type.
func untypedAt(p ir.Provenance) bool { return strings.HasPrefix(string(p.Pointer), "/untyped/") }

// stubbed is a Merger over a plain map and a stub walk, which records what its
// folds report, so the conflict lattice can be driven with no lowerer, parser
// or document in the setup. Its positions are placed in source 0.
type stubbed struct {
	Merger
	walk     *stubWalk
	recorded *[]ir.Diagnostic
}

// fold is Merger.fold, recording what it reports.
func (s *stubbed) fold(decls []declaration) ir.Property {
	merged, diags := s.Merger.fold(decls)
	*s.recorded = append(*s.recorded, diags...)
	return merged
}

// stubMerger returns a stubbed Merger over reg, and what its folds report.
func stubMerger(reg map[ir.TypeID]ir.TypeDef) (*stubbed, *[]ir.Diagnostic) {
	walk := &stubWalk{reg: reg}
	recorded := &[]ir.Diagnostic{}
	return &stubbed{Reg: reg, Walk: walk, walk: walk, recorded: recorded}, recorded
}

// stubWalk is the walk a test folds under. Mint registers a node in reg under
// composedAt's ID and counts it in minted, Interned answers for the schemas
// interned lists, Alike reads every two sites against one base unless apart
// says they are read apart, and Untyped is untypedAt.
type stubWalk struct {
	reg      map[ir.TypeID]ir.TypeDef
	interned []jsontext.Pointer
	apart    bool
	minted   int
}

// Mint is Walk.Mint.
func (w *stubWalk) Mint(pointer jsontext.Pointer, node ir.TypeDef) ir.TypeID {
	common := node.Common()
	common.ID = composedAt(pointer)
	w.reg[common.ID] = node
	w.minted++
	return common.ID
}

// Interned is Walk.Interned.
func (w *stubWalk) Interned(pointer jsontext.Pointer) bool {
	return slices.Contains(w.interned, pointer)
}

// Alike is Walk.Alike.
func (w *stubWalk) Alike(ir.Provenance, ir.Provenance) bool { return !w.apart }

// Untyped is Walk.Untyped.
func (w *stubWalk) Untyped(at ir.Provenance) bool { return untypedAt(at) }

// composedAt is the ID stubMerger mints a composition under, for the field the
// declaration at pointer names.
func composedAt(pointer jsontext.Pointer) ir.TypeID { return "t/composed" + ir.TypeID(pointer) }

// mintedProp is the ID a field the declaration at pointer names takes where
// another model carries its first declaration (identify).
func mintedProp(pointer jsontext.Pointer) ir.PropID { return ids.Document{}.ComposedProp(pointer) }

// foldTwo folds src, rendered by source, into dst, a first declaration with
// no source recorded: the pair the walk hands over for one redeclared field.
func foldTwo(g *stubbed, dst, src ir.Property, source Source) ir.Property {
	return g.fold([]declaration{{prop: dst}, {prop: src, source: source}})
}

// indexOver returns an index filling m, and settle, which folds what the index
// queued through g, recording what they report, as Finish takes the compiler's
// completions once every lowering has returned.
func indexOver(m *ir.Model, g *stubbed) (idx *PropIndex, settle func()) {
	idx = NewPropIndex(m)
	return idx, func() {
		*g.recorded = append(*g.recorded, idx.Fold(g.Merger)...)
	}
}

// TestPropIndex_ARedeclarationFoldsOnceTheRegistryIsWhole pins what frees the
// fold from declaration order (GitHub #898): the walk lands a wire name's first
// declaration and records every later one, the model hands its folds over
// once, and the fold reads the registry it settles against, which holds nodes
// the walk had not built when it met the declarations.
func TestPropIndex_ARedeclarationFoldsOnceTheRegistryIsWhole(t *testing.T) {
	t.Parallel()
	reg := ir.TypeRegistry{"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString}}
	m := &ir.Model{ID: "t/m"}
	idx := NewPropIndex(m)

	w := func(at string) ir.Property {
		return ir.Property{WireName: "w", Type: ir.TypeRef{Target: "t/str"}, Provenance: ptrAt(jsontext.Pointer("/allOf/" + at + "/properties/w"))}
	}
	firsts := make([]bool, 0, 5)
	for _, d := range []struct {
		prop   ir.Property
		source Source
	}{
		{redeclared("t/str", "0"), written(`{"type":"string"}`)}, {w("0"), unread(t)},
		{redeclared("t/uri", "1"), unread(t)}, {w("1"), unread(t)},
		{redeclared("t/bool", "2"), written(`{"type":"boolean"}`)},
	} {
		firsts = append(firsts, idx.MergeProperty(d.prop, d.source, ""))
	}

	assert.Equal(t, []bool{false, false, true, false, false}, firsts,
		"one settlement per model, at its first redeclaration, however many fields are redeclared")
	require.Len(t, m.Properties, 2, "a redeclaration adds no property")
	assert.Equal(t, ir.TypeID("t/str"), m.Properties[0].Type.Target, "nothing is folded before the settlement runs")
	assert.Empty(t, m.Properties[0].Unmodeled)

	reg["t/uri"] = &ir.Primitive{ID: "t/uri", Prim: ir.PrimURL}
	reg["t/bool"] = &ir.Primitive{ID: "t/bool", Prim: ir.PrimBool}
	g, _ := stubMerger(reg)
	settled := idx.Fold(g.Merger)

	assert.Equal(t, ir.TypeID("t/uri"), m.Properties[0].Type.Target,
		"the narrower type wins, judged against a node the walk had not built")
	require.Len(t, settled, 1)
	assert.Equal(t, diag.ConflictingRedecl, settled[0].Code)
	assert.Equal(t, ptrAt("/allOf/2/properties/v").Pointer, settled[0].Provenance.Pointer)
	assert.Equal(t, []string{losingDeclarationKey + "/allOf/2/properties/v"}, slices.Sorted(maps.Keys(m.Properties[0].Unmodeled)),
		"the losing declaration is kept, and only it")
	assert.Equal(t, ir.TypeID("t/str"), m.Properties[1].Type.Target, "a field declared once is left as declared")
}

// TestPropIndex_AModelDeclaringEachNameOnceQueuesNothing pins the common path:
// a model whose properties are each declared once has nothing to fold, so it
// hands no settlement over.
func TestPropIndex_AModelDeclaringEachNameOnceQueuesNothing(t *testing.T) {
	t.Parallel()
	m := &ir.Model{ID: "t/m"}
	idx := NewPropIndex(m)

	assert.False(t, idx.MergeProperty(ir.Property{WireName: "a", Provenance: ptrAt("/properties/a")}, unread(t), ""))
	assert.False(t, idx.MergeProperty(ir.Property{WireName: "b", Provenance: ptrAt("/properties/b")}, unread(t), ""),
		"a model with no redeclaration queues no fold")

	require.Len(t, m.Properties, 2)
	assert.Equal(t, "a", m.Properties[0].WireName)
	assert.Equal(t, "b", m.Properties[1].WireName)
}

// TestWireNameIndex_KeysEveryPropertyByItsWireName pins the index a
// redeclaration is found through. A property missing from it would be appended a
// second time rather than reconciled, leaving one wire name declared twice in
// one model.
func TestWireNameIndex_KeysEveryPropertyByItsWireName(t *testing.T) {
	t.Parallel()
	got := WireNameIndex([]ir.Property{{WireName: "id"}, {WireName: "name"}, {WireName: "age"}})

	assert.Equal(t, map[string]int{"id": 0, "name": 1, "age": 2}, got)
	assert.Empty(t, WireNameIndex(nil), "no properties index to nothing")
}

// TestMergeProperty_AppendsThenFolds pins the two outcomes the entry point
// chooses between: a wire name not yet present is appended and indexed, and one
// already present is folded into the property that holds it rather than
// appended beside it.
func TestMergeProperty_AppendsThenFolds(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(nil)
	m := &ir.Model{}
	idx, settle := indexOver(m, g)

	idx.MergeProperty(ir.Property{WireName: "id", Required: true, Provenance: ptrAt("/p")}, unread(t), "")
	idx.MergeProperty(ir.Property{WireName: "name", Provenance: ptrAt("/p")}, unread(t), "")

	require.Len(t, m.Properties, 2, "two distinct wire names are two properties")
	assert.Equal(t, 0, idx.byWire["id"].pos)
	assert.Equal(t, 1, idx.byWire["name"].pos)

	idx.MergeProperty(ir.Property{WireName: "id", Secret: true, Provenance: ptrAt("/other")}, unread(t), "")
	settle()

	require.Len(t, m.Properties, 2, "a redeclaration folds rather than appending")
	assert.True(t, m.Properties[0].Required, "required is kept from the first declaration")
	assert.True(t, m.Properties[0].Secret, "and OR-ed with the redeclaration's")
	assert.Empty(t, *recorded, "agreeing declarations are not a conflict")
}
