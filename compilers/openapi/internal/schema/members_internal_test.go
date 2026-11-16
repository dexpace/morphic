// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/internal/ctxtest"
	"github.com/dexpace/morphic/ir"
)

// settledUnder lowers schemas, components of a 3.1 document, with the run's
// settlement reading at most limit declarations, and returns the document, what
// it reports and the declarations the settlement read.
func settledUnder(t *testing.T, schemas string, limit budget.Limit) (*ir.Document, []ir.Diagnostic, int) {
	t.Helper()
	l, diags := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", schemas))
	membersOf(l.Ctx).reads = budget.New(limit)
	l.Diags.AppendAll(LowerComponentSchemas(t.Context(), l.Ctx, l.Types, l.Schemas()))
	doc := l.Document(t)
	return doc, append(diags, l.Diags.List()...), membersOf(l.Ctx).reads.Spent()
}

// hierarchy is n models, each composing the one before it and, where wide, the
// first too, each redeclaring x, which the first declares an integer, as a
// number, and declaring a member of its own: a name no other model declares,
// or, where shared, one an unrelated model declares too.
func hierarchy(n int, wide, shared bool) string {
	var b strings.Builder
	b.WriteString("    A0: {type: object, properties: {x: {type: integer}}}\n")
	if shared {
		b.WriteString("    U: {type: object, properties: {")
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, "m%d: {type: string}, ", i)
		}
		b.WriteString("}}\n")
	}
	for i := 1; i < n; i++ {
		entries := fmt.Sprintf("{$ref: '#/components/schemas/A%d'}", i-1)
		if wide {
			entries += ", {$ref: '#/components/schemas/A0'}"
		}
		fmt.Fprintf(&b, "    A%d: {allOf: [%s, {type: object, properties: {x: {type: number}, m%d: {type: string}}}]}\n",
			i, entries, i)
	}
	return b.String()
}

// TestMembers_ADeclarationASettledModelHoldsKeepsWhereItsTypeIsWritten pins
// what a table read through a model not settled yet says of a member a settled
// model declares: where that model's settlement left its type written, not
// the member's own place, and the member's own place for any other.
func TestMembers_ADeclarationASettledModelHoldsKeepsWhereItsTypeIsWritten(t *testing.T) {
	t.Parallel()
	settled := ir.Property{ID: "p/K/b", WireName: "b", Provenance: ir.Provenance{Pointer: "/K/allOf/0/properties/b"}}
	other := ir.Property{ID: "p/L/b", WireName: "b", Provenance: ir.Provenance{Pointer: "/L/properties/b"}}
	typeAt := []ir.Provenance{{Pointer: "/K/allOf/1/properties/b"}}
	held := ir.PropertyDecl{Owner: "t/K", Property: &settled}
	rec := &members{tables: map[ir.TypeID]merge.Table{"t/K": {"b": {PropertyDecl: held, TypeAt: typeAt}}}}

	assert.Equal(t, typeAt, rec.declared(held).TypeAt, "as its settlement left it")
	for name, d := range map[string]ir.PropertyDecl{
		"a model with no table":              {Owner: "t/L", Property: &other},
		"another declaration under its name": {Owner: "t/K", Property: &other},
	} {
		assert.Equal(t, []ir.Provenance{other.Provenance}, rec.declared(d).TypeAt, name)
	}
}

// TestMembers_ReadsGrowWithAHierarchysSizeNotItsDepthSquared pins what the
// settlement reads of a hierarchy, a chain and one whose every level composes
// the first model too: its tables hold only names more than one model
// declares, so the reads grow linearly with the levels, each folding the x it
// inherits, however deep the hierarchy nests.
func TestMembers_ReadsGrowWithAHierarchysSizeNotItsDepthSquared(t *testing.T) {
	t.Parallel()
	for _, wide := range []bool{false, true} {
		read := func(n int) int {
			doc, diags, reads := settledUnder(t, hierarchy(n, wide, false), budget.Unbounded())
			openapitest.RequireNoErrorDiags(t, diags)
			last, ok := doc.Types[ir.TypeID(fmt.Sprintf("t/openapi/components/schemas/A%d", n-1))].(*ir.Model)
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("t/prim/integer"), last.Properties[0].Type.Target, "wide %v: the base's integer", wide)
			return reads
		}
		a, b, c := read(100), read(200), read(400)
		assert.LessOrEqual(t, float64(c-b), 2.5*float64(b-a), "wide %v: %d, %d, %d", wide, a, b, c)
	}
}

