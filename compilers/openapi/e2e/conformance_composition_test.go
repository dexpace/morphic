// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

func assertAllOfInheritance(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	d, ok := doc.Types[componentID("Derived")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, d.Base, "sole allOf $ref becomes Base")
	assert.Equal(t, componentID("Base"), d.Base.Target)
	assert.Empty(t, d.Mixins)
	_, ok = propByWire(d, "extra")
	assert.True(t, ok, "property declared alongside allOf survives")
	// Model-level detail declared alongside allOf must be captured, not dropped.
	assert.Equal(t, "A Base specialized with one extra field.", d.Docs.Description,
		"composed schema keeps its description")
	assert.NotNil(t, d.Deprecation, "composed schema keeps deprecated")
	assert.Equal(t, ir.AdditionalClosed, d.Additional,
		"composed schema keeps additionalProperties: false")
	assert.Contains(t, d.Unmodeled, "openapi:x-team", "composed schema keeps x-* extensions")
}

func assertAllOfMixins(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	c, ok := doc.Types[componentID("C")].(*ir.Model)
	require.True(t, ok)
	assert.Nil(t, c.Base, "multiple non-hierarchy refs stay Mixins, none is Base")
	require.Len(t, c.Mixins, 2)
	assert.Equal(t, componentID("A"), c.Mixins[0].Target)
	assert.Equal(t, componentID("B"), c.Mixins[1].Target)
}

func assertAllOfInlineMerge(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[componentID("Merged")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, m.Base)
	assert.Equal(t, componentID("Base"), m.Base.Target)
	_, ok = propByWire(m, "name")
	assert.True(t, ok, "inline allOf branch contributes its properties")
}

// assertAllOfNestedInline pins the flattening of
// docs/compilers/openapi.md#allof-classification: a nested spelling classifies
// as the flat one does and keeps each property's own pointer. A wrapper writing
// nothing else is kept nowhere; one that also writes a keyword is kept whole,
// with one diagnostic.
func assertAllOfNestedInline(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	m, ok := doc.Types[componentID("M")].(*ir.Model)
	require.True(t, ok)
	assert.Nil(t, m.Base, "two refs and no hierarchy: none is Base")
	require.Len(t, m.Mixins, 2)
	assert.Equal(t, componentID("A"), m.Mixins[0].Target)
	assert.Equal(t, componentID("B"), m.Mixins[1].Target)
	assert.Empty(t, m.Unmodeled, "the wrapper is flattened, not kept verbatim")

	n, ok := doc.Types[componentID("Nested2")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, n.Base, "the sole ref two wrappers down is the Base")
	assert.Equal(t, componentID("A"), n.Base.Target)
	c, ok := propByWire(n, "c")
	require.True(t, ok)
	assert.Equal(t, jsontext.Pointer("/components/schemas/Nested2/allOf/0/allOf/1/properties/c"),
		c.Provenance.Pointer)

	mixed, ok := doc.Types[componentID("Mixed")].(*ir.Model)
	require.True(t, ok)
	mc, ok := propByWire(mixed, "c")
	require.True(t, ok)
	assert.True(t, mc.Required, "the wrapper's required list reaches the property inside it")
	wires := make([]string, 0, len(mixed.Properties))
	for _, p := range mixed.Properties {
		wires = append(wires, p.WireName)
	}
	assert.Equal(t, []string{"c", "w"}, wires, "the wrapper's own property merges after its branches'")

	closed, ok := doc.Types[componentID("Closed")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.AdditionalClosed, closed.Additional, "a false inside a wrapper still closes")
	assert.Contains(t, closed.Unmodeled, "openapi:allOf/0/allOf/0")
	assert.NotContains(t, closed.Unmodeled, "openapi:allOf/0")

	described, ok := doc.Types[componentID("Described")].(*ir.Model)
	require.True(t, ok)
	assert.Nil(t, described.Base)
	require.Len(t, described.Mixins, 2, "the wrapper's refs compose despite its description")
	assert.Equal(t, componentID("A"), described.Mixins[0].Target)
	assert.Equal(t, componentID("B"), described.Mixins[1].Target)
	assert.Contains(t, described.Unmodeled, "openapi:allOf/0", "the wrapper is kept whole")
	assert.Equal(t, "grouped", described.Docs.Description, "and what it says of the position lands there")

	for _, id := range []ir.TypeID{"t/composed/components/schemas/U/oneOf/0", "t/composed/components/schemas/U/oneOf/1"} {
		variant, ok := doc.Types[id].(*ir.Model)
		require.True(t, ok, "%s should be a composed variant", id)
		assert.Equal(t, ir.AdditionalClosed, variant.Additional, "%s: a false nested beside a oneOf closes every variant", id)
		assert.Contains(t, variant.Unmodeled, "openapi:allOf/0/allOf/2/allOf/0", "%s keeps the false at its path", id)
		a, ok := propByWire(variant, "a")
		require.True(t, ok, id)
		assert.True(t, a.Required, "%s: the wrapper's required list reaches the variant's property", id)
	}

	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Provenance.Pointer == "/components/schemas/Described/allOf/0" {
			continue // the one kept wrapper
		}
		assert.NotEqual(t, diag.DegradedConstruct, d.Code, "flattening is exact: %+v", d)
	}
}

