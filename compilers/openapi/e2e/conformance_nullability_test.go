package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func assertNullabilityFourStates(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 4)
	states := openapitest.PropsByWire(m.Properties)
	assert.True(t, states["reqPlain"].Required)
	assert.False(t, states["reqPlain"].Type.Nullable)
	assert.True(t, states["reqNull"].Required)
	assert.True(t, states["reqNull"].Type.Nullable)
	assert.False(t, states["optPlain"].Required)
	assert.False(t, states["optPlain"].Type.Nullable)
	assert.False(t, states["optNull"].Required)
	assert.True(t, states["optNull"].Type.Nullable)
}

func assertNullable30(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 1)
	assert.True(t, m.Properties[0].Type.Nullable, "3.0 nullable lowers to the same IR bit")
}

func assertNullable31Ref(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("Owner")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 2)
	byName := openapitest.PropsByWire(m.Properties)

	assert.True(t, byName["p"].Type.Nullable,
		"3.1's type-array null spelling normalizes to the same IR bit at a $ref site")
	assert.Equal(t, namedID("Target"), byName["p"].Type.Target, "the ref resolves to the named component")

	assert.True(t, byName["q"].Type.Nullable,
		"a union's null branch is stripped into the ref's Nullable bit, so the ref must carry it")
	assert.Equal(t, namedID("UnionTarget"), byName["q"].Type.Target, "the ref resolves to the union component")

	u, ok := doc.Types[namedID("UnionTarget")].(*ir.Union)
	require.True(t, ok)
	assert.Len(t, u.Variants, 2, "the null branch lifts to the ref rather than becoming a variant")

	assertCollapsedBranchHint(t, doc)
}

// assertCollapsedBranchHint covers the {X, null} collapse's naming of the
// branch it keeps. The branch pointer is nameable from outside (BranchRef names
// it) and only the first lowering to reach it interns the node, so the collapse
// and an outside $ref must derive the same hint, or the document depends on
// which component is declared first (GitHub #281). The spec declares Collapsed
// before BranchRef on purpose: that is the order in which the collapse reaches
// the pointer first. The permutation half is the two-order oracle's
// (internal/harness).
func assertCollapsedBranchHint(t *testing.T, doc *ir.Document) {
	t.Helper()
	const branchID = ir.TypeID("t/anon/components/schemas/Collapsed/oneOf/0")

	collapsed, ok := doc.Types[namedID("Collapsed")].(*ir.Scalar)
	require.True(t, ok, "a {X, null} set resolves to its one branch rather than to a union node")
	require.NotNil(t, collapsed.Base)
	assert.True(t, collapsed.Base.Nullable, "the null branch lifts onto the reference")
	assert.Equal(t, branchID, collapsed.Base.Target)

	branch, ok := doc.Types[branchID]
	require.True(t, ok, "the branch declares a description, so it owns a node of its own")
	assert.Equal(t, "variant_0", branch.Common().Name.Hint,
		"the branch is named by its position in the composition, which is what a $ref to it derives too")

	ref, ok := doc.Types[namedID("BranchRef")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, ref.Base)
	assert.Equal(t, branchID, ref.Base.Target, "the outside reference reaches that same node")
}