// TestMembers_ReadsPastTheBudgetFoldTheirOwnDeclarationsAlone pins the bound
// on what the settlement reads: a hierarchy each level of which adds a name an
// unrelated model declares holds more at each level, so past the bound the
// models left fold their own declarations alone, reported once, at the first.
func TestMembers_ReadsPastTheBudgetFoldTheirOwnDeclarationsAlone(t *testing.T) {
	t.Parallel()
	doc, diags, _ := settledUnder(t, hierarchy(40, false, true), budget.AtMost(100))

	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.BudgetExceeded, ir.SeverityError), "%+v", diags)
	first, ok := doc.Types["t/openapi/components/schemas/A1"].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/integer"), first.Properties[0].Type.Target, "settled before the bound")
	last, ok := doc.Types["t/openapi/components/schemas/A39"].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/prim/number"), last.Properties[0].Type.Target, "past it, its own")

	cut := 1
	for ; cut < 40; cut++ {
		m, isModel := doc.Types[ir.TypeID(fmt.Sprintf("t/openapi/components/schemas/A%d", cut))].(*ir.Model)
		require.True(t, isModel)
		if m.Properties[0].Type.Target == "t/prim/number" {
			break
		}
	}
	for _, d := range diags {
		if d.Code == diag.BudgetExceeded {
			assert.Equal(t, jsontext.Pointer(fmt.Sprintf("/components/schemas/A%d", cut)), d.Provenance.Pointer,
				"at the first model left with its own")
		}
	}
}

// TestMembers_ReadsAPartThroughItsAliasesAndNothingElse pins what a settlement
// reads of a part: a model's names more than one model declares, through any
// alias scalars, kept where the walks noted no model, and none for a type
// naming no model, an alias with no base among them, or a chain no model ends.
func TestMembers_ReadsAPartThroughItsAliasesAndNothingElse(t *testing.T) {
	t.Parallel()
	prop := func(wire string) ir.Property {
		return ir.Property{ID: ir.PropID("p/" + wire), WireName: wire, Type: ir.TypeRef{Target: "t/prim/string"}}
	}
	reg := ir.TypeRegistry{
		"t/M":     &ir.Model{ID: "t/M", Properties: []ir.Property{prop("p"), prop("q")}},
		"t/N":     &ir.Model{ID: "t/N", Properties: []ir.Property{prop("p")}},
		"t/alias": &ir.Scalar{ID: "t/alias", Base: &ir.TypeRef{Target: "t/M"}},
		"t/loop":  &ir.Scalar{ID: "t/loop", Base: &ir.TypeRef{Target: "t/loop"}},
		"t/bare":  &ir.Scalar{ID: "t/bare"},
		"t/u":     &ir.Union{ID: "t/u"},
	}
	rec := &members{byID: map[ir.TypeID]modelFields{"t/N": {}}, settled: map[ir.TypeID]bool{},
		tables: map[ir.TypeID]merge.Table{}, shared: map[string]bool{"p": true}, reads: budget.New(budget.Unbounded())}

	got := rec.tableOf(reg, "t/alias", "t/X")
	require.Len(t, got, 1, "q is declared once")
	assert.Equal(t, ir.TypeID("t/M"), got["p"].Owner)
	assert.Contains(t, rec.tables, ir.TypeID("t/M"), "a model the walks did not note is read once")
	assert.Len(t, rec.tableOf(reg, "t/N", "t/X"), 1)
	assert.NotContains(t, rec.tables, ir.TypeID("t/N"), "a noted model is read as it stands until settled")
	for _, target := range []ir.TypeID{"t/loop", "t/bare", "t/u", "t/missing"} {
		assert.Nil(t, rec.tableOf(reg, target, "t/X"), target)
	}
	assert.Empty(t, rec.cut, "no read was refused")
}

