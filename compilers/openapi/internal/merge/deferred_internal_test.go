package merge

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// deferring is a Merger over reg whose Defer queues instead of settling.
func deferring(reg map[ir.TypeID]ir.TypeDef) (g *Merger, queued *[]Settle, reported *[]ir.Diagnostic) {
	g, reported = stubMerger(reg)
	queued = &[]Settle{}
	g.Defer = func(s Settle) { *queued = append(*queued, s) }
	return g, queued, reported
}

// redeclared is the property v of the allOf branch at, typed target.
func redeclared(target ir.TypeID, at string) ir.Property {
	return ir.Property{WireName: "v", Type: ir.TypeRef{Target: target}, Provenance: ptrAt(jsontext.Pointer("/allOf/" + at + "/properties/v"))}
}

// TestMergeProperty_AMissedTypeDefersEveryRedeclarationOfTheProperty pins what
// keeps the fold order free (GitHub #898): a redeclaration whose judgment reads
// a node the registry lacks mid-walk is neither folded nor reported, and nor is
// any later one of the property; the settlement judges each against the whole
// registry, in order.
func TestMergeProperty_AMissedTypeDefersEveryRedeclarationOfTheProperty(t *testing.T) {
	t.Parallel()
	reg := map[ir.TypeID]ir.TypeDef{
		"t/str":  &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/bool": &ir.Primitive{ID: "t/bool", Prim: ir.PrimBool},
	}
	g, queued, reported := deferring(reg)
	m, idx := &ir.Model{ID: "t/m"}, NewPropIndex()

	g.MergeProperty(m, idx, redeclared("t/str", "0"), written(`{"type":"string"}`))
	g.MergeProperty(m, idx, redeclared("t/int", "1"), written(`{"$ref":"int"}`))
	g.MergeProperty(m, idx, redeclared("t/bool", "2"), written(`{"type":"boolean"}`))

	require.Len(t, *queued, 1, "one settlement per property, however many redeclarations")
	require.Len(t, m.Properties, 1)
	assert.Equal(t, ir.TypeID("t/str"), m.Properties[0].Type.Target)
	assert.Empty(t, m.Properties[0].Unmodeled, "nothing is folded before the settlement runs")
	assert.Empty(t, *reported, "nothing is reported before the settlement runs")

	reg["t/int"] = &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32}
	reg["t/m"] = m
	var settled []ir.Diagnostic
	(*queued)[0](reg, func(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any) {
		settled = append(settled, diag.Newf(sev, code, ir.Provenance{Pointer: pointer}, format, args...))
	})

	require.Len(t, settled, 2, "each redeclaration is judged, in order, against the whole registry, one that resolved included")
	assert.Equal(t, diag.ConflictingRedecl, settled[0].Code)
	assert.Equal(t, diag.ConflictingRedecl, settled[1].Code)
	assert.Equal(t, ptrAt("/allOf/1/properties/v").Pointer, settled[0].Provenance.Pointer)
	assert.Equal(t, ptrAt("/allOf/2/properties/v").Pointer, settled[1].Provenance.Pointer)
	assert.Len(t, m.Properties[0].Unmodeled, 2, "both losing declarations are kept")
	assert.Empty(t, *reported, "the walk's sink is not written to afterwards")
}

// TestMergeProperty_AValueJudgedAgainstAMissedTypeDefers pins that the lookups
// the value judgment makes are counted: the top type against a list relates
// without reading the element, but its default is held to that element.
func TestMergeProperty_AValueJudgedAgainstAMissedTypeDefers(t *testing.T) {
	t.Parallel()
	g, queued, reported := deferring(map[ir.TypeID]ir.TypeDef{
		"t/any":  &ir.Primitive{ID: "t/any", Prim: ir.PrimAny},
		"t/list": &ir.List{ID: "t/list", Elem: ir.TypeRef{Target: "t/late"}},
	})
	m, idx := &ir.Model{ID: "t/m"}, NewPropIndex()
	broad := redeclared("t/any", "0")
	broad.Default = &ir.Value{Kind: ir.ValueList, List: []ir.Value{{Kind: ir.ValueString, Str: "x"}}}

	g.MergeProperty(m, idx, broad, written(`{"default":["x"]}`))
	g.MergeProperty(m, idx, redeclared("t/list", "1"), written(`{"type":"array"}`))

	require.Len(t, *queued, 1, "the default is judged against an element no node holds yet")
	assert.Empty(t, *reported)
	assert.NotNil(t, m.Properties[0].Default, "nothing is refused before the settlement runs")
}

// TestMergeProperty_ASettlementOfAReplacedModelIsAnInvariantBreach pins that a
// fold deferred on a model the registry no longer holds under its ID is
// reported as a compiler bug and not taken. No lowering rebuilds a model, so no
// document reaches it; folding silently into the orphan would hide the bug.
func TestMergeProperty_ASettlementOfAReplacedModelIsAnInvariantBreach(t *testing.T) {
	t.Parallel()
	g, queued, _ := deferring(map[ir.TypeID]ir.TypeDef{"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString}})
	m, idx := &ir.Model{ID: "t/m"}, NewPropIndex()
	g.MergeProperty(m, idx, redeclared("t/str", "0"), written(`{}`))
	g.MergeProperty(m, idx, redeclared("t/int", "1"), written(`{}`))
	require.Len(t, *queued, 1)

	reg := ir.TypeRegistry{"t/m": &ir.Model{ID: "t/m"}, "t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32}}
	var settled []ir.Diagnostic
	(*queued)[0](reg, func(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any) {
		settled = append(settled, diag.Newf(sev, code, ir.Provenance{Pointer: pointer}, format, args...))
	})

	require.Len(t, settled, 1)
	assert.Equal(t, diag.InternalInvariant, settled[0].Code)
	assert.Equal(t, ir.SeverityError, settled[0].Severity)
	assert.Equal(t, jsontext.Pointer("/allOf/1/properties/v"), settled[0].Provenance.Pointer, "at the first redeclaration deferred")
	assert.Empty(t, m.Properties[0].Unmodeled, "the replaced model is left as it was")
}

func TestMergeProperty_AResolvedRedeclarationIsFoldedAtOnceBesideADefer(t *testing.T) {
	t.Parallel()
	g, queued, reported := deferring(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
	})
	m, idx := &ir.Model{ID: "t/m"}, NewPropIndex()

	g.MergeProperty(m, idx, redeclared("t/str", "0"), written(`{}`))
	g.MergeProperty(m, idx, redeclared("t/int", "1"), written(`{"type":"integer"}`))

	assert.Empty(t, *queued, "every type resolved, so nothing waits")
	require.Len(t, *reported, 1)
	assert.Equal(t, diag.ConflictingRedecl, (*reported)[0].Code)
}
