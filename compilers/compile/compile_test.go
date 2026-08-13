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
	assert.Equal(t, ir.PrimTypeID(ir.PrimString), first.Target,
		"the framework interns at the shared ID rather than deriving one of its own")
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
			types := compile.NewTypes(0)
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
	types := compile.NewTypes(0)
	types.Intern("/p", "t/x", func() ir.TypeDef { return &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x"}} })
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
	types := compile.NewTypes(0)
	node := func(id ir.TypeID) func() ir.TypeDef {
		return func() ir.TypeDef { return &ir.Model{TypeCommon: ir.TypeCommon{ID: id}} }
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
	types := compile.NewTypes(0)
	build := func() ir.TypeDef { return &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x/A"}} }
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
	types := compile.NewTypes(0)
	types.Intern("/first", "t/x/A", func() ir.TypeDef { return nil })
	require.Len(t, types.Violations(), 1, "the nil build is refused")

	types.Intern("/second", "t/x/A", func() ir.TypeDef {
		return &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/x/A"}}
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
			x.Intern("/p", "t/shared/p", func() ir.TypeDef { return &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/shared/p"}} })
			x.Register("t/shared/minted", &ir.Any{TypeCommon: ir.TypeCommon{ID: "t/shared/minted"}})
		}},
		{"source-addressed after minted", func(x *compile.Types) {
			x.Register("t/shared/minted", &ir.Any{TypeCommon: ir.TypeCommon{ID: "t/shared/minted"}})
			x.Intern("/p", "t/shared/p", func() ir.TypeDef { return &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/shared/p"}} })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			types := compile.NewTypes(0)
			tc.use(types)
			require.Len(t, types.Violations(), 1, "whichever arrives second is refused")
			assert.Contains(t, types.Violations()[0], "needs a namespace of its own")
		})
	}
}

// named returns a model at id carrying hint as its only name, for the
// provisional-naming tests below.
func named(hint string) ir.TypeDef {
	return &ir.Model{TypeCommon: ir.TypeCommon{ID: provisionalID, Name: ir.Naming{Hint: hint}}}
}

// provisionalPointer is the coordinate the provisional-naming tests below use:
// an inline position inside another declaration's body, which is the shape a
// reference can name and a declaration also owns.
const (
	provisionalPointer = "/a/items"
	// provisionalID is the ID that coordinate derives, spelled once so the helper
	// below can mint a node without every caller repeating it.
	provisionalID ir.TypeID = "t/anon/a/items"
)

// hintAt reads the hint of the node interned at provisionalPointer.
func hintAt(t *testing.T, types *compile.Types) string {
	t.Helper()
	td, ok := types.NodeAt(provisionalPointer)
	require.True(t, ok, "a node is interned at %s", provisionalPointer)
	return td.Common().Name.Hint
}

// TestTypes_DeclarationReplacesAProvisionalName is the property that makes a
// node's name independent of lowering order: a reference naming a coordinate
// inside another declaration's body builds the node, and the declaration names
// it whenever it arrives.
func TestTypes_DeclarationReplacesAProvisionalName(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes(0)

	types.InternProvisional("/a/items", "t/anon/a/items", func() ir.TypeDef {
		return named("items")
	})
	assert.Equal(t, "items", hintAt(t, types), "the reference names it first")

	types.NameFromDeclaration("/a/items", "a_item")
	assert.Equal(t, "a_item", hintAt(t, types), "the declaration replaces that name")

	// And only once: a placeholder that has been replaced is no longer one, so a
	// second declaration at the same coordinate — which claimID is what refuses —
	// cannot rename it from here.
	types.NameFromDeclaration("/a/items", "something_else")
	assert.Equal(t, "a_item", hintAt(t, types))
}

// TestTypes_NameFromDeclarationNeutralizesTheHint pins that a replacement writes
// the field the way interning writes it. NamingHint neutralizes on the way in, so
// a raw hint here would leave the node holding the caller's spelling — and the
// name would then depend on whether a reference reached the coordinate first,
// which is the dependence this path exists to remove.
//
// The two halves are asserted against each other rather than against a literal:
// what matters is that they agree, not what the grammar happens to produce.
func TestTypes_NameFromDeclarationNeutralizesTheHint(t *testing.T) {
	t.Parallel()
	const raw = "A"

	declaredFirst := compile.NewTypes(0)
	declaredFirst.Intern(provisionalPointer, provisionalID, func() ir.TypeDef {
		return &ir.Scalar{TypeCommon: ir.TypeCommon{ID: provisionalID, Name: compile.NamingHint(raw)}}
	})
	interned := hintAt(t, declaredFirst)

	referencedFirst := compile.NewTypes(0)
	referencedFirst.InternProvisional(provisionalPointer, provisionalID, func() ir.TypeDef {
		return named("placeholder")
	})
	referencedFirst.NameFromDeclaration(provisionalPointer, raw)
	replaced := hintAt(t, referencedFirst)

	assert.Equal(t, interned, replaced,
		"one hint must name the node the same whether it was interned or replaced")
	assert.NotEqual(t, raw, replaced, "and the stored hint carries no casing of its own")
}

// TestTypes_NameFromDeclarationLeavesADeclaredNameAlone holds the other
// direction: a coordinate the declaration reached first carries no placeholder,
// so a reference arriving later cannot have marked it and the name stands.
func TestTypes_NameFromDeclarationLeavesADeclaredNameAlone(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes(0)

	types.Intern("/a/items", "t/anon/a/items", func() ir.TypeDef {
		return named("a_item")
	})
	types.InternProvisional("/a/items", "t/anon/a/items", func() ir.TypeDef {
		return named("items")
	})
	assert.Equal(t, "a_item", hintAt(t, types),
		"the second call interns nothing, so it names nothing")

	types.NameFromDeclaration("/a/items", "a_item")
	assert.Equal(t, "a_item", hintAt(t, types))
}

// TestTypes_NameFromDeclarationIgnoresAnUninternedCoordinate covers the ordinary
// case: every declaration calls this at its own coordinate, and almost none of
// them are replacing anything.
func TestTypes_NameFromDeclarationIgnoresAnUninternedCoordinate(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes(0)
	require.NotPanics(t, func() { types.NameFromDeclaration("/nothing/here", "x") })
	assert.Empty(t, types.Violations())
}

// TestTypes_RefusedProvisionalInternIsNotNamed holds the guard on the marking: a
// build yielding nothing leaves no node, so there is nothing for a later
// declaration to rename and no coordinate left mapped.
func TestTypes_RefusedProvisionalInternIsNotNamed(t *testing.T) {
	t.Parallel()
	types := compile.NewTypes(0)

	types.InternProvisional("/a/items", "t/anon/a/items", func() ir.TypeDef { return nil })
	_, ok := types.NodeAt("/a/items")
	require.False(t, ok, "a refused intern leaves the coordinate unmapped")
	assert.NotEmpty(t, types.Violations())

	require.NotPanics(t, func() { types.NameFromDeclaration("/a/items", "a_item") })
}
