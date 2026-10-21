package merge

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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

// stub is a Merger and the conflict lattice over one plain map.
type stub struct {
	*Merger
	Nodes
}

// stubMerger returns a Merger backed by a plain map and a diagnostic recorder,
// with no lowerer, parser or document in the setup, so the conflict lattice can
// be driven directly. Its Later reports each conflict at once against the map,
// as the compiler's settlement does once the registry is whole.
func stubMerger(reg map[ir.TypeID]ir.TypeDef) (*stub, *[]ir.Diagnostic) {
	recorded := &[]ir.Diagnostic{}
	nodes := Nodes(func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok })
	report := func(sev ir.Severity, code string, pointer jsontext.Pointer, format string, args ...any) {
		*recorded = append(*recorded, diag.Newf(sev, code, ir.Provenance{Pointer: pointer}, format, args...))
	}
	g := &Merger{Report: report, Later: func(c Conflict) {
		if pointer, message, ok := c.Report(nodes); ok {
			report(ir.SeverityWarning, diag.ConflictingRedecl, pointer, "%s", message)
		}
	}}
	return &stub{Merger: g, Nodes: nodes}, recorded
}

// TestMerger_ATypeConflictWaitsForTheRegistry pins what makes the report order
// free (GitHub #898): a redeclaration of another type reports nothing while the
// walk runs and goes to Later, still folded as one that lost its type, and
// whether it conflicts is read from the registry it is reported against, which
// may lack a node mid-walk.
func TestMerger_ATypeConflictWaitsForTheRegistry(t *testing.T) {
	t.Parallel()
	var later []Conflict
	var reported []string
	g := &Merger{
		Report: func(_ ir.Severity, code string, _ jsontext.Pointer, _ string, _ ...any) {
			reported = append(reported, code)
		},
		Later: func(c Conflict) { later = append(later, c) },
	}
	dst := ir.Property{WireName: "m", Type: ir.TypeRef{Target: "t/list"}, Provenance: ptrAt("/a")}
	src := ir.Property{WireName: "m", Type: ir.TypeRef{Target: "t/model"}, Provenance: ptrAt("/b")}

	dropped, lost := g.recordRedeclarationConflict(&dst, &src)

	assert.True(t, dropped, "the redeclaration's type is lost whatever the nodes say")
	assert.True(t, lost)
	assert.Empty(t, reported, "nothing is reported mid-walk")
	require.Len(t, later, 1)

	whole := Nodes(func(id ir.TypeID) (ir.TypeDef, bool) {
		td, ok := map[ir.TypeID]ir.TypeDef{"t/list": &ir.List{ID: "t/list"}, "t/model": &ir.Model{ID: "t/model"}}[id]
		return td, ok
	})
	pointer, message, ok := later[0].Report(whole)
	require.True(t, ok, "a list against a model conflicts")
	assert.Equal(t, jsontext.Pointer("/b"), pointer)
	assert.Contains(t, message, "incompatible types t/list and t/model")

	partial := Nodes(func(ir.TypeID) (ir.TypeDef, bool) { return nil, false })
	_, _, ok = later[0].Report(partial)
	assert.False(t, ok, "read before the model exists, the same pair would have reported nothing")
}

func TestMerger_TypesConflictComparesReferentsNotReferences(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/a":   &ir.Primitive{ID: "t/a", Prim: ir.PrimString},
		"t/b":   &ir.Primitive{ID: "t/b", Prim: ir.PrimString},
		"t/i":   &ir.Primitive{ID: "t/i", Prim: ir.PrimInt32},
		"t/any": &ir.Any{ID: "t/any"},
		"t/m":   &ir.Model{ID: "t/m"},
	})
	ref := func(id ir.TypeID) ir.TypeRef { return ir.TypeRef{Target: id} }

	assert.False(t, g.typesConflict(ref("t/a"), ref("t/b")),
		"two references to the same primitive kind agree, however they are named")
	assert.True(t, g.typesConflict(ref("t/a"), ref("t/i")),
		"string and integer cannot both be satisfied")
	assert.False(t, g.typesConflict(ref("t/any"), ref("t/i")),
		"the top type absorbs anything, so it conflicts with nothing")
	assert.True(t, g.typesConflict(ref("t/m"), ref("t/a")),
		"a structural type and a primitive are different kinds")
}

// TestMerger_ReconcileReportsDisagreementAndKeepsAWinner pins the documented
// policy: an unsatisfiable redeclaration is surfaced rather than silently
// resolved, and the merge still yields one property.
func TestMerger_ReconcileReportsDisagreementAndKeepsAWinner(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/int": &ir.Primitive{ID: "t/int", Prim: ir.PrimInt32},
	})
	dst := ir.Property{Name: ir.Naming{Source: "id"}, WireName: "id", Type: ir.TypeRef{Target: "t/str"}}
	src := ir.Property{Name: ir.Naming{Source: "id"}, WireName: "id", Type: ir.TypeRef{Target: "t/int"},
		Provenance: ptrAt("/components/schemas/S/properties/id")}

	g.reconcileProperty(&dst, src, written(`{"type":"integer"}`))

	require.Len(t, *recorded, 1, "one disagreement, one diagnostic")
	d := (*recorded)[0]
	assert.Equal(t, diag.ConflictingRedecl, d.Code)
	assert.Contains(t, d.Message, `"id"`, "the diagnostic names the field that disagrees")
	assert.Equal(t, ir.TypeID("t/str"), dst.Type.Target, "the property already merged keeps its type")
}

// TestMerger_MergeConstraintsKeepsBothSidesBounds covers the merge half rather
// than the conflict half: constraints that do not disagree combine.
func TestMerger_MergeConstraintsKeepsBothSidesBounds(t *testing.T) {
	t.Parallel()
	got := mergeConstraints(
		&ir.Constraints{MaxLength: new(int64(10))},
		&ir.Constraints{MinLength: new(int64(2))},
	)
	require.NotNil(t, got)
	require.NotNil(t, got.MaxLength)
	require.NotNil(t, got.MinLength)
	assert.Equal(t, int64(10), *got.MaxLength)
	assert.Equal(t, int64(2), *got.MinLength, "a bound only the second side declares survives the merge")
}

func TestMerger_ResolvePrimKindReportsUnresolvableTargets(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/str": &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
	})

	kind, ok := g.resolvePrimKind(ir.TypeRef{Target: "t/str"})
	require.True(t, ok)
	assert.Equal(t, ir.PrimString, kind)

	_, ok = g.resolvePrimKind(ir.TypeRef{Target: "t/absent"})
	assert.False(t, ok, "a target the registry does not hold resolves to no kind")
}
