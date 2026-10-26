package compile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

// TestTypes_InternIsIdempotentAndRecordsBeforeBuilding pins the property that
// terminates recursion: the coordinate is recorded before build runs, so a
// self-reference reached during build resolves instead of re-entering.
func TestTypes_InternIsIdempotentAndRecordsBeforeBuilding(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()

	var seenDuringBuild bool
	id := types.Intern("/p", "t/x", func() ir.TypeDef {
		_, seenDuringBuild = types.Lookup("/p")
		return &ir.Model{ID: "t/x"}
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
	types := compile.NewTypes()

	_, ok := types.Lookup("/nope")
	assert.False(t, ok)
	_, ok = types.Node("t/nope")
	assert.False(t, ok)
}

// TestTypes_NodeAtResolvesCoordinateAndNodeTogether pins the property that makes
// the paired lookup safe: a coordinate whose build has returned resolves to its
// node in one step.
func TestTypes_NodeAtResolvesCoordinateAndNodeTogether(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	types.Intern("/p", "t/x", func() ir.TypeDef { return &ir.Model{ID: "t/x"} })

	td, ok := types.NodeAt("/p")
	require.True(t, ok, "an interned coordinate resolves to its node in one step")
	assert.Equal(t, ir.TypeID("t/x"), td.Common().ID)

	_, ok = types.NodeAt("/never-interned")
	assert.False(t, ok, "and an uninterned coordinate reports the miss")
}

// TestTypes_ANodeStillBeingBuiltIsBuildingAndNotYetANode pins the window Intern
// opens. The coordinate is recorded before build runs, so a revisit gets the ID
// back and recursion terminates, but the node exists only once build returns.
// NodeAt reports that window as a miss, and Building is what tells it apart
// from a coordinate nothing interned (GitHub #749).
func TestTypes_ANodeStillBeingBuiltIsBuildingAndNotYetANode(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	const pointer, id = "/p", ir.TypeID("t/x")

	var resolved, hasNode, building bool
	types.Intern(pointer, id, func() ir.TypeDef {
		_, resolved = types.Lookup(pointer)
		_, hasNode = types.NodeAt(pointer)
		building = types.Building(id)
		return &ir.Model{ID: id}
	})
	assert.True(t, resolved, "the coordinate resolves while its build runs")
	assert.False(t, hasNode, "but it has no node to read yet")
	assert.True(t, building, "and Building says why")

	_, ok := types.NodeAt(pointer)
	assert.True(t, ok, "once build returns, the node is there")
	assert.False(t, types.Building(id), "and the build is over")
}

// TestTypes_BuildingHoldsOnlyDuringABuild holds Building to that window. A
// coordinate never interned, a node minted without one, and a build refused
// for yielding nothing all report false.
func TestTypes_BuildingHoldsOnlyDuringABuild(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	assert.False(t, types.Building("t/never"), "nothing interned it")

	types.Register("t/minted", &ir.Model{ID: "t/minted"})
	assert.False(t, types.Building("t/minted"), "a minted node has no build window")

	types.Intern("/refused", "t/refused", func() ir.TypeDef { return nil })
	assert.False(t, types.Building("t/refused"), "a refused intern leaves nothing behind")
}

// TestTypes_RegisterTakesNoCoordinate covers the minted-node path: a composed
// union variant has no source coordinate of its own, because the pointer it
// would occupy already denotes the branch it was built from.
func TestTypes_RegisterTakesNoCoordinate(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	types.Register("t/composed", &ir.Model{ID: "t/composed"})

	td, ok := types.Node("t/composed")
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/composed"), td.Common().ID)

	_, coordinated := types.Lookup("t/composed")
	assert.False(t, coordinated, "a minted node claims no coordinate")
}

// TestTypes_PrimRefInternsOnceAndNamesNoSource pins that primitives are interned
// by kind rather than by position, and that one shared by every position of its
// kind claims no source (GitHub #528).
func TestTypes_PrimRefInternsOnceAndNamesNoSource(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()

	first := types.PrimRef(ir.PrimString)
	assert.Equal(t, ir.PrimTypeID(ir.PrimString), first.Target,
		"the framework interns at the shared ID rather than deriving one of its own")
	assert.Equal(t, first.Target, types.PrimID(ir.PrimString), "a second reach is the same ID")
	assert.Equal(t, 1, types.Len(), "and interns nothing further")

	td, ok := types.Node(first.Target)
	require.True(t, ok)
	assert.Equal(t, ir.Provenance{Source: ir.NoSource}, td.Common().Provenance)
	_, coordinated := types.Lookup(string(first.Target))
	assert.False(t, coordinated, "primitives are leaves and hold no coordinate")
}

func TestTypes_RegistryIsTheLiveMap(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	reg, _ := types.Finish(t.Context())
	types.Register("t/late", &ir.Any{})

	assert.Contains(t, reg, ir.TypeID("t/late"),
		"the Document shares the registry rather than a snapshot of it")
}

// TestTypes_RefusesEntriesTheRegistryCannotHold covers the boundary this type
// exists to hold. It owns the registry's invariants, so it is also the one place
// able to manufacture the malformed states irverify and pass.Validate report —
// an empty ID or a nil type definition. Neither is producible from any source, so
// both are compiler bugs, and refusing them silently would hide the bug rather
// than the symptom.
func TestTypes_RefusesEntriesTheRegistryCannotHold(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*compile.Types){
		"empty register id": func(x *compile.Types) { x.Register("", &ir.Any{}) },
		"nil node":          func(x *compile.Types) { x.Register("t/x", nil) },
		"typed nil node":    func(x *compile.Types) { x.Register("t/x", (*ir.Model)(nil)) },
		"empty intern id":   func(x *compile.Types) { x.Intern("/p", "", func() ir.TypeDef { return &ir.Any{} }) },
		"empty coordinate":  func(x *compile.Types) { x.Intern("", "t/x", func() ir.TypeDef { return &ir.Any{} }) },
		"nil build":         func(x *compile.Types) { x.Intern("/p", "t/x", nil) },
		"build returns nil": func(x *compile.Types) { x.Intern("/p", "t/x", func() ir.TypeDef { return nil }) },
		"build returns typed nil": func(x *compile.Types) {
			x.Intern("/p", "t/x", func() ir.TypeDef { return (*ir.Union)(nil) })
		},
	}
	for name, attempt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			types := compile.NewTypes()
			attempt(types)

			assert.Zero(t, types.Len(), "nothing malformed reaches the registry")
			assert.Len(t, types.Violations(), 1, "and the refusal is recorded, not swallowed")
			_, coordinated := types.Lookup("/p")
			assert.False(t, coordinated,
				"a refused intern leaves no coordinate resolving to a node that is not there")
		})
	}
}