func assertAllOfRequiredOnly(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	d, ok := doc.Types[componentID("Derived")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, d.Base, "the $ref branch still becomes Base")
	name, ok := propByWire(d, "name")
	require.True(t, ok, "the inline branch's own property survives")
	assert.True(t, name.Required,
		"a required-only allOf branch attaches across the whole composition, not just its own properties map (issue #29)")
}

// assertRequiredMembers pins where a `required` name lands when no property of
// the object it is written for declares it
// (docs/ir/models.md#required-members): in Model.Requires, once, beside the
// Required on the properties it does name. The four shapes are four ways an
// object lowers: a plain object, an allOf making a property its base declares
// required, an object whose catch-all governs the name, which stays a Model
// rather than becoming a map since a map requires no key, and the object
// variant of a multi-typed schema.
func assertRequiredMembers(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	plain, ok := doc.Types[componentID("Plain")].(*ir.Model)
	require.True(t, ok)
	id, ok := propByWire(plain, "id")
	require.True(t, ok)
	assert.True(t, id.Required, "a declared name is Required on its property")
	assert.Equal(t, []string{"ghost"}, plain.Requires, "an undeclared one is listed, and the declared one is not")

	created, ok := doc.Types[componentID("CreatedUser")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, created.Base)
	assert.Equal(t, componentID("User"), created.Base.Target)
	assert.Empty(t, created.Properties, "the base declares id, so the composed model has no property for it")
	assert.Equal(t, []string{"id"}, created.Requires, "the inherited property it makes required")
	user, ok := doc.Types[componentID("User")].(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, user.Requires, "the base is not made to require what only its subtype does")
	userID, ok := propByWire(user, "id")
	require.True(t, ok)
	assert.False(t, userID.Required, "and its own property stays optional")

	tagged, ok := doc.Types[componentID("Tagged")].(*ir.Model)
	require.True(t, ok, "a required name beside a catch-all keeps the object a Model, not a map")
	assert.NotNil(t, tagged.AdditionalProps)
	assert.Empty(t, tagged.Properties)
	assert.Equal(t, []string{"a"}, tagged.Requires, "required only because the catch-all governs it")
	_, isMap := ir.AsMap(tagged)
	assert.False(t, isMap, "AsMap keeps a model that requires a member a Model")

	either, ok := doc.Types[componentID("Either")].(*ir.Union)
	require.True(t, ok)
	var object *ir.Model
	for _, v := range either.Variants {
		if m, isModel := doc.Types[v.Type.Target].(*ir.Model); isModel {
			object = m
		}
	}
	require.NotNil(t, object, "the object variant is a Model")
	variantID, ok := propByWire(object, "id")
	require.True(t, ok)
	assert.True(t, variantID.Required)
	assert.Equal(t, []string{"extra"}, object.Requires, "the variant lists what it declares nowhere")
}

