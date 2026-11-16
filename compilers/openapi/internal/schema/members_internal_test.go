// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// settledUnder lowers schemas, components of a 3.1 document, with the run's
// settlement reading at most limit declarations, and returns the document, what
// it reports and the declarations the settlement read.
func settledUnder(t *testing.T, schemas string, limit budget.Limit) (*ir.Document, []ir.Diagnostic, int) {
	t.Helper()
	l, diags := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", schemas))
	membersOf(l.Ctx).reads = budget.New(limit)
	l.Diags.AppendAll(LowerComponentSchemas(t.Context(), l.Ctx, l.Types))
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