func TestTypes_ViolationsIsEmptyForLegitimateEntries(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	types.Intern("/p", "t/x", func() ir.TypeDef { return &ir.Model{ID: "t/x"} })
	types.Register("t/composed", &ir.Any{})

	assert.Equal(t, 2, types.Len())
	assert.Empty(t, types.Violations())
}

// TestTypes_RefusesADerivationThatCollapsesTwoCoordinates covers invariant 3's
// injectivity half: two distinct source coordinates must not derive one ID.
//
// Nothing downstream can see this happen. Intern keys by coordinate, so both
// pointers map cleanly and the second node overwrites the first in the registry.
// The survivor is well-formed, carries its own provenance, and satisfies every
// structural check — including irverify's ID-to-provenance agreement, which the
// node that was overwritten is no longer present to fail.
func TestTypes_RefusesADerivationThatCollapsesTwoCoordinates(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	node := func(id ir.TypeID) func() ir.TypeDef {
		return func() ir.TypeDef { return &ir.Model{ID: id} }
	}
	types.Intern("/components/schemas/A~1B", "t/openapi/collapsed", node("t/openapi/collapsed"))
	types.Intern("/components/schemas/A/B", "t/openapi/collapsed", node("t/openapi/collapsed"))

	require.Len(t, types.Violations(), 1, "the second coordinate claiming the ID is refused")
	assert.Contains(t, types.Violations()[0], "collapses two coordinates")
	assert.Contains(t, types.Violations()[0], "/components/schemas/A~1B", "the message names both derivations")
	assert.Contains(t, types.Violations()[0], "/components/schemas/A/B")
}

// TestTypes_ReinterningOneCoordinateIsNotACollision is the control. Intern is
// the interning table, not a constructor: a second visit to the same coordinate
// returns the first ID and is the mechanism that terminates recursive schemas,
// so it must not be mistaken for two coordinates colliding.
func TestTypes_ReinterningOneCoordinateIsNotACollision(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	build := func() ir.TypeDef { return &ir.Model{ID: "t/x/A"} }
	types.Intern("/p", "t/x/A", build)
	types.Intern("/p", "t/x/A", build)

	assert.Empty(t, types.Violations(), "one coordinate visited twice is a revisit, not a collision")
	assert.Equal(t, 1, types.Len())
}

// TestTypes_RefusedInternReleasesItsID pins that a build returning nothing frees
// the ID it had claimed. Leaving it claimed would report the *next* coordinate
// that legitimately derives it as a collision — a second, misleading violation
// caused by the first.
func TestTypes_RefusedInternReleasesItsID(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes()
	types.Intern("/first", "t/x/A", func() ir.TypeDef { return nil })
	require.Len(t, types.Violations(), 1, "the nil build is refused")

	types.Intern("/second", "t/x/A", func() ir.TypeDef {
		return &ir.Model{ID: "t/x/A"}
	})
	assert.Len(t, types.Violations(), 1, "no second violation: the refused intern left no claim behind")
	assert.Equal(t, 1, types.Len())
}

// TestTypes_RefusesANamespaceUsedBothWays pins invariant 3's corollary, the half
// claimID does not cover: a namespace holding both minted and source-addressed
// nodes leaves the two racing for one ID, with declaration order deciding.
func TestTypes_RefusesANamespaceUsedBothWays(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		use  func(*compile.Types)
	}{
		{"minted after source-addressed", func(x *compile.Types) {
			x.Intern("/p", "t/shared/p", func() ir.TypeDef { return &ir.Model{ID: "t/shared/p"} })
			x.Register("t/shared/minted", &ir.Any{ID: "t/shared/minted"})
		}},
		{"source-addressed after minted", func(x *compile.Types) {
			x.Register("t/shared/minted", &ir.Any{ID: "t/shared/minted"})
			x.Intern("/p", "t/shared/p", func() ir.TypeDef { return &ir.Model{ID: "t/shared/p"} })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			types := compile.NewTypes()
			tc.use(types)
			require.Len(t, types.Violations(), 1, "whichever arrives second is refused")
			assert.Contains(t, types.Violations()[0], "needs a namespace of its own")
		})
	}
}