// assertAllOfOneOfCooccurrence pins both halves of the co-declared composition
// rule: docs/ir/models.md#composition-beside-a-union distributes a union whose
// branches all name referents, and the rule
// docs/compilers/openapi.md#union-beside-structure keeps one with an inline
// branch verbatim rather than distributing it halfway. The verbatim half is
// covered over a model body, which owns a node already, and over a scalar one,
// which does not — there the alias hoisted for the union carries what the
// position wrote beside it too. The outside reference to a branch pointer pins
// the third thing: a composed variant is Morphic's own node, so it cannot be
// taken by, or take from, a $ref.
func assertAllOfOneOfCooccurrence(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	combo, ok := doc.Types[componentID("Combo")].(*ir.Union)
	require.True(t, ok, "the union is the value, not a preserved sibling")
	require.Len(t, combo.Variants, 2)
	for i, branch := range []string{"A", "B"} {
		v, ok := doc.Types[combo.Variants[i].Type.Target].(*ir.Model)
		require.True(t, ok, "variant %d composes as a model", i)
		require.NotNil(t, v.Base, "the allOf composition rides on every variant")
		assert.Equal(t, componentID("Base"), v.Base.Target)
		require.Len(t, v.Mixins, 1)
		assert.Equal(t, componentID(branch), v.Mixins[0].Target)
		require.NotNil(t, v.Constraints, "the enclosing schema's own minProperties rides on every variant too (GitHub #407)")
		require.NotNil(t, v.Constraints.MinProps)
		assert.Equal(t, int64(2), *v.Constraints.MinProps)
	}

	mixed, ok := doc.Types[componentID("MixedKinds")].(*ir.Model)
	require.True(t, ok, "the structural body survives")
	entry, ok := mixed.Unmodeled["openapi:oneOf"]
	require.True(t, ok, "and the union it could not absorb survives beside it")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)

	bounded, ok := doc.Types[componentID("BoundedKinds")].(*ir.Scalar)
	require.True(t, ok, "a body that is not a model reduces to a shared primitive and hoists an alias")
	entry, ok = bounded.Unmodeled["openapi:oneOf"]
	require.True(t, ok, "which is the node the kept union sits on")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	require.NotNil(t, bounded.Constraints, "and the bounds written beside the union sit on it too")
	require.NotNil(t, bounded.Constraints.MinLength)
	assert.Equal(t, int64(3), *bounded.Constraints.MinLength)

	outsider, ok := doc.Types[componentID("Outsider")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, outsider.Properties, 1)
	branchNode, ok := doc.Types[outsider.Properties[0].Type.Target].(*ir.Scalar)
	require.True(t, ok, "the branch pointer hoists an alias of what the branch references")
	require.NotNil(t, branchNode.Base)
	assert.Equal(t, componentID("A"), branchNode.Base.Target,
		"the reference gets the branch schema, never the variant composed for that branch")

	assertInlineBranchHint(t, doc)
}

// assertInlineBranchHint pins the hint an inline composition branch takes when
// an outside reference names its pointer too. The union reaches that node
// through its composition, and the reference builds it (hoistSubSchema) only
// where nothing else did, under the hint the composition recorded. An inline
// branch has only its position to be named by, and the reference path once used
// the bare ordinal "0" (GitHub #181). The value is asserted because the golden
// records whichever hint won without saying the two agree, and the corpus
// carries the shape so the two-order oracle covers it.
func assertInlineBranchHint(t *testing.T, doc *ir.Document) {
	t.Helper()
	const branchID = ir.TypeID("t/anon/components/schemas/InlineHost/oneOf/0")

	host, ok := doc.Types[componentID("InlineHost")].(*ir.Union)
	require.True(t, ok, "the inline union is the value")
	require.Len(t, host.Variants, 2)
	assert.Equal(t, branchID, host.Variants[0].Type.Target,
		"the union's first variant is the node the branch pointer owns")

	branch, ok := doc.Types[branchID]
	require.True(t, ok, "the branch pointer owns a node")
	assert.Equal(t, "variant_0", branch.Common().Name.Hint,
		"named by position, which is all an inline branch has to be named by")

	outsider, ok := doc.Types[componentID("InlineOutsider")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, outsider.Properties, 1)
	assert.Equal(t, branchID, outsider.Properties[0].Type.Target,
		"the outside reference resolves to that same node rather than hoisting a second")
}

