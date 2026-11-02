// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestOutcomeKeeps_ReportsANodeTheRegistryLacks pins the one way recordOn keeps
// nothing: the node it was handed is not in the registry, a compiler bug no source
// provokes, which it reports rather than dropping what it holds.
func TestOutcomeKeeps_ReportsANodeTheRegistryLacks(t *testing.T) {
	t.Parallel()
	l, s, at := lowererDeclaringS(t)
	diags := outcomeKeeps{keywords: []string{"format"}, shape: refinedShape}.recordOn(l.Ctx, l.Types, "t/anon/none", s, at)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.InternalInvariant, diags[0].Code)
}

// TestRecordVoicedResidue_NeedsANodeItsPositionOwns pins the two positions that
// record nothing: a carrier holds what binds a use itself, and a pointer no node
// stands at has nowhere to keep it.
func TestRecordVoicedResidue_NeedsANodeItsPositionOwns(t *testing.T) {
	t.Parallel()
	l, s, at := lowererDeclaringS(t)
	voices := []annotation.Voice{{Node: s, At: at}}
	assert.Empty(t, recordVoicedResidue(l.Ctx, l.Types, voices, at, annotation.HomeCarrier))
	assert.Empty(t, recordVoicedResidue(l.Ctx, l.Types, voices, at, annotation.HomeOwnNode))
	internNode(l.Ctx, l.Types, at, func(cm ir.TypeCommon) ir.TypeDef { return &ir.Model{TypeCommon: cm} })
	assert.NotEmpty(t, recordVoicedResidue(l.Ctx, l.Types, voices, at, annotation.HomeOwnNode))
}

// declared returns the schema the component name declares.
func declared(t *testing.T, l *Lowerer, name string) *oas3.Schema {
	t.Helper()
	js, ok := l.Ctx.Doc.Components.Schemas.Get(name)
	require.True(t, ok, name)
	return js.GetSchema()
}

// TestRefinementOf_ReadsAPositionOnce pins that a position's decision is read once
// a run, however many of its readers ask: its target changes after the first read,
// which no lowering does, so only a remembered answer stays. Where it is read from
// is part of the answer, so a pointer not yet asked reads afresh.
func TestRefinementOf_ReadsAPositionOnce(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    Port: {type: integer}
    T: {allOf: [{$ref: '#/components/schemas/Port'}]}
`))
	const at = "/components/schemas/T"
	s := declared(t, l, "T")
	_, refined := refinementOf(l.Ctx, s, at)
	require.True(t, refined)

	declared(t, l, "Port").Type = oas3.NewTypeFromString(oas3.SchemaTypeObject)
	_, refined = refinementOf(l.Ctx, s, at)
	assert.True(t, refined, "the second ask reads what the first read")

	_, refined = refinementOf(l.Ctx, s, "/elsewhere")
	assert.False(t, refined, "another pointer is another answer")
}

// TestAdmission_ReadsAValueSetOnce pins that an enum is read once a run however
// many positions read it, which is what keeps positions times members from being
// the cost of a shared enum: a member changes after the first read, which no
// lowering does, so only a remembered set stays.
func TestAdmission_ReadsAValueSetOnce(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    E: {type: string, enum: [a, b]}\n"))
	s := declared(t, l, "E")
	first := newAdmission(l.Ctx)
	require.Contains(t, first.written(s).values, "s:a")

	s.GetEnum()[0].Value = "z"
	second := newAdmission(l.Ctx)
	read := second.written(s)
	assert.Contains(t, read.values, "s:a", "the second read takes what the first remembered")
	assert.NotContains(t, read.values, "s:z")
}

// TestVoicesAmong_ReadsASchemaOnce pins that what folds is read once a run however
// many uses of a schema ask, which keeps a schema's uses times its branches from
// being the cost of a wide composition: its allOf goes after the first read, which
// no lowering does, so only a remembered answer stays.
func TestVoicesAmong_ReadsASchemaOnce(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    Str: {type: string}
    T: {allOf: [{$ref: '#/components/schemas/Str'}, {description: d}]}
`))
	s := declared(t, l, "T")
	require.Equal(t, []bool{false, true}, voicesAmong(l.Ctx, s))

	s.AllOf = nil
	assert.Equal(t, []bool{false, true}, voicesAmong(l.Ctx, s), "the second ask reads what the first read")
}

