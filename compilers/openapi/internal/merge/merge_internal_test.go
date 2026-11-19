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

// sitesAt is the one place a declaration written at pointer holds its type.
func sitesAt(pointer jsontext.Pointer) []ir.Provenance { return []ir.Provenance{ptrAt(pointer)} }

// atRoot is the sites of two declarations each written at its document's
// root, which every node of the document is written under, read by walk.
func atRoot(walk FieldDeclarationContext) declarationSites {
	return declarationSites{walk: walk, written: [2][]ir.Provenance{sitesAt(""), sitesAt("")}}
}

// judge is the type judgment over the registry stubbedValue reads, its
// declarations read against one base.
func judge(stubbedValue *stubbed) typeJudge {
	return typeJudge{resolve: stubbedValue.resolve, sites: atRoot(stubbedValue.walk)}
}

// untypedAt reports whether a node of a registry is written under /untyped,
// whose sources state no instance type and so admit every kind.
func untypedAt(provenance ir.Provenance) bool {
	return strings.HasPrefix(string(provenance.Pointer), "/untyped/")
}

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
// says they are read apart, and Admits answers untypedAt, exactly.
type stubWalk struct {
	reg      map[ir.TypeID]ir.TypeDef
	interned []jsontext.Pointer
	apart    bool
	minted   int
	brought  map[ir.TypeID][]jsontext.Pointer
}

// Mint is FieldDeclarationContext.Mint.
func (w *stubWalk) Mint(pointer jsontext.Pointer, node ir.TypeDef, brought, _ []jsontext.Pointer) ir.TypeID {
	common := node.Common()
	common.ID = composedAt(pointer)
	w.reg[common.ID] = node
	w.minted++
	if w.brought == nil {
		w.brought = map[ir.TypeID][]jsontext.Pointer{}
	}
	w.brought[common.ID] = brought
	return common.ID
}

// Interned is FieldDeclarationContext.Interned.
func (w *stubWalk) Interned(pointer jsontext.Pointer) bool {
	return slices.Contains(w.interned, pointer)
}

// Alike is FieldDeclarationContext.Alike.
func (w *stubWalk) Alike(ir.Provenance, ir.Provenance) bool { return !w.apart }

// Admits is FieldDeclarationContext.Admits: every kind, exactly, under
// /untyped.
func (w *stubWalk) Admits(at ir.Provenance, _ JSONInstanceKind) (admits, exact bool) {
	return untypedAt(at), true
}

// composedAt is the ID stubMerger mints a composition under, for the field the
// declaration at pointer names.
func composedAt(pointer jsontext.Pointer) ir.TypeID { return "type/composed" + ir.TypeID(pointer) }

// mintedProp is the ID a field the declaration at pointer names takes where
// another model carries its first declaration (identify).
func mintedProp(pointer jsontext.Pointer) ir.PropertyID {
	return ids.DocumentIDNamespace{}.ComposedProp(pointer)
}

// foldTwo folds src, rendered by source, into dst, a first declaration
// rendered as firstSource: the pair the walk hands over for one redeclared
// field.
func foldTwo(stubbedValue *stubbed, dst, src ir.Property, source DeclarationConstructRenderer) ir.Property {
	return stubbedValue.fold([]declaration{{prop: dst, source: written(firstSource)}, {prop: src, source: source}})
}

// firstSource is how foldTwo renders its first declaration.
const firstSource = `{"declared":"first"}`

// indexOver returns an index filling model, and settle, which folds what the
// index queued through stubbedValue, recording what they report, as Finish
// takes the compiler's completions once every lowering has returned.
func indexOver(model *ir.Model, stubbedValue *stubbed) (idx *ModelPropertyDeclarations, settle func()) {
	idx = NewModelPropertyDeclarations(model)
	return idx, func() {
		*stubbedValue.recorded = append(*stubbedValue.recorded, settleFlat(stubbedValue, idx, stubbedValue.Reg)...)
	}
}

