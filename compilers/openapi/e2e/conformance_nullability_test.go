// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func assertNullabilityFourStates(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	model, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, model.Properties, 4)
	states := openapitest.PropertiesByWire(model.Properties)
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
	model, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, model.Properties, 1)
	assert.True(t, model.Properties[0].Type.Nullable, "3.0 nullable lowers to the same IR bit")
}

func assertNullable31Ref(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	model, ok := doc.Types[componentID("Owner")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, model.Properties, 2)
	byName := openapitest.PropertiesByWire(model.Properties)

	assert.True(t, byName["p"].Type.Nullable,
		"3.1's type-array null spelling normalizes to the same IR bit at a $ref site")
	assert.Equal(t, componentID("Target"), byName["p"].Type.Target, "the ref resolves to the named component")

	assert.True(t, byName["q"].Type.Nullable,
		"a union's null branch is stripped into the ref's Nullable bit, so the ref must carry it")
	assert.Equal(t, componentID("UnionTarget"), byName["q"].Type.Target, "the ref resolves to the union component")

	union, ok := doc.Types[componentID("UnionTarget")].(*ir.Union)
	require.True(t, ok)
	assert.Len(t, union.Variants, 2, "the null branch lifts to the ref rather than becoming a variant")

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
	const branchID = ir.TypeID("type/anonymous/components/schemas/Collapsed/oneOf/0")

	collapsed, ok := doc.Types[componentID("Collapsed")].(*ir.Scalar)
	require.True(t, ok, "a {X, null} set resolves to its one branch rather than to a union node")
	require.NotNil(t, collapsed.Base)
	assert.True(t, collapsed.Base.Nullable, "the null branch lifts onto the reference")
	assert.Equal(t, branchID, collapsed.Base.Target)

	branch, ok := doc.Types[branchID]
	require.True(t, ok, "the branch declares a description, so it owns a node of its own")
	assert.Equal(t, "variant_0", branch.Common().Name.Hint,
		"the branch is named by its position in the composition, which is what a $ref to it derives too")

	ref, ok := doc.Types[componentID("BranchRef")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, ref.Base)
	assert.Equal(t, branchID, ref.Base.Target, "the outside reference reaches that same node")
}

// assertEnumNullMember holds a null enum entry to the reference: no Literal
// holds it, the Enum listing null alone has no member, a type excluding null
// leaves the entry admitting nothing (docs/compilers/openapi.md#enum-const),
// and a union branch of the golden spec's spelling of null alone is the
// union's null (docs/compilers/openapi.md#union-null-variant).
func assertEnumNullMember(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	for id, td := range doc.Types {
		if lit, ok := td.(*ir.Literal); ok {
			assert.NotEqual(t, ir.ValueNull, lit.Value.Kind, "no Literal holds the null: %s", id)
		}
	}
	mixed, ok := doc.Types[componentID("Mixed")].(*ir.Union)
	require.True(t, ok, "members of two kinds are a union")
	assert.Len(t, mixed.Variants, 2, "a variant per member that is not null")
	narrowed, ok := doc.Types[componentID("Narrowed")].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, narrowed.Members, 1)
	assert.Equal(t, "a", narrowed.Members[0].Value.String)
	assert.Equal(t, []ir.Severity{ir.SeverityWarning},
		diagsAt(diags, "openapi/degraded-construct", "/components/schemas/Narrowed/enum/1"),
		"the null its type excludes is reported at it")

	holder, ok := doc.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok)
	nullable := map[string]bool{}
	for _, property := range holder.Properties {
		nullable[property.WireName] = property.Type.Nullable
	}
	assert.Equal(t, map[string]bool{"mixed": true, "narrowed": false, "orNull": true}, nullable)
	orNull, ok := doc.Types[componentID("OrNull")].(*ir.Scalar)
	require.True(t, ok, "a branch admitting null alone is the union's null, so the union collapses")
	require.NotNil(t, orNull.Base)
	assert.Equal(t, ir.TypeRef{Target: componentID("Mixed"), Nullable: true}, *orNull.Base)

	operation, ok := operationByName(doc, "deleteZone")
	require.True(t, ok)
	body, ok := doc.Types[firstContent(t, operation).Type.Target].(*ir.Model)
	require.True(t, ok)
	for _, wire := range []string{"result", "gone"} {
		property, found := propertyByWire(body, wire)
		require.True(t, found, wire)
		assert.True(t, property.Type.Nullable, "%s admits null", wire)
		assert.Equal(t, wire == "result", property.Required, "%s keeps its own presence", wire)
		only, isEnum := doc.Types[property.Type.Target].(*ir.Enum)
		require.True(t, isEnum, "%s is an Enum, got %T", wire, doc.Types[property.Type.Target])
		assert.Empty(t, only.Members, "%s admits null and nothing else", wire)
		assert.True(t, only.Closed)
		assert.Empty(t, only.Unmodeled, "a type beside no member says nothing")
	}
}

