// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// TestAdmission_ReadsAValueSetOnce pins that an enum is rendered once a run however
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
	assert.Contains(t, read.values, "s:a", "the second read takes what the first rendered")
	assert.NotContains(t, read.values, "s:z")
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
// rendered must cut a later one exactly as rendering it would, or which position
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
// render cuts every read of it, the first and those after.
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