// assertNullableEnum31 covers 3.1's spelling of a nullable enum: `null` listed
// among the members of a null-admitting type array. The member is normalized
// onto the Nullable bit of every reference to the enum rather than degrading the
// declaration to a union of literals, so the claim is the pair: a closed Enum of
// the scalar members, and null admitted at each use. The golden cannot pin the
// strip, since the type array already carries the bit and deleting the `null`
// member moves only the source hash; the member count below does.
func assertNullableEnum31(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	e, ok := doc.Types[namedID("Color")].(*ir.Enum)
	require.True(t, ok, "the null member does not cost the declaration its enum-ness")
	assert.True(t, e.Closed)
	assert.Equal(t, ir.PrimString, e.ValueType, "null contributes no value type of its own")
	require.Len(t, e.Members, 2, "the null member is normalized away, the scalar members are not")
	assert.Equal(t, "red", e.Members[0].Value.Str)
	assert.Equal(t, "green", e.Members[1].Value.Str)
	for _, d := range diags {
		assert.NotEqual(t, "openapi/degraded-construct", d.Code, "normalizing is not degrading: %+v", d)
	}

	m, ok := doc.Types[namedID("Holder")].(*ir.Model)
	require.True(t, ok)
	one, ok := propByWire(m, "one")
	require.True(t, ok)
	assert.Equal(t, ir.TypeRef{Target: namedID("Color"), Nullable: true}, one.Type,
		"a property $ref admits the null the members no longer carry")

	many, ok := propByWire(m, "many")
	require.True(t, ok)
	list, ok := doc.Types[many.Type.Target].(*ir.List)
	require.True(t, ok, "the array property hoists a List")
	assert.Equal(t, ir.TypeRef{Target: namedID("Color"), Nullable: true}, list.Elem,
		"and so does a list element, which holds its own reference")
	assert.False(t, many.Type.Nullable, "the list itself was never declared nullable")

	op, ok := opByName(doc, "pick")
	require.True(t, ok)
	p, ok := paramByName(op, "color")
	require.True(t, ok)
	assert.Equal(t, ir.TypeRef{Target: namedID("Color"), Nullable: true}, p.Type,
		"a parameter reaches the same declaration through the operation walk")

	// The two spellings beside Color. A non-empty enum fixes the value space, so
	// the members decide null admission whether or not a type keyword is written
	// beside them — and when one is, the two conjoin rather than the type winning.
	bare, ok := doc.Types[namedID("BareColor")].(*ir.Enum)
	require.True(t, ok, "a bare enum listing null is still an enum of its scalar members")
	require.Len(t, bare.Members, 2, "the null member is normalized away here too")
	assert.Equal(t, ir.PrimString, bare.ValueType, "the kept members supply the value type")

	narrowed, ok := doc.Types[namedID("NarrowedColor")].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, narrowed.Members, 2, "nothing to strip: the members never listed null")

	bareRef, ok := propByWire(m, "bare")
	require.True(t, ok)
	assert.Equal(t, ir.TypeRef{Target: namedID("BareColor"), Nullable: true}, bareRef.Type,
		"the bare spelling admits null at its uses, like the type-array spelling of the same set")

	narrowedRef, ok := propByWire(m, "narrowed")
	require.True(t, ok)
	assert.Equal(t, ir.TypeRef{Target: namedID("NarrowedColor"), Nullable: false}, narrowedRef.Type,
		"an enum excluding null must not admit it, whatever the type keyword names")
}

// assertNullabilityConjunction covers how null admission is decided for a schema
// whose keywords disagree: JSON Schema conjoins them, so a position admits null
// when something declares it and nothing takes it away. The claim is agreement:
// the same constraint written two ways reaches one Nullable bit, which is all an
// emitter reads.
//
// Base and Mixins are asserted to stay bare on purpose. They name one side of a
// conjunction, so the bit belongs to the usage that names the whole; a
// composition carrying its own would state one fact in two places that can
// disagree.
func assertNullabilityConjunction(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("Holder")].(*ir.Model)
	require.True(t, ok)
	props := openapitest.PropsByWire(m.Properties)
	require.Len(t, props, 5)

	assert.Equal(t, props["direct"].Type.Nullable, props["viaAllOf"].Type.Nullable,
		"a sole conjunct's null survives the composition that names it")
	assert.True(t, props["viaAllOf"].Type.Nullable)
	assert.Equal(t, props["directModel"].Type.Nullable, props["viaAllOfModel"].Type.Nullable,
		"a model target has no second hop to recover the null from")
	assert.True(t, props["viaAllOfModel"].Type.Nullable)
	assert.False(t, props["viaAllOfBoth"].Type.Nullable,
		"one conjunct forbidding null decides the conjunction")

	for _, name := range []string{"WrapNullableScalar", "WrapNullableModel", "WrapBoth"} {
		wrap, isModel := doc.Types[namedID(name)].(*ir.Model)
		require.True(t, isModel, "%s composes as a model", name)
		if wrap.Base != nil {
			assert.False(t, wrap.Base.Nullable, "%s.Base names a conjunct, not a usage", name)
		}
		for i, mix := range wrap.Mixins {
			assert.False(t, mix.Nullable, "%s.Mixins[%d] names a conjunct, not a usage", name, i)
		}
	}

	plain, ok := doc.Types[namedID("PlainUnion")].(*ir.Union)
	require.True(t, ok)
	distributed, ok := doc.Types[namedID("DistributedUnion")].(*ir.Union)
	require.True(t, ok)
	require.Len(t, plain.Variants, 2)
	require.Len(t, distributed.Variants, 2)
	for i := range plain.Variants {
		assert.Equal(t, plain.Variants[i].Type.Nullable, distributed.Variants[i].Type.Nullable,
			"variant %d: distributing a body that declares no type does not change what its branch admits", i)
	}
	assert.True(t, plain.Variants[0].Type.Nullable, "the nullable branch is the one that admits null")
	assert.False(t, plain.Variants[1].Type.Nullable, "and the plain one is not")
}