// assertNullAlone holds every spelling of a schema admitting null and nothing
// else to one shape: a closed Enum with no member at the position, behind a
// nullable reference, where a schema admitting every value and null is the
// top type (docs/compilers/openapi.md#null-alone). A union branch of null
// alone is the union's null in each spelling, keeping what else it writes
// (docs/compilers/openapi.md#union-null-variant).
func assertNullAlone(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	holder, ok := doc.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok)
	for _, wire := range []string{"onlyNull", "listed", "enumNull", "constNull", "nullUnion", "nil", "withProps"} {
		property, found := propertyByWire(holder, wire)
		require.True(t, found, wire)
		assert.True(t, property.Type.Nullable, "%s admits null", wire)
		assert.Equal(t, wire == "onlyNull", property.Required, "%s keeps its own presence", wire)
		enum, isEnum := doc.Types[property.Type.Target].(*ir.Enum)
		require.True(t, isEnum, "%s is an Enum, got %T", wire, doc.Types[property.Type.Target])
		assert.Empty(t, enum.Members, "%s admits null and nothing else", wire)
		assert.True(t, enum.Closed, wire)
		assert.Equal(t, ir.PrimitiveAny, enum.ValueType, wire)
	}
	nested, found := propertyByWire(holder, "nil")
	require.True(t, found)
	assert.Equal(t, componentID("Nil"), nested.Type.Target, "a $ref names the component's own Enum")

	anything, found := propertyByWire(holder, "anything")
	require.True(t, found)
	assert.Equal(t, ir.TypeRef{Target: "type/primitive/any", Nullable: true}, anything.Type,
		"every value and null is the top type, told apart from null alone")

	withProperties, found := propertyByWire(holder, "withProps")
	require.True(t, found)
	assertKeptRaw(t, doc.Types[withProperties.Type.Target].Common().Unmodeled, "openapi:properties", `{"a":{"type":"string"}}`)
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/degraded-construct", "/components/schemas/Holder/properties/withProps"),
		"the property set the Enum has no home for is reported once")
	assertNullBranches(t, doc, holder, diags)
}

// assertNullBranches holds null-alone.yaml's union branches of null alone to
// the union's reference: no variant, every spelling alike, a description kept
// whole on the carrier, and a $ref to one a variant naming its declaration.
func assertNullBranches(t *testing.T, doc *ir.Document, holder *ir.Model, diags []ir.Diagnostic) {
	t.Helper()
	for _, wire := range []string{"typed", "enumed", "constant", "described"} {
		property, found := propertyByWire(holder, wire)
		require.True(t, found, wire)
		assert.Equal(t, ir.TypeRef{Target: componentID("X"), Nullable: true}, property.Type,
			"%s is a nullable reference to its one other branch", wire)
	}
	described, found := propertyByWire(holder, "described")
	require.True(t, found)
	entry := unmodeledEntry(t, described.Unmodeled, "openapi:oneOf/1")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `{"type":"null","description":"nothing"}`, string(entry.Value))
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/degraded-construct", "/components/schemas/Holder/properties/described/oneOf/1"))

	many, found := propertyByWire(holder, "many")
	require.True(t, found)
	assert.True(t, many.Type.Nullable)
	union, isUnion := doc.Types[many.Type.Target].(*ir.Union)
	require.True(t, isUnion)
	require.Len(t, union.Variants, 2, "the null branch is no variant")

	refNull, found := propertyByWire(holder, "refNull")
	require.True(t, found)
	assert.False(t, refNull.Type.Nullable, "the union's own reference says no null")
	union, isUnion = doc.Types[refNull.Type.Target].(*ir.Union)
	require.True(t, isUnion)
	require.Len(t, union.Variants, 2)
	assert.Equal(t, ir.TypeRef{Target: componentID("Nil"), Nullable: true}, union.Variants[1].Type,
		"a $ref to a schema of null alone stays a variant")
}