// TestPropIndex_ARedeclarationFoldsOnceTheRegistryIsWhole pins what frees the
// fold from declaration order (GitHub #898): the walk lands a wire name's first
// declaration and records every later one, the model hands its folds over
// once, and the fold reads the registry it settles against, which holds nodes
// the walk had not built when it met the declarations.
func TestPropIndex_ARedeclarationFoldsOnceTheRegistryIsWhole(t *testing.T) {
	t.Parallel()
	reg := ir.TypeRegistry{"type/str": &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString}}
	model := &ir.Model{ID: "type/m"}
	idx := NewModelPropertyDeclarations(model)

	w := func(at string) ir.Property {
		return ir.Property{WireName: "w", Type: ir.TypeRef{Target: "type/str"}, Provenance: ptrAt(jsontext.Pointer("/allOf/" + at + "/properties/w"))}
	}
	firsts := make([]bool, 0, 5)
	for _, d := range []struct {
		prop   ir.Property
		source DeclarationConstructRenderer
	}{
		{redeclared("type/str", "0"), written(`{"type":"string"}`)}, {w("0"), unread(t)},
		{redeclared("type/uri", "1"), unread(t)}, {w("1"), unread(t)},
		{redeclared("type/bool", "2"), written(`{"type":"boolean"}`)},
	} {
		firsts = append(firsts, idx.MergeProperty(d.prop, d.source, ""))
	}

	assert.Equal(t, []bool{false, false, true, false, false}, firsts,
		"one settlement per model, at its first redeclaration, however many fields are redeclared")
	require.Len(t, model.Properties, 2, "a redeclaration adds no property")
	assert.Equal(t, ir.TypeID("type/str"), model.Properties[0].Type.Target, "nothing is folded before the settlement runs")
	assert.Empty(t, model.Properties[0].Unmodeled)

	reg["type/uri"] = &ir.Primitive{ID: "type/uri", PrimitiveKind: ir.PrimitiveURL}
	reg["type/bool"] = &ir.Primitive{ID: "type/bool", PrimitiveKind: ir.PrimitiveBool}
	stubbed, _ := stubMerger(reg)
	settled := settleFlat(stubbed, idx, reg)

	assert.Equal(t, ir.TypeID("type/uri"), model.Properties[0].Type.Target,
		"the narrower type wins, judged against a node the walk had not built")
	require.Len(t, settled, 1)
	assert.Equal(t, diag.ConflictingRedecl, settled[0].Code)
	assert.Equal(t, ptrAt("/allOf/2/properties/v").Pointer, settled[0].Provenance.Pointer)
	assert.Equal(t, []string{losingDeclarationKey + "/allOf/2/properties/v"}, slices.Sorted(maps.Keys(model.Properties[0].Unmodeled)),
		"the losing declaration is kept, and only it")
	assert.Equal(t, ir.TypeID("type/str"), model.Properties[1].Type.Target, "a field declared once is left as declared")
}

// TestPropIndex_AModelDeclaringEachNameOnceQueuesNothing pins the common path:
// a model whose properties are each declared once has nothing to fold, so it
// hands no settlement over.
func TestPropIndex_AModelDeclaringEachNameOnceQueuesNothing(t *testing.T) {
	t.Parallel()
	model := &ir.Model{ID: "type/m"}
	idx := NewModelPropertyDeclarations(model)

	assert.False(t, idx.MergeProperty(ir.Property{WireName: "a", Provenance: ptrAt("/properties/a")}, unread(t), ""))
	assert.False(t, idx.MergeProperty(ir.Property{WireName: "b", Provenance: ptrAt("/properties/b")}, unread(t), ""),
		"a model with no redeclaration queues no fold")

	require.Len(t, model.Properties, 2)
	assert.Equal(t, "a", model.Properties[0].WireName)
	assert.Equal(t, "b", model.Properties[1].WireName)
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
	stubbed, recorded := stubMerger(nil)
	model := &ir.Model{}
	idx, settle := indexOver(model, stubbed)

	idx.MergeProperty(ir.Property{WireName: "id", Required: true, Provenance: ptrAt("/p")}, unread(t), "")
	idx.MergeProperty(ir.Property{WireName: "name", Provenance: ptrAt("/p")}, unread(t), "")

	require.Len(t, model.Properties, 2, "two distinct wire names are two properties")
	assert.Equal(t, 0, idx.byWire["id"].pos)
	assert.Equal(t, 1, idx.byWire["name"].pos)

	idx.MergeProperty(ir.Property{WireName: "id", Secret: true, Provenance: ptrAt("/other")}, unread(t), "")
	settle()

	require.Len(t, model.Properties, 2, "a redeclaration folds rather than appending")
	assert.True(t, model.Properties[0].Required, "required is kept from the first declaration")
	assert.True(t, model.Properties[0].Secret, "and OR-ed with the redeclaration's")
	assert.Empty(t, *recorded, "agreeing declarations are not a conflict")
}