// TestNullableVoices_ReadsASchemaOnce pins that the branches widening a schema
// are read once a run, as its voicing is, since every use of the schema asks:
// its allOf goes after the first read, so only a remembered answer stays.
func TestNullableVoices_ReadsASchemaOnce(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    Str: {type: string}
    T: {allOf: [{$ref: '#/components/schemas/Str'}, {nullable: true}]}
`))
	s := declared(t, l, "T")
	first := nullableVoices(l.Ctx, s)
	require.Len(t, first, 1)
	assert.Equal(t, jsontext.Pointer("/allOf/1"), first[0].pointer, "each branch at its pointer under the schema's")

	s.AllOf = nil
	assert.Equal(t, first, nullableVoices(l.Ctx, s), "the second ask reads what the first read")
}

// TestLendingBranches_LendsEachEntryOnce pins what a use of a wide composition
// reads: a branch repeating what s or a branch before it writes lends nothing
// more, so a use pays for each entry once, while one writing a new entry lends it.
func TestLendingBranches_LendsEachEntryOnce(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    Obj: {type: object}
    T:
      description: d
      allOf:
        - {$ref: '#/components/schemas/Obj'}
        - {description: d}
        - {description: d, readOnly: true}
        - {readOnly: true}
        - {description: d}
`))
	got := lendingBranches(l.Ctx, declared(t, l, "T"))
	require.Len(t, got, 1, "only the branch first writing readOnly lends what T does not")
	require.NotNil(t, got[0].ReadOnly)
	assert.True(t, *got[0].ReadOnly)
	assert.Equal(t, "d", got[0].GetDescription())

	split, _ := LoweredFor(t, openapitest.ComponentSpec(`    Obj: {type: object}
    T: {allOf: [{$ref: '#/components/schemas/Obj'}, {title: a, description: d}, {title: b}, {description: d}]}
`))
	got = lendingBranches(split.Ctx, declared(t, split, "T"))
	require.Len(t, got, 1, "a branch kept for disagreeing lends nothing, nor marks what it writes as lent")
	assert.Empty(t, got[0].GetTitle())
	assert.Equal(t, "d", got[0].GetDescription())
}