// assertAllOfPositionConstraints pins GitHub #407: a value constraint written
// beside an allOf composing position reaches the same ir.Model.Constraints
// field a plain object's does, rather than falling to Unmodeled under a
// diagnostic that claims the node has no home for it — the node demonstrably
// does, since ViaModel's sibling proves the field exists and is read.
func assertAllOfPositionConstraints(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	viaModel, ok := doc.Types[componentID("ViaModel")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, viaModel.Constraints)
	require.NotNil(t, viaModel.Constraints.MinProps)
	assert.Equal(t, int64(2), *viaModel.Constraints.MinProps)

	viaAllOf, ok := doc.Types[componentID("ViaAllOf")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, viaAllOf.Base, "the sole allOf $ref still becomes Base")
	assert.Equal(t, componentID("Base"), viaAllOf.Base.Target)
	require.NotNil(t, viaAllOf.Constraints, "the composing position's own minProperties reaches the model it built, exactly as a plain object's does")
	require.NotNil(t, viaAllOf.Constraints.MinProps)
	assert.Equal(t, int64(2), *viaAllOf.Constraints.MinProps)
	assert.NotContains(t, viaAllOf.Unmodeled, "openapi:minProperties",
		"minProperties has a home now, so it is not also kept verbatim")
	assert.Empty(t, diagsAt(diags, diag.DegradedConstruct, "/components/schemas/ViaAllOf"),
		"and no diagnostic claims the node has no home for it")
}

// assertAllOfRefinement pins docs/compilers/openapi.md#allof-refinement: an allOf
// no conjunct can make an object lowers as a use of the conjunct deciding it, at
// every kind of position, and one that can stays a composition.
func assertAllOfRefinement(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	zone, ok := doc.Types[componentID("Zone")].(*ir.Model)
	require.True(t, ok)
	props := openapitest.PropsByWire(zone.Properties)

	owner := props["owner"]
	decorator, ok := doc.Types[owner.Type.Target].(*ir.Model)
	require.True(t, ok, "an object target keeps its composition")
	assert.Equal(t, "The owner", decorator.Docs.Description)
	assert.Empty(t, decorator.Unmodeled, "nothing is left of the branch")
	assert.True(t, owner.Type.Nullable)
	assert.NotEmpty(t, owner.Type.Inferred, "the policy reads the null")
	assert.Equal(t, []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}, owner.Visibility.Only)

	zoneID := props["zone_id"]
	assert.Equal(t, componentID("Identifier"), zoneID.Type.Target, "the position is a use of the string")
	assert.Equal(t, "The zone", zoneID.Docs.Description)
	assert.Equal(t, "An identifier", zoneID.Docs.Summary, "the target speaks where the use does not")
	require.NotNil(t, zoneID.Constraints)
	assert.Equal(t, new(int64(32)), zoneID.Constraints.MaxLength)
	assert.NotEmpty(t, zoneID.Visibility.Only, "what binds a use comes down from the target")

	assert.Equal(t, componentID("Port"), props["port"].Type.Target)
	assert.Empty(t, props["port"].Unmodeled, "a restated type keeps nothing")
	assert.Equal(t, componentID("Identifier"), props["typed"].Type.Target)
	assert.Len(t, props["typed"].Examples, 1)

	mode := props["mode"]
	narrowed, ok := doc.Types[mode.Type.Target].(*ir.Enum)
	require.True(t, ok, "the enum is the value set")
	assert.Len(t, narrowed.Members, 1)
	assert.Equal(t, "What to do", mode.Docs.Description, "and the $ref it narrows still speaks")
	unmodeledEntry(t, mode.Unmodeled, "openapi:allOf/0")

	assert.Equal(t, componentID("Port"), props["width"].Type.Target, "a $ref beats a narrower inline type")
	unmodeledEntry(t, props["width"].Unmodeled, "openapi:allOf/1")

	scan := props["scan"]
	assert.Equal(t, "A scan", scan.Docs.Description)
	assert.NotEmpty(t, scan.Visibility.Only, "a use reads through a refinement to what it refines")

	assertRefinedOperation(t, doc)
	assertRefinedNodes(t, doc)
	assert.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
		"only Disjoint's branches are contradicted")
	assert.Equal(t, []jsontext.Pointer{"/components/schemas/Disjoint/allOf/0", "/components/schemas/Disjoint/allOf/1"},
		warningPointers(diags))
}