// TestMembers_ReadingAModelNotSettledChargesItsComposition pins what reading a
// model as it stands costs: its flattening walks each type its composition
// reaches and each entry naming one, besides the declarations it reads, so
// the reads charge both. N composes M through an alias and M2 beside it: four
// types, three entries, one name read with its two overridden declarations,
// and the table's one entry.
func TestMembers_ReadingAModelNotSettledChargesItsComposition(t *testing.T) {
	t.Parallel()
	prop := func(id string) ir.Property {
		return ir.Property{ID: ir.PropID(id), WireName: "p", Type: ir.TypeRef{Target: "t/prim/string"}}
	}
	reg := ir.TypeRegistry{
		"t/N": &ir.Model{ID: "t/N", Base: &ir.TypeRef{Target: "t/alias"}, Mixins: []ir.TypeRef{{Target: "t/M2"}},
			Properties: []ir.Property{prop("p/N")}},
		"t/alias": &ir.Scalar{ID: "t/alias", Base: &ir.TypeRef{Target: "t/M"}},
		"t/M":     &ir.Model{ID: "t/M", Properties: []ir.Property{prop("p/M")}},
		"t/M2":    &ir.Model{ID: "t/M2", Properties: []ir.Property{prop("p/M2")}},
	}
	rec := &members{byID: map[ir.TypeID]modelFields{"t/N": {}}, settled: map[ir.TypeID]bool{},
		tables: map[ir.TypeID]merge.Table{}, shared: map[string]bool{"p": true}, reads: budget.New(budget.Unbounded())}

	require.Len(t, rec.tableOf(reg, "t/N", "t/X"), 1)
	assert.Equal(t, 4+3+3+1, rec.reads.Spent(), "types, entries, declarations and the table's entry")
}

// deepLeaf is Leaf, redeclaring the id Base declares through a chain of depth
// models above Base, and M, composing Leaf and Base again.
func deepLeaf(depth int) string {
	var b strings.Builder
	b.WriteString("    Base: {type: object, properties: {id: {type: integer, description: base}}}\n")
	below := "Base"
	for i := 1; i <= depth; i++ {
		fmt.Fprintf(&b, "    L%d: {allOf: [{$ref: '#/components/schemas/%s'}]}\n", i, below)
		below = fmt.Sprintf("L%d", i)
	}
	fmt.Fprintf(&b, "    Leaf: {allOf: [{$ref: '#/components/schemas/%s'}, "+
		"{type: object, properties: {id: {type: integer, description: leaf, minimum: 5}}}]}\n", below)
	b.WriteString("    M: {allOf: [{$ref: '#/components/schemas/Leaf'}, {$ref: '#/components/schemas/Base'}]}\n")
	return b.String()
}

// TestMembers_ABaseComposedBesideItsHierarchyBringsNothingAtAnyDepth pins a
// model composing a hierarchy and its base again: the base's declaration
// stands within the hierarchy however deep it nests, so the model declares
// nothing, reports nothing, and its flattening takes Leaf's declaration.
func TestMembers_ABaseComposedBesideItsHierarchyBringsNothingAtAnyDepth(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{4, 200} {
		doc, diags, _ := settledUnder(t, deepLeaf(depth), budget.Unbounded())

		openapitest.RequireNoErrorDiags(t, diags)
		m, ok := doc.Types["t/openapi/components/schemas/M"].(*ir.Model)
		require.True(t, ok)
		assert.Empty(t, m.Properties, "at depth %d", depth)
		flat := ir.FlattenedProperties(doc.Types, "t/openapi/components/schemas/M")
		require.Len(t, flat.Properties, 1, "at depth %d", depth)
		assert.Equal(t, "leaf", flat.Properties[0].Property.Docs.Description, "at depth %d", depth)
		for _, d := range diags {
			assert.NotContains(t, string(d.Provenance.Pointer), "/components/schemas/M", "at depth %d", depth)
		}
	}
}

// TestMembers_ACutTellingDeclarationsApartIsReported pins the bound on telling
// whether one part's declaration stands within another's composition: it
// spends the run's reads, and where they run out there, in the last model to
// settle, the cut is reported at that model.
func TestMembers_ACutTellingDeclarationsApartIsReported(t *testing.T) {
	t.Parallel()
	_, _, all := settledUnder(t, deepLeaf(200), budget.Unbounded())

	_, diags, _ := settledUnder(t, deepLeaf(200), budget.AtMost(all-50))

	assert.Equal(t, []jsontext.Pointer{"/components/schemas/M"}, openapitest.PointersOf(diags, diag.BudgetExceeded), "%+v", diags)
}

// twoDeepLeaves is deepLeaf twice over, for x over BaseX and y over BaseY, and M
// composing both hierarchies and both bases again.
func twoDeepLeaves(depth int) string {
	var b strings.Builder
	for _, name := range []string{"X", "Y"} {
		fmt.Fprintf(&b, "    Base%s: {type: object, properties: {%s: {type: integer}}}\n", name, strings.ToLower(name))
		below := "Base" + name
		for i := 1; i <= depth; i++ {
			fmt.Fprintf(&b, "    L%s%d: {allOf: [{$ref: '#/components/schemas/%s'}]}\n", name, i, below)
			below = fmt.Sprintf("L%s%d", name, i)
		}
		fmt.Fprintf(&b, "    Leaf%s: {allOf: [{$ref: '#/components/schemas/%s'}, "+
			"{type: object, properties: {%s: {type: integer, minimum: 5}}}]}\n", name, below, strings.ToLower(name))
	}
	b.WriteString("    M: {allOf: [{$ref: '#/components/schemas/LeafX'}, {$ref: '#/components/schemas/BaseX'}, " +
		"{$ref: '#/components/schemas/LeafY'}, {$ref: '#/components/schemas/BaseY'}]}\n")
	return b.String()
}

