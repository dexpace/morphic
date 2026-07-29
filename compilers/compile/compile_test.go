package compile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

func TestPrimTypeID_IsTheSharedScheme(t *testing.T) {
	t.Parallel()
	assert.Equal(t, ir.TypeID("t/prim/string"), compile.PrimTypeID(ir.PrimString),
		"every compiler must reach the same ID for the same primitive")
}

// TestTypes_InternIsIdempotentAndRecordsBeforeBuilding pins the property that
// terminates recursion: the coordinate is recorded before build runs, so a
// self-reference reached during build resolves instead of re-entering.
func TestTypes_InternIsIdempotentAndRecordsBeforeBuilding(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes(0)

	var seenDuringBuild bool
	id := types.Intern("/p", "t/x", func() ir.TypeDef {
		_, seenDuringBuild = types.Lookup("/p")
		return &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x"}}
	})
	assert.Equal(t, ir.TypeID("t/x"), id)
	assert.True(t, seenDuringBuild, "the coordinate resolves while build is still running")

	builds := 0
	again := types.Intern("/p", "t/other", func() ir.TypeDef { builds++; return &ir.Any{} })
	assert.Equal(t, ir.TypeID("t/x"), again, "a revisit returns the first ID")
	assert.Zero(t, builds, "and does not rebuild")
	assert.Equal(t, 1, types.Len())
}

func TestTypes_LookupAndNodeReportMisses(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes(0)

	_, ok := types.Lookup("/nope")
	assert.False(t, ok)
	_, ok = types.Node("t/nope")
	assert.False(t, ok)
}

// TestTypes_NodeAtResolvesCoordinateAndNodeTogether pins the property that makes
// the paired lookup safe: whenever a coordinate resolves, so does its node.
func TestTypes_NodeAtResolvesCoordinateAndNodeTogether(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes(0)
	types.Intern("/p", "t/x", func() ir.TypeDef { return &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x"}} })

	td, ok := types.NodeAt("/p")
	require.True(t, ok, "an interned coordinate resolves to its node in one step")
	assert.Equal(t, ir.TypeID("t/x"), td.Common().ID)

	_, ok = types.NodeAt("/never-interned")
	assert.False(t, ok, "and an uninterned coordinate reports the miss")
}

// TestTypes_RegisterTakesNoCoordinate covers the minted-node path: a composed
// union variant has no source coordinate of its own, because the pointer it
// would occupy already denotes the branch it was built from.
func TestTypes_RegisterTakesNoCoordinate(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes(0)
	types.Register("t/composed", &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/composed"}})

	td, ok := types.Node("t/composed")
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/composed"), td.Common().ID)

	_, coordinated := types.Lookup("t/composed")
	assert.False(t, coordinated, "a minted node claims no coordinate")
}

// TestTypes_PrimRefInternsOnceAndStampsSource pins that primitives are interned
// by kind rather than by position and carry the compile's source index.
func TestTypes_PrimRefInternsOnceAndStampsSource(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes(7)

	first := types.PrimRef(ir.PrimString)
	assert.Equal(t, compile.PrimTypeID(ir.PrimString), first.Target)
	assert.Equal(t, first.Target, types.PrimID(ir.PrimString), "a second reach is the same ID")
	assert.Equal(t, 1, types.Len(), "and interns nothing further")

	td, ok := types.Node(first.Target)
	require.True(t, ok)
	assert.Equal(t, 7, td.Common().Provenance.Source)
	_, coordinated := types.Lookup(string(first.Target))
	assert.False(t, coordinated, "primitives are leaves and hold no coordinate")
}

func TestTypes_RegistryIsTheLiveMap(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes(0)
	reg := types.Registry()
	types.Register("t/late", &ir.Any{})

	assert.Contains(t, reg, ir.TypeID("t/late"),
		"the Document shares the registry rather than a snapshot of it")
	assert.Contains(t, types.String(), "types: 1")
}

// TestDiags_DedupesByFullIdentity pins that the collapse is by identity and
// nothing coarser: two findings differing in any one field both survive.
func TestDiags_DedupesByFullIdentity(t *testing.T) {
	t.Parallel()
	base := ir.Diagnostic{
		Severity:   ir.SeverityError,
		Code:       "x/y",
		Message:    "m",
		Provenance: ir.Provenance{Source: 0, Pointer: "/p"},
	}
	differs := map[string]func(d *ir.Diagnostic){
		"severity":   func(d *ir.Diagnostic) { d.Severity = ir.SeverityInfo },
		"code":       func(d *ir.Diagnostic) { d.Code = "x/z" },
		"message":    func(d *ir.Diagnostic) { d.Message = "other" },
		"source":     func(d *ir.Diagnostic) { d.Provenance.Source = 1 },
		"pointer":    func(d *ir.Diagnostic) { d.Provenance.Pointer = "/q" },
		"inferredBy": func(d *ir.Diagnostic) { d.Provenance.Inferred = "guess" },
	}
	for name, mutate := range differs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var d compile.Diags
			d.Append(base)
			d.Append(base)
			require.Equal(t, 1, d.Len(), "an identical repeat collapses")

			other := base
			mutate(&other)
			d.Append(other)
			assert.Equal(t, 2, d.Len(), "a diagnostic differing in %s is a distinct finding", name)
		})
	}
}

func TestDiags_AppendAllPreservesFirstSeenOrder(t *testing.T) {
	t.Parallel()
	mk := func(code string) ir.Diagnostic {
		return ir.Diagnostic{Severity: ir.SeverityError, Code: code}
	}
	var d compile.Diags
	d.AppendAll([]ir.Diagnostic{mk("a"), mk("b"), mk("a"), mk("c")})

	got := make([]string, 0, d.Len())
	for _, x := range d.List() {
		got = append(got, x.Code)
	}
	assert.Equal(t, []string{"a", "b", "c"}, got)
}

func TestDiags_ZeroValueIsUsable(t *testing.T) {
	t.Parallel()
	var d compile.Diags
	assert.Zero(t, d.Len())
	assert.Empty(t, d.List())
}