// assertRefinedOperation covers the positions an operation holds.
func assertRefinedOperation(t *testing.T, doc *ir.Document) {
	t.Helper()
	op := openapitest.FindOp(t, doc, "uploadToZone")
	param, ok := openapitest.ParamBySource(op, "zone_id")
	require.True(t, ok)
	assert.Equal(t, componentID("Identifier"), param.Type.Target)
	assert.Equal(t, "The zone", param.Docs.Description)
	require.NotNil(t, param.Constraints)
	assert.Equal(t, new(int64(32)), param.Constraints.MaxLength)

	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	assert.NotNil(t, op.Request.Contents[0].File, "a refinement of a binary string is a file body")

	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)
	assert.Equal(t, componentID("Port"), op.Responses[0].Headers[0].Type.Target)
	require.NotNil(t, op.Responses[0].Payload)
	alias, ok := doc.Types[op.Responses[0].Payload.Contents[0].Type.Target].(*ir.Scalar)
	require.True(t, ok, "a content has no carrier, so its annotation hoists an alias")
	require.NotNil(t, alias.Base)
	assert.Equal(t, componentID("Identifier"), alias.Base.Target)
	assert.Equal(t, "The new zone", alias.Docs.Description)
}

// assertRefinedNodes covers the components and the positions no carrier holds.
func assertRefinedNodes(t *testing.T, doc *ir.Document) {
	t.Helper()
	scores, ok := doc.Types[componentID("Scores")].(*ir.List)
	require.True(t, ok)
	elem, ok := doc.Types[scores.Elem.Target].(*ir.Scalar)
	require.True(t, ok, "an element's annotation hoists an alias")
	assert.Equal(t, "A score", elem.Docs.Description)

	variants, ok := doc.Types[componentID("Variants")].(*ir.Union)
	require.True(t, ok)
	require.Len(t, variants.Variants, 2)
	assert.Equal(t, componentID("Identifier"), variants.Variants[0].Type.Target)
	assert.Equal(t, "identifier", variants.Variants[0].Name.Hint)
	assert.Equal(t, "port", variants.Variants[1].Name.Hint)

	for _, name := range []string{"Prim", "PrimSwapped"} {
		prim, ok := doc.Types[componentID(name)].(*ir.Scalar)
		require.True(t, ok, "%s is a scalar, not an empty model", name)
		require.NotNil(t, prim.Base)
		assert.Equal(t, ir.PrimTypeID(ir.PrimInt32), prim.Base.Target, name)
	}
	bounded, ok := doc.Types[componentID("Bounded")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, bounded.Constraints)
	assert.Equal(t, new(int64(3)), bounded.Constraints.MaxLength)

	disjoint, ok := doc.Types[componentID("Disjoint")].(*ir.Model)
	require.True(t, ok, "no value satisfies it, so nothing refines")
	unmodeledEntry(t, disjoint.Unmodeled, "openapi:allOf/0")
	unmodeledEntry(t, disjoint.Unmodeled, "openapi:allOf/1")
}

// warningPointers returns where each warning is reported, in order.
func warningPointers(diags []ir.Diagnostic) []jsontext.Pointer {
	var out []jsontext.Pointer
	for _, d := range diags {
		if d.Severity == ir.SeverityWarning {
			out = append(out, d.Provenance.Pointer)
		}
	}
	slices.Sort(out)
	return out
}

func assertOneOfDiscriminated(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	pet, ok := doc.Types[componentID("Pet")].(*ir.Union)
	require.True(t, ok, "oneOf survives as a Union node, never collapsed")
	assert.True(t, pet.Exclusive)
	assert.True(t, pet.WireTagged, "the discriminator's tag names the variant")
	require.Len(t, pet.Variants, 2)
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, "petType", pet.Discriminator.PropertyName)
	assert.Equal(t, componentID("Cat"), pet.Discriminator.Mapping["cat"])
	assert.Equal(t, componentID("Dog"), pet.Discriminator.Mapping["dog"])
}

// assertAnyOfDiscriminated pins that the tag mode and Exclusive are independent
// (docs/ir/unions.md): a discriminator on an anyOf tags the union without
// making it exclusive, since a payload may still satisfy both branches.
func assertAnyOfDiscriminated(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	u, ok := doc.Types[componentID("Resource")].(*ir.Union)
	require.True(t, ok, "anyOf survives as a Union node, never collapsed")
	assert.False(t, u.Exclusive, "anyOf admits a payload both branches satisfy")
	assert.True(t, u.WireTagged, "the discriminator's tag names the variant")
	require.Len(t, u.Variants, 2)
	require.NotNil(t, u.Discriminator)
	assert.Equal(t, "kind", u.Discriminator.PropertyName)
	assert.Equal(t, componentID("Named"), u.Discriminator.Mapping["named"])
	assert.Equal(t, componentID("Labelled"), u.Discriminator.Mapping["labelled"])
}