// assertNullableEnum31 covers 3.1's spelling of a nullable enum: `null` listed
// among the members of a null-admitting type array. The member is normalized
// onto the Nullable bit of every reference to the enum rather than degrading the
// declaration to a union of literals, so the claim is the pair: a closed Enum of
// the scalar members, and null admitted at each use. The golden cannot pin the
// strip, since the type array already carries the bit and deleting the `null`
// member moves only the source hash; the member count below does.
func assertNullableEnum31(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	enum, ok := doc.Types[componentID("Color")].(*ir.Enum)
	require.True(t, ok, "the null member does not cost the declaration its enum-ness")
	assert.True(t, enum.Closed)
	assert.Equal(t, ir.PrimitiveString, enum.ValueType, "null contributes no value type of its own")
	require.Len(t, enum.Members, 2, "the null member is normalized away, the scalar members are not")
	assert.Equal(t, "red", enum.Members[0].Value.String)
	assert.Equal(t, "green", enum.Members[1].Value.String)
	for _, diagnostic := range diags {
		assert.NotEqual(t, "openapi/degraded-construct", diagnostic.Code, "normalizing is not degrading: %+v", diagnostic)
	}

	model, ok := doc.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok)
	one, ok := propertyByWire(model, "one")
	require.True(t, ok)
	assert.Equal(t, ir.TypeRef{Target: componentID("Color"), Nullable: true}, one.Type,
		"a property $ref admits the null the members no longer carry")

	many, ok := propertyByWire(model, "many")
	require.True(t, ok)
	list, ok := doc.Types[many.Type.Target].(*ir.List)
	require.True(t, ok, "the array property hoists a List")
	assert.Equal(t, ir.TypeRef{Target: componentID("Color"), Nullable: true}, list.ElementType,
		"and so does a list element, which holds its own reference")
	assert.False(t, many.Type.Nullable, "the list itself was never declared nullable")

	operation, ok := operationByName(doc, "pick")
	require.True(t, ok)
	parameter, ok := openapitest.ParamBySource(operation, "color")
	require.True(t, ok)
	assert.Equal(t, ir.TypeRef{Target: componentID("Color"), Nullable: true}, parameter.Type,
		"a parameter reaches the same declaration through the operation walk")

	// The two spellings beside Color. A non-empty enum fixes the value space, so
	// the members decide null admission whether or not a type keyword is written
	// beside them — and when one is, the two conjoin rather than the type winning.
	bare, ok := doc.Types[componentID("BareColor")].(*ir.Enum)
	require.True(t, ok, "a bare enum listing null is still an enum of its scalar members")
	require.Len(t, bare.Members, 2, "the null member is normalized away here too")
	assert.Equal(t, ir.PrimitiveString, bare.ValueType, "the kept members supply the value type")

	narrowed, ok := doc.Types[componentID("NarrowedColor")].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, narrowed.Members, 2, "nothing to strip: the members never listed null")

	bareRef, ok := propertyByWire(model, "bare")
	require.True(t, ok)
	assert.Equal(t, ir.TypeRef{Target: componentID("BareColor"), Nullable: true}, bareRef.Type,
		"the bare spelling admits null at its uses, like the type-array spelling of the same set")

	narrowedRef, ok := propertyByWire(model, "narrowed")
	require.True(t, ok)
	assert.Equal(t, ir.TypeRef{Target: componentID("NarrowedColor"), Nullable: false}, narrowedRef.Type,
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
	model, ok := doc.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok)
	properties := openapitest.PropertiesByWire(model.Properties)
	require.Len(t, properties, 5)

	assert.Equal(t, properties["direct"].Type.Nullable, properties["viaAllOf"].Type.Nullable,
		"a sole conjunct's null survives the composition that names it")
	assert.True(t, properties["viaAllOf"].Type.Nullable)
	assert.Equal(t, properties["directModel"].Type.Nullable, properties["viaAllOfModel"].Type.Nullable,
		"a model target has no second hop to recover the null from")
	assert.True(t, properties["viaAllOfModel"].Type.Nullable)
	assert.False(t, properties["viaAllOfBoth"].Type.Nullable,
		"one conjunct forbidding null decides the conjunction")

	_, isModel := doc.Types[componentID("WrapNullableScalar")].(*ir.Model)
	assert.False(t, isModel, "a conjunct admitting no object composes nothing")
	// An allOf of one $ref saying nothing is the reference: over a target
	// admitting null, which no Base carries, the component owns the alias whose
	// Base is that use, null and all.
	alias, ok := doc.Types[componentID("WrapNullableModel")].(*ir.Scalar)
	require.True(t, ok, "WrapNullableModel is a use of NullableModel")
	require.NotNil(t, alias.Base)
	assert.Equal(t, ir.TypeRef{Target: componentID("NullableModel"), Nullable: true}, *alias.Base)
	for _, name := range []string{"WrapBoth"} {
		wrap, isModel := doc.Types[componentID(name)].(*ir.Model)
		require.True(t, isModel, "%s composes as a model", name)
		if wrap.Base != nil {
			assert.False(t, wrap.Base.Nullable, "%s.Base names a conjunct, not a usage", name)
		}
		for i, mix := range wrap.Mixins {
			assert.False(t, mix.Nullable, "%s.Mixins[%d] names a conjunct, not a usage", name, i)
		}
	}

	plain, ok := doc.Types[componentID("PlainUnion")].(*ir.Union)
	require.True(t, ok)
	distributed, ok := doc.Types[componentID("DistributedUnion")].(*ir.Union)
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
