package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// stubMerger returns a Merger backed by a plain map and a diagnostic recorder,
// with no lowerer, no parser and no document anywhere in the setup.
//
// This is the extraction's whole purpose. Reaching the conflict lattice
// previously meant standing up a compiler and feeding it a spec that happened to
// produce the pair of declarations under test; the registry dependency is narrow
// enough to pass as a function, so the lattice can be driven directly.
// ptrAt is the provenance a declaration at pointer carries. MergeProperty reads
// the position off the property rather than taking it alongside, so a test that
// merges one has to say where it was written.
func ptrAt(pointer string) ir.Provenance { return ir.Provenance{Pointer: pointer} }

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

func stubMerger(reg map[ir.TypeID]ir.TypeDef) (*Merger, *[]ir.Diagnostic) {
	recorded := &[]ir.Diagnostic{}
	g := &Merger{
		Resolve: func(id ir.TypeID) (ir.TypeDef, bool) { td, ok := reg[id]; return td, ok },
		Report: func(sev ir.Severity, code, pointer, format string, args ...any) {
			*recorded = append(*recorded, diag.Newf(sev, code, ir.Provenance{Pointer: pointer}, format, args...))
		},
	}
	return g, recorded
}

func TestMerger_TypesConflictComparesReferentsNotReferences(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/a":   &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/a"}, Prim: ir.PrimString},
		"t/b":   &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/b"}, Prim: ir.PrimString},
		"t/i":   &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/i"}, Prim: ir.PrimInt32},
		"t/any": &ir.Any{TypeCommon: ir.TypeCommon{ID: "t/any"}},
		"t/m":   &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/m"}},
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
		"t/str": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/str"}, Prim: ir.PrimString},
		"t/int": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/int"}, Prim: ir.PrimInt32},
	})
	dst := ir.Property{Name: ir.Naming{Source: "id"}, WireName: "id", Type: ir.TypeRef{Target: "t/str"}}
	src := ir.Property{Name: ir.Naming{Source: "id"}, WireName: "id", Type: ir.TypeRef{Target: "t/int"},
		Provenance: ptrAt("/components/schemas/S/properties/id")}

	g.reconcileProperty(&dst, src, written(`{"type":"integer"}`))

	require.Len(t, *recorded, 1, "one disagreement, one diagnostic")
	d := (*recorded)[0]
	assert.Equal(t, diag.ConflictingRedecl, d.Code)
	assert.Contains(t, d.Message, `"id"`, "the diagnostic names the field that disagrees")
}

// TestMerger_MergeConstraintsKeepsBothSidesBounds covers the merge half rather
// than the conflict half: constraints that do not disagree combine.
func TestMerger_MergeConstraintsKeepsBothSidesBounds(t *testing.T) {
	t.Parallel()
	maxLen := int64(10)
	minLen := int64(2)
	got := mergeConstraints(
		&ir.Constraints{MaxLength: &maxLen},
		&ir.Constraints{MinLength: &minLen},
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
		"t/str": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/str"}, Prim: ir.PrimString},
	})

	kind, ok := g.resolvePrimKind(ir.TypeRef{Target: "t/str"})
	require.True(t, ok)
	assert.Equal(t, ir.PrimString, kind)

	_, ok = g.resolvePrimKind(ir.TypeRef{Target: "t/absent"})
	assert.False(t, ok, "a target the registry does not hold resolves to no kind")
}