// assertDiscriminatorInheritance covers the model-hierarchy form of a
// discriminator, which the union form (oneof-discriminated) does not reach: the
// tag is declared on the base, each subtype composes the base by $ref, and the
// wire value each subtype answers to is recorded on the subtype.
//
// Both spellings of that value are pinned, because they come from different
// places: a subtype the base's mapping names takes the mapping key, and one the
// mapping omits takes OpenAPI's implicit schema name.
func assertDiscriminatorInheritance(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok, "the base is a Model, not a Union")
	require.NotNil(t, pet.Discriminator)
	tag, ok := propByWire(pet, "petType")
	require.True(t, ok, "the base declares the tag property itself")
	assert.Equal(t, tag.ID, pet.Discriminator.Property,
		"a declared tag property binds by identity, not by wire name")
	assert.Empty(t, pet.Discriminator.PropertyName,
		"the name spelling is for a tag no property declares")
	assert.Equal(t, componentID("Cat"), pet.Discriminator.Mapping["cat"])

	for name, value := range map[string]string{"Cat": "cat", "Dog": "Dog"} {
		sub, ok := doc.Types[componentID(name)].(*ir.Model)
		require.True(t, ok, "%s composes as a Model", name)
		require.NotNil(t, sub.Base, "the discriminated base becomes Base, not a Mixin")
		assert.Equal(t, componentID("Pet"), sub.Base.Target)
		assert.Equal(t, value, sub.DiscriminatorValue)
		assert.Nil(t, sub.Discriminator, "a subtype does not restate its base's discriminator")
	}
}

// assertDiscriminatorInlineSubtype covers the subtypes OpenAPI's implicit
// mapping cannot name (GitHub #517). The implicit tag is the name of the
// component schema that declares a subtype, so one declared inline — as a
// property or a union branch — takes no tag, while it still composes the
// discriminated base. Kennel's Dog shares its key with the component Dog on
// purpose: a tag read off the pointer gives both the same value.
func assertDiscriminatorInlineSubtype(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok, "the base is a Model")
	require.NotNil(t, pet.Discriminator)
	assert.Empty(t, pet.Discriminator.Mapping, "every tag in this document is implicit")

	for _, tc := range []struct {
		id    ir.TypeID
		value string
	}{
		{componentID("Dog"), "Dog"},
		{"t/anon/components/schemas/Kennel/properties/Dog", ""},
		{"t/anon/components/schemas/Kennel/properties/pet", ""},
		{"t/anon/components/schemas/Either/oneOf/0", ""},
	} {
		sub, ok := doc.Types[tc.id].(*ir.Model)
		require.True(t, ok, "%s composes as a Model", tc.id)
		require.NotNil(t, sub.Base, "%s composes the discriminated base", tc.id)
		assert.Equal(t, componentID("Pet"), sub.Base.Target)
		assert.Equal(t, tc.value, sub.DiscriminatorValue,
			"%s: only a component schema has a name to answer to", tc.id)
	}
}