// TestLentBy_ReadsATargetOnce pins that what a schema lends is read once a run
// however many $refs name it, for the reason voicesAmong's answer is: its allOf
// goes after the first read, so only a remembered chain stays.
func TestLentBy_ReadsATargetOnce(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    Str: {type: string}
    T: {allOf: [{$ref: '#/components/schemas/Str'}, {description: d}]}
`))
	s := declared(t, l, "T")
	first := lentBy(l.Ctx, s).Referents
	require.Len(t, first, 4, "T, the branch that folds, the $ref and what it names")

	s.AllOf = nil
	assert.Equal(t, first, lentBy(l.Ctx, s).Referents, "the second ask reads what the first read")
}

// TestAdmission_RendersASchemaOnce pins that a schema's text is rendered once a run
// however many positions restate it: the node changes after the first render, which
// no lowering does, so only remembered text stays.
func TestAdmission_RendersASchemaOnce(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    T: {type: string}\n"))
	js, ok := l.Ctx.Doc.Components.Schemas.Get("T")
	require.True(t, ok)
	first := newAdmission(l.Ctx)
	text := first.text(js)
	require.NotNil(t, text)

	*js.GetCore().GetRootNode() = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "other"}
	second := newAdmission(l.Ctx)
	assert.Equal(t, text, second.text(js), "the second ask reads what the first rendered")
}

// TestReadVoices_RendersEachValueOnce pins what a first writer costs: every later
// conjunct compares with it, so rendering it for each comparison made a document's
// branches times its longest value the cost of one composition. It is not parallel,
// as the count is the process's, and takes the least of three counts, since
// anything else allocating only adds to one.
func TestReadVoices_RendersEachValueOnce(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	cost := func(later int) uint64 {
		var src strings.Builder
		src.WriteString("    T:\n      allOf:\n        - {description: " + big + "}\n")
		for i := range later {
			fmt.Fprintf(&src, "        - {description: d%d}\n", i)
		}
		l, _ := LoweredFor(t, openapitest.ComponentSpec(src.String()))
		s := declared(t, l, "T")
		var stats runtime.MemStats
		least := uint64(math.MaxUint64)
		for range 3 {
			runtime.ReadMemStats(&stats)
			before := stats.TotalAlloc
			read := readVoices(s)
			runtime.ReadMemStats(&stats)
			least = min(least, stats.TotalAlloc-before)
			require.Len(t, read.speaks, later+1)
		}
		return least
	}
	assert.Less(t, cost(256), 2*cost(64), "four times the branches, not four times the value")
}

// TestRead_AVoiceCostsItsOwnEntries pins what a site's voices cost: each adds its
// own entries, where overlaying what came before on each made the cost of a
// composition its branches squared. It is not parallel, as the count is the
// process's, and takes the least of three counts for the same reason as
// TestReadVoices_RendersEachValueOnce.
func TestRead_AVoiceCostsItsOwnEntries(t *testing.T) {
	const at = "/components/schemas/T"
	cost := func(voices int) uint64 {
		var src strings.Builder
		src.WriteString("    O: {type: object}\n    T:\n      allOf:\n        - {$ref: '#/components/schemas/O'}\n")
		for i := range voices {
			fmt.Fprintf(&src, "        - {x-k%d: 1}\n", i)
		}
		l, _ := LoweredFor(t, openapitest.ComponentSpec(src.String()))
		s := declared(t, l, "T")
		site := Use{Voices: positionVoices(l.Ctx, l.Types, s, at, ir.TypeRef{})}.Site(s)
		require.Len(t, site.Voices, voices)
		var stats runtime.MemStats
		least := uint64(math.MaxUint64)
		for range 3 {
			runtime.ReadMemStats(&stats)
			before := stats.TotalAlloc
			_, _ = annotation.Read(site, at, l.Ctx.ProvenanceAt)
			runtime.ReadMemStats(&stats)
			least = min(least, stats.TotalAlloc-before)
		}
		return least
	}
	one, two, four := cost(500), cost(1000), cost(2000)
	assert.LessOrEqual(t, four-two, (two-one)*5/2, "linear growth gives 2, quadratic 4")
}

// TestKindOfValue_NamesEachValuesKind pins each kind of value to the JSON kind an
// instance of it is, and what is no instance to none.
func TestKindOfValue_NamesEachValuesKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		v    ir.Value
		want kindSet
	}{
		{ir.Value{Kind: ir.ValueString}, kindString},
		{ir.Value{Kind: ir.ValueSymbol}, kindString},
		{ir.Value{Kind: ir.ValueBytes}, kindString},
		{ir.Value{Kind: ir.ValueBool}, kindBoolean},
		{ir.Value{Kind: ir.ValueNull}, kindNull},
		{ir.Value{Kind: ir.ValueList}, kindArray},
		{ir.Value{Kind: ir.ValueObject}, kindObject},
		{ir.Value{Kind: ir.ValueNumber, Num: "1.0"}, kindInteger},
		{ir.Value{Kind: ir.ValueNumber, Num: "1.5"}, kindFraction},
		{ir.Value{Kind: ir.ValueNumber, Num: "1e2"}, kindNumber},
		{ir.Value{Kind: ir.ValueRefKind}, 0},
		{ir.Value{Kind: ir.ValueCtor}, 0},
		{ir.Value{Kind: "no kind"}, 0},
	} {
		assert.Equal(t, tc.want, kindOfValue(tc.v), tc.v.Kind)
	}
}

// TestAdmission_AnEnumAndAConstAreTwoSets pins that one schema's two value sets are
// remembered apart: each is read and conjoined, so an instance is held to both.
func TestAdmission_AnEnumAndAConstAreTwoSets(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", "    E: {type: string, enum: [a, b], const: a}\n"))
	w := newAdmission(l.Ctx)
	assert.Equal(t, map[string]kindSet{"s:a": kindString}, w.written(declared(t, l, "E")).values)
}

// TestAdmission_ANullableBesideARefAdmitsNoNull pins a 3.0 $ref's siblings: the
// dialect ignores them, so a `nullable` there widens nothing a reading holds.
func TestAdmission_ANullableBesideARefAdmitsNoNull(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpecVer("3.0.3", `    S: {type: string}
    T: {$ref: '#/components/schemas/S', type: string, nullable: true}
`))
	w := newAdmission(l.Ctx)
	assert.Equal(t, kindString, w.written(declared(t, l, "T")).kinds)
}

// TestAdmission_ASchemaIsWithinItself pins the comparison every election makes of
// a conjunct with itself, which needs no rendering: rendering costs the whole
// `items` subtree at each level, and a value that will not render shows it ran.
func TestAdmission_ASchemaIsWithinItself(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    E: {type: array, items: {type: string, x-t: .nan}}\n"))
	w := newAdmission(l.Ctx)
	read := w.written(declared(t, l, "E"))
	assert.True(t, w.within(read, read))
}

// TestAdmission_ARememberedSetStillSpends pins why a remembered set costs its
// budget: a read's verdict turns on what it spent, so a set a first reader
// read must cut a later one exactly as reading it would, or which position
// lowered first would decide what another lowers to.
func TestAdmission_ARememberedSetStillSpends(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    E: {type: string, enum: [a, b]}\n"))
	s := declared(t, l, "E")
	first := newAdmission(l.Ctx)
	first.written(s)
	require.False(t, first.incomplete())

	short := newAdmission(l.Ctx)
	short.members = budget.New(budget.AtMost(1))
	assert.Empty(t, short.written(s).values, "a set past the budget is not read at all")
	assert.True(t, short.incomplete())
}

// TestAdmission_AnUnreadableSetIsRememberedAsUnread pins that a set no reader could
// read cuts every read of it, the first and those after.
func TestAdmission_AnUnreadableSetIsRememberedAsUnread(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    E: {type: string, enum: [a, b]}\n"))
	s := declared(t, l, "E")
	s.GetEnum()[0].Kind = 0
	for range 2 {
		w := newAdmission(l.Ctx)
		w.written(s)
		assert.True(t, w.incomplete())
	}
}