// TestMembers_ACutTellingDeclarationsApartFallsAlikeInEveryRun pins the order
// a settlement asks of its names: where the reads run out between two names'
// walks, the same name is cut in every run, so the document does not follow
// the order a map yields them in.
func TestMembers_ACutTellingDeclarationsApartFallsAlikeInEveryRun(t *testing.T) {
	t.Parallel()
	_, _, all := settledUnder(t, twoDeepLeaves(100), budget.Unbounded())
	declared := func() []string {
		doc, _, _ := settledUnder(t, twoDeepLeaves(100), budget.AtMost(all-60))
		m, ok := doc.Types["t/openapi/components/schemas/M"].(*ir.Model)
		require.True(t, ok)
		wires := make([]string, 0, len(m.Properties))
		for _, p := range m.Properties {
			wires = append(wires, p.WireName)
		}
		return wires
	}
	first := declared()
	require.Len(t, first, 1, "the cut walk leaves one name's candidates both in")
	for range 30 {
		assert.Equal(t, first, declared())
	}
}

// clique is n models each composing every other, each declaring x.
func clique(n int) string {
	var b strings.Builder
	for i := range n {
		var entries []string
		for j := range n {
			if j != i {
				entries = append(entries, fmt.Sprintf("{$ref: '#/components/schemas/K%d'}", j))
			}
		}
		fmt.Fprintf(&b, "    K%d: {allOf: [%s, {type: object, properties: {x: {type: integer}}}]}\n", i, strings.Join(entries, ", "))
	}
	return b.String()
}

// TestMembers_ACycleIsChargedTheCompositionsItsReadsWalk pins the bound on a
// cycle of compositions: a model of it is read as it stands by each settled
// before it, each read walking every entry of the cycle, so the reads grow
// with the square of its entries, faster than the declarations they find,
// and a cycle dense enough is cut at the bound and reported once.
func TestMembers_ACycleIsChargedTheCompositionsItsReadsWalk(t *testing.T) {
	t.Parallel()
	read := func(n int) int {
		_, diags, reads := settledUnder(t, clique(n), budget.Unbounded())
		openapitest.RequireNoErrorDiags(t, diags)
		return reads
	}
	a, b := read(6), read(12)
	assert.GreaterOrEqual(t, b, 12*a, "reads at 6 and 12 models: %d, %d", a, b)

	_, diags, _ := settledUnder(t, clique(12), budget.AtMost(b/2))
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.BudgetExceeded, ir.SeverityError), "%+v", diags)
}

// TestMembers_TheSettlementAsksItsContextBeforeEachModel pins where a cancel
// stops the settlement: it asks the run's context once before each model it
// settles, so a hierarchy twice as long asks that many more times, and once
// the context refuses no further model settles.
func TestMembers_TheSettlementAsksItsContextBeforeEachModel(t *testing.T) {
	t.Parallel()
	finish := func(models, left int) (asked, settledAtRefusal, settled int) {
		l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", hierarchy(models+1, false, false)))
		l.Diags.AppendAll(LowerComponentSchemas(t.Context(), l.Ctx, l.Types, l.Schemas()))
		rec := membersOf(l.Ctx)
		ctx := &ctxtest.Countdown{Context: t.Context(), Left: left}
		ctx.OnRefuse = func() { settledAtRefusal = len(rec.settled) }
		l.Types.Finish(ctx)
		return ctx.Calls, settledAtRefusal, len(rec.settled)
	}

	short, _, all := finish(20, math.MaxInt)
	long, _, _ := finish(40, math.MaxInt)
	require.Equal(t, 20, all, "every composing model settles")
	assert.Equal(t, 20, long-short, "one ask for each model more")

	_, atRefusal, settled := finish(20, short-10)
	assert.Equal(t, atRefusal, settled, "nothing settles once the context refuses")
	assert.Positive(t, settled)
	assert.Less(t, settled, all)
}