// assertDiscriminatorTransitive covers a hierarchy deeper than the two levels
// discriminator-inheritance reaches: the subtype's own allOf branch names an
// intermediate schema that declares no discriminator, so the tag value can only
// come from an ancestor further up (GitHub #305). What each subtype composes is
// pinned beside its tag, since depth is the point. The undiscriminated chain
// beside them is the boundary: depth alone must not manufacture a value. Puppy
// is named by the mapping, routing a wire value straight onto a grandchild;
// Whelp is named by nothing and answers to its own schema name three hops down.
func assertDiscriminatorTransitive(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok, "the root of the hierarchy is a Model")
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, componentID("Dog"), pet.Discriminator.Mapping["dog"])
	assert.Equal(t, componentID("Puppy"), pet.Discriminator.Mapping["puppy"],
		"the base maps a wire value straight onto its grandchild")

	// base is the schema each subtype's own allOf branch names, which is the only
	// thing a one-hop reading of the chain could see.
	for _, tc := range []struct{ name, base, value string }{
		{"Dog", "Pet", "dog"},
		{"Puppy", "Dog", "puppy"},
		{"Whelp", "Puppy", "Whelp"},
	} {
		sub, ok := doc.Types[componentID(tc.name)].(*ir.Model)
		require.True(t, ok, "%s composes as a Model", tc.name)
		require.NotNil(t, sub.Base, "%s composes a base", tc.name)
		assert.Equal(t, componentID(tc.base), sub.Base.Target,
			"%s composes %s, not the schema anchoring the hierarchy", tc.name, tc.base)
		assert.Equal(t, tc.value, sub.DiscriminatorValue,
			"%s answers to the tag its ancestor spells for it", tc.name)
		assert.Nil(t, sub.Discriminator, "a subtype does not restate the hierarchy's discriminator")
	}

	for _, name := range []string{"Shrub", "Sapling"} {
		sub, ok := doc.Types[componentID(name)].(*ir.Model)
		require.True(t, ok, "%s composes as a Model", name)
		require.NotNil(t, sub.Base, "%s composes a base", name)
		assert.Empty(t, sub.DiscriminatorValue,
			"%s has no discriminated ancestor, so walking the chain finds no tag", name)
	}
}

// assertDiscriminatorAliasMapping covers a mapping that spells two keys for one
// subtype (GitHub #410). Model.DiscriminatorValue holds one value and a mapping
// is unordered, so the subtype takes the smallest key in byte order whichever
// was written first — the fixture writes the larger first — while the base's
// mapping keeps both, and the election is reported at the subtype as
// information since nothing is lost.
func assertDiscriminatorAliasMapping(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok, "the base is a Model")
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, map[string]ir.TypeID{"alpha": componentID("Dog"), "zulu": componentID("Dog")},
		pet.Discriminator.Mapping, "the base keeps every key")

	dog, ok := doc.Types[componentID("Dog")].(*ir.Model)
	require.True(t, ok, "Dog composes as a Model")
	assert.Equal(t, "alpha", dog.DiscriminatorValue,
		"the smallest key in byte order, not the first written")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/Dog"),
		"the election is reported at the subtype: %+v", diags)
}

// assertDiscriminatorDefaultMapping pins Discriminator.Default, whose only source
// is the 3.2 discriminator.defaultMapping — and with it that a 3.2-only schema
// keyword compiles without an error diagnostic (GitHub #146). The library checks
// every schema object against the 3.1 meta-schema whatever the document says, so
// before this the spec below failed to compile and the field had no witness.
func assertDiscriminatorDefaultMapping(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	assert.Empty(t, diagsAt(diags, "openapi/validation/validation-invalid-schema",
		"/components/schemas/Pet/discriminator/defaultMapping"),
		"a 3.2 keyword in a 3.2 document is not an invalid schema")

	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, componentID("Dog"), pet.Discriminator.Default,
		"defaultMapping names the variant an unrecognized tag falls back to")
	assert.Equal(t, componentID("Cat"), pet.Discriminator.Mapping["cat"],
		"and it is read separately from the mapping")
}

// assertDiscriminatedUnionBesideModel pins where a discriminator goes when its
// union is kept verbatim beside a model. Pet's and Animal's routes are not
// subtypes of them, so pass.Validate would reject the discriminator on the
// model; it moves whole to Unmodeled beside the union. Vehicle's object
// variant holds one the same way. Shape's routes are true allOf subtypes, so
// the model keeps it, and the document validates clean.
func assertDiscriminatedUnionBesideModel(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	found, err := pass.Validate(t.Context(), doc)
	require.NoError(t, err)
	assert.Empty(t, found)

	for _, name := range []string{"Pet", "Animal"} {
		m, ok := doc.Types[componentID(name)].(*ir.Model)
		require.True(t, ok, name)
		assert.Nil(t, m.Discriminator, "%s routes to a non-subtype, so the model does not carry it", name)
		entry, ok := m.Unmodeled["openapi:discriminator"]
		require.True(t, ok, "%s keeps it verbatim instead", name)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
		assert.Contains(t, m.Unmodeled, "openapi:oneOf", "beside the union it routes through")
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/"+name+"/discriminator"))
	}
	_, isUnion := doc.Types[componentID("Bird")].(*ir.Union)
	assert.True(t, isUnion, "Bird writes allOf to Animal but lowers to a union, which is no subtype")

	vehicle, ok := doc.Types[componentID("Vehicle")].(*ir.Union)
	require.True(t, ok, "a multi-typed body lowers to a union")
	assert.Contains(t, vehicle.Unmodeled, "openapi:discriminator", "kept beside the union it was written for")
	variant, ok := doc.Types["t/anon/components/schemas/Vehicle/type/0"].(*ir.Model)
	require.True(t, ok)
	assert.Nil(t, variant.Discriminator, "the object variant does not carry it")

	shape, ok := doc.Types[componentID("Shape")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, shape.Discriminator, "every target is a subtype, so it stays")
	assert.Equal(t, componentID("Circle"), shape.Discriminator.Mapping["circle"])
	assert.NotContains(t, shape.Unmodeled, "openapi:discriminator")
}

// assertNullableUnionDiscriminator pins that a discriminator beside a oneOf or
// anyOf that collapses to nullable X is kept verbatim, with a diagnostic,
// where the collapse leaves no node with a field for it.
func assertNullableUnionDiscriminator(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	maybe, ok := doc.Types[componentID("MaybeCat")].(*ir.Scalar)
	require.True(t, ok, "the component is an alias over the nullable reference")
	assert.Equal(t, ir.TypeRef{Target: componentID("Cat"), Nullable: true}, *maybe.Base)
	assert.Contains(t, maybe.Unmodeled, "openapi:discriminator")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/MaybeCat"))

	owner, ok := doc.Types[componentID("Owner")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, owner.Properties, 1)
	assert.Equal(t, ir.TypeRef{Target: componentID("Cat"), Nullable: true}, owner.Properties[0].Type)
	assert.Contains(t, owner.Properties[0].Unmodeled, "openapi:discriminator", "the property carries it")
}

func assertAnyOfUntagged(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	u, ok := doc.Types[componentID("StringOrNumber")].(*ir.Union)
	require.True(t, ok)
	assert.False(t, u.Exclusive, "anyOf is a non-exclusive union")
	assert.False(t, u.WireTagged, "with no discriminator, validation infers the variant")
	assert.Nil(t, u.Discriminator)
	assert.Len(t, u.Variants, 2)
}

// assertUnionDecoratedBranch pins a union branch annotated the only way 3.0
// can, by wrapping its $ref (GitHub #1018). The wrapper lowers to a decorator
// over Pet that keeps the description, so no information is lost, and its
// variant is named for Pet while the node keeps the positional hint.
func assertUnionDecoratedBranch(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	u, ok := doc.Types[componentID("U")].(*ir.Union)
	require.True(t, ok)
	require.Len(t, u.Variants, 2)
	assert.Equal(t, "user", u.Variants[0].Name.Hint)
	assert.Equal(t, "pet", u.Variants[1].Name.Hint, "named for the $ref it wraps, as 3.1's sibling spelling is")

	branch, ok := doc.Types[u.Variants[1].Type.Target].(*ir.Model)
	require.True(t, ok, "the wrapped branch is a node of its own")
	base, decorates := ir.Decorated(branch)
	require.True(t, decorates, "a description beside the wrapped $ref is an annotation, not a shape")
	assert.Equal(t, componentID("Pet"), base.Target)
	assert.Equal(t, "a pet", branch.Docs.Description, "the annotation is kept")
	assert.Equal(t, "variant_1", branch.Name.Hint, "the node keeps the positional hint")
}

func assertNegationNot(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	m, ok := doc.Types[componentID("NotFoo")].(*ir.Model)
	require.True(t, ok)
	raw, ok := m.Unmodeled["openapi:not"]
	require.True(t, ok, "not-keyword kept verbatim under Unmodeled")
	assert.JSONEq(t, `{"required":["b"]}`, string(raw.Value))
	assert.Equal(t, ir.ReasonValidationOnly, raw.Reason)
	var found bool
	for _, d := range diags {
		if d.Code == "openapi/validation-only-keyword" {
			found = true
			assert.Equal(t, ir.SeverityInfo, d.Severity)
		}
	}
	assert.True(t, found, "expected a validation-only-keyword info diagnostic")
}
