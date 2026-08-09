// This file holds the conformance assertions whose subject is what the IR keeps
// verbatim rather than what it models: a construct the compiler cannot lower must
// survive under Unmodeled, under the reason that says which kind of gap it is,
// and be announced by a diagnostic. It shares TestConformance's table in
// conformance_test.go; the split is by subject, so the structural cases stay
// readable beside each other there.
package openapi_test // external test package — exercises only the public API

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// unmodeledEntry returns the Unmodeled entry at key, failing when absent.
func unmodeledEntry(t *testing.T, p ir.Unmodeled, key string) ir.UnmodeledEntry {
	t.Helper()
	entry, ok := p[key]
	require.True(t, ok, "%s kept verbatim; got %v", key, p)
	return entry
}

// diagsAt returns the severities of the diagnostics coded code at pointer.
func diagsAt(diags []ir.Diagnostic, code, pointer string) []ir.Severity {
	var out []ir.Severity
	for _, d := range diags {
		if d.Code == code && d.Provenance.Pointer == pointer {
			out = append(out, d.Severity)
		}
	}
	return out
}

// assertUnhomedKeywords pins the keywords a position writes that the node its
// lowering produced has nowhere to carry (GitHub #148, #151).
//
// Two source shapes reach it. A contradictory schema takes one half and keeps the
// other beside it, which is ir-design §4.8's rule about a degraded lowering. And
// an applicator with no `type` beside it still constrains an instance —
// {items: {type: string}} constrains every array — where the position lowers to
// the top type, which understates it entirely.
func assertUnhomedKeywords(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	untyped := map[string]struct{ keyword, raw string }{
		"UntypedItems":       {"items", `{"type":"string"}`},
		"UntypedPrefixItems": {"prefixItems", `[{"type":"string"}]`},
		"UntypedRequired":    {"required", `["name"]`},
		"UntypedFormat":      {"format", `"uuid"`},
	}
	for name, want := range untyped {
		sc, ok := doc.Types[namedID(name)].(*ir.Scalar)
		require.True(t, ok, "%s owns a node of its own to keep the keyword on", name)
		require.NotNil(t, sc.Base)
		assert.Equal(t, ir.TypeID("t/prim/any"), sc.Base.Target,
			"%s still lowers to the top type; only the loss is now recorded", name)
		entry := unmodeledEntry(t, sc.Unmodeled, "openapi:"+want.keyword)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
		assert.JSONEq(t, want.raw, string(entry.Value))
		assert.Equal(t, []ir.Severity{ir.SeverityInfo},
			diagsAt(diags, "openapi/degraded-construct", "/components/schemas/"+name))
	}

	e, ok := doc.Types[namedID("EnumWithProperties")].(*ir.Enum)
	require.True(t, ok, "the enum half is taken")
	assert.JSONEq(t, `{"f":{"type":"integer"}}`,
		string(unmodeledEntry(t, e.Unmodeled, "openapi:properties").Value),
		"the property set the enum contradicts is kept beside it")

	sc, ok := doc.Types[namedID("ScalarWithProperties")].(*ir.Scalar)
	require.True(t, ok, "the scalar half is taken")
	assert.JSONEq(t, `{"g":{"type":"integer"}}`,
		string(unmodeledEntry(t, sc.Unmodeled, "openapi:properties").Value))

	m, ok := doc.Types[namedID("ObjectWithItems")].(*ir.Model)
	require.True(t, ok)
	assert.JSONEq(t, `{"type":"integer"}`,
		string(unmodeledEntry(t, m.Unmodeled, "openapi:items").Value),
		"a Model has no element type, so the array applicator is kept beside it")
	_, ok = propByWire(m, "h")
	assert.True(t, ok, "the half that did lower is untouched")

	// The union case reaches this through hasUnionSiblings, which used to treat
	// items as not-a-sibling and lower the union over it. Both halves survive:
	// the applicator here, the branches under the union's own keys.
	u, ok := doc.Types[namedID("UnionWithItems")].(*ir.Scalar)
	require.True(t, ok, "a union with siblings lowers its structural body and keeps the union")
	assert.JSONEq(t, `{"type":"string"}`,
		string(unmodeledEntry(t, u.Unmodeled, "openapi:items").Value))
	assert.JSONEq(t, `[{"type":"string"},{"type":"integer"}]`,
		string(unmodeledEntry(t, u.Unmodeled, "openapi:oneOf").Value))

	assertRefSiteKeywords(t, doc, diags)
	assertElectedLoweringKeywords(t, doc, diags)
}

// assertRefSiteKeywords pins the same census at a $ref site, where it did not
// run at all (GitHub #283). $ref is an ordinary 2020-12 keyword, so its siblings
// are conjoined with it; the position lowers to an alias over the target, which
// has none of their fields, and the five below reached no field, no Unmodeled
// entry and no diagnostic.
func assertRefSiteKeywords(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	for name, want := range map[string]struct{ keyword, target, raw string }{
		"RefFormat":               {"format", "RefTargetStr", `"email"`},
		"RefEnum":                 {"enum", "RefTargetStr", `["a","b"]`},
		"RefConst":                {"const", "RefTargetStr", `"a"`},
		"RefRequired":             {"required", "RefTargetObj", `["a"]`},
		"RefAdditionalProperties": {"additionalProperties", "RefTargetObj", `false`},
	} {
		sc, ok := doc.Types[namedID(name)].(*ir.Scalar)
		require.True(t, ok, "%s hoists an alias to hold what it wrote beside its $ref", name)
		require.NotNil(t, sc.Base)
		assert.Equal(t, namedID(want.target), sc.Base.Target, "%s still aliases its target", name)
		entry := unmodeledEntry(t, sc.Unmodeled, "openapi:"+want.keyword)
		assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
		assert.JSONEq(t, want.raw, string(entry.Value))
		assert.Equal(t, []ir.Severity{ir.SeverityInfo},
			diagsAt(diags, "openapi/degraded-construct", "/components/schemas/"+name))
	}

	ctl, ok := doc.Types[namedID("RefConstrained")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, ctl.Constraints)
	assert.Equal(t, int64(3), *ctl.Constraints.MinLength, "a bound beside a $ref always had a home")
	assert.Equal(t, "kept", ctl.Docs.Description)
	assert.Empty(t, ctl.Unmodeled, "so it keeps nothing verbatim")

	carrier, ok := doc.Types[namedID("RefCarrier")].(*ir.Model)
	require.True(t, ok)
	p, ok := propByWire(carrier, "p")
	require.True(t, ok)
	assert.Equal(t, namedID("RefTargetStr"), p.Type.Target,
		"a carrier hoists no node, so it still resolves straight to the target")
	assert.JSONEq(t, `"email"`, string(unmodeledEntry(t, p.Unmodeled, "openapi:format").Value),
		"and keeps the keyword on itself instead")
}

// assertElectedLoweringKeywords pins the keywords the *winning* family's
// lowering never reads (GitHub #268). The census asks the node that was built,
// which is why the two allOf cases differ: a composed Model asserts `object`
// already and loses nothing, and asserts nothing about `string` at all.
func assertElectedLoweringKeywords(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	scalarType, ok := doc.Types[namedID("AllOfWithScalarType")].(*ir.Model)
	require.True(t, ok)
	assert.JSONEq(t, `"string"`,
		string(unmodeledEntry(t, scalarType.Unmodeled, "openapi:type").Value))

	objectType, ok := doc.Types[namedID("AllOfWithObjectType")].(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, objectType.Unmodeled, "the composed Model already asserts `object`")
	assert.Empty(t, diagsAt(diags, "openapi/degraded-construct", "/components/schemas/AllOfWithObjectType"))

	format, ok := doc.Types[namedID("ConstWithFormat")].(*ir.Literal)
	require.True(t, ok)
	assert.JSONEq(t, `"int32"`, string(unmodeledEntry(t, format.Unmodeled, "openapi:format").Value),
		"a Literal has no Encoding field")

	bound, ok := doc.Types[namedID("ConstWithBound")].(*ir.Literal)
	require.True(t, ok)
	assert.JSONEq(t, `1`, string(unmodeledEntry(t, bound.Unmodeled, "openapi:maxLength").Value),
		"nor a Constraints field, and it owns its pointer so no alias carries one either")
}

// assertAllOfBooleanBranch pins the lowering of a boolean allOf branch, which
// declares no keywords and so has no residue to rescue (GitHub #154).
//
// The claim is that `false` lowers the same way wherever it appears. ir-design
// §4.8 fixes a bare `false` schema as a closed empty Model with an info
// diagnostic; before this, the same construct as a branch composed an *open*
// empty model — the most permissive shape the IR has, for a source that admits
// nothing.
func assertAllOfBooleanBranch(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	never, ok := doc.Types[namedID("Never")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.AdditionalClosed, never.Additional,
		"a false conjunct closes the composition, as a bare false schema closes its own model")
	entry := unmodeledEntry(t, never.Unmodeled, "openapi:allOf/0")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `false`, string(entry.Value),
		"the branch is kept, so `false` stays distinguishable from additionalProperties: false")
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/false-schema", "/components/schemas/Never/allOf/0"))
	_, ok = propByWire(never, "id")
	assert.True(t, ok, "the other branch still contributes; closing is not emptying")

	always, ok := doc.Types[namedID("Always")].(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, always.Additional, "a true conjunct constrains nothing, so it closes nothing")
	assert.Empty(t, always.Unmodeled, "and there is nothing about it to keep")
	assert.Empty(t, diagsAt(diags, "openapi/false-schema", "/components/schemas/Always/allOf/0"),
		"nor anything to report")

	bare, ok := doc.Types[namedID("BareFalse")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.AdditionalClosed, bare.Additional, "the rule the composed case is held to")
	assert.Empty(t, bare.Properties)
}

// assertAllOfInlineResidue pins the residue of an inline allOf branch: the merge
// reads only its properties and required list, so whatever else the branch wrote
// survives beside the composed model rather than vanishing into it (GitHub #123).
//
// The severity split is the point. A branch declaring a type that cannot be an
// object contradicts the ir.Model the composition produced, so it is a warning;
// a branch that only narrows the model further is info.
func assertAllOfInlineResidue(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	narrowed, ok := doc.Types[namedID("Narrowed")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, narrowed.Base, "the $ref branch still becomes Base")
	_, ok = propByWire(narrowed, "name")
	assert.True(t, ok, "the inline branch still contributes its properties")
	entry := unmodeledEntry(t, narrowed.Unmodeled, "openapi:allOf/1")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `{"type":"object","properties":{"name":{"type":"string"}},"maxProperties":3}`,
		string(entry.Value), "the whole branch is kept, not only the unmerged keyword")
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/degraded-construct", "/components/schemas/Narrowed/allOf/1"),
		"a branch that only narrows the model is info")

	contradicted, ok := doc.Types[namedID("Contradicted")].(*ir.Model)
	require.True(t, ok)
	entry = unmodeledEntry(t, contradicted.Unmodeled, "openapi:allOf/1")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.Equal(t, []ir.Severity{ir.SeverityWarning},
		diagsAt(diags, "openapi/degraded-construct", "/components/schemas/Contradicted/allOf/1"),
		"a branch excluding object contradicts the composed model and is a warning")
}

// assertAllOfRefBranchSiblings covers the other branch kind: keywords written
// beside a `$ref` in an allOf branch bind that branch, not the schema it names,
// so they cannot go on the shared target's node. The branch position gets a node
// of its own to hold them — the alias any $ref position hoists when it carries no
// Property or Parameter to hold them instead — and the composition points at that
// (GitHub #143).
//
// The bare branch is here to pin the other half: a `$ref` that writes nothing
// beside itself still composes straight to the target, so the fix costs no node
// where there was nothing to keep.
func assertAllOfRefBranchSiblings(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	annotated, ok := doc.Types[namedID("Annotated")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, annotated.Base, "the $ref branch still composes as Base")
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Annotated/allOf/0"), annotated.Base.Target,
		"Base names the branch's own node, not the shared target")

	branch, ok := doc.Types[annotated.Base.Target].(*ir.Scalar)
	require.True(t, ok, "the branch position hoists an alias over its target")
	require.NotNil(t, branch.Base)
	assert.Equal(t, namedID("Base"), branch.Base.Target, "the alias still resolves to the referenced schema")
	assert.Equal(t, "Base as this composition uses it", branch.Docs.Description)
	require.NotNil(t, branch.Constraints)
	require.NotNil(t, branch.Constraints.MinProps)
	assert.Equal(t, int64(3), *branch.Constraints.MinProps)
	entry := unmodeledEntry(t, branch.Unmodeled, "openapi:x-vendor")
	assert.Equal(t, ir.ReasonVendorExtension, entry.Reason)
	assert.JSONEq(t, `"keepme"`, string(entry.Value))

	assertAllOfRefBranchShapes(t, doc)
}

// assertAllOfRefBranchShapes checks the two branch shapes the annotated case
// does not reach: a mixin position, and a bare $ref that hoists nothing.
func assertAllOfRefBranchShapes(t *testing.T, doc *ir.Document) {
	t.Helper()
	mixed, ok := doc.Types[namedID("Mixed")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, mixed.Mixins, 2, "two unqualified refs stay mixins")
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Mixed/allOf/0"), mixed.Mixins[0].Target,
		"a mixin's siblings get the same home a base's do")
	assert.Equal(t, namedID("Extra"), mixed.Mixins[1].Target,
		"the branch writing nothing beside its $ref is untouched")

	bare, ok := doc.Types[namedID("Bare")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, bare.Base)
	assert.Equal(t, namedID("Base"), bare.Base.Target,
		"a bare $ref branch composes straight to its target, hoisting no node")
	_, hoisted := doc.Types[ir.TypeID("t/anon/components/schemas/Bare/allOf/0")]
	assert.False(t, hoisted, "nothing to keep, so no node for the branch position")
}

// assertParamXMLResidue pins a parameter schema's xml hints: ir.Parameter is the
// one annotation carrier with no XML field, and the hint is not inert here — a
// content-style parameter can bind application/xml (GitHub #124). Both parameter
// shapes are covered because they read the schema from different places.
func assertParamXMLResidue(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	op, ok := opByName(doc, "findDocs")
	require.True(t, ok)
	require.Len(t, op.Params, 2)

	tag, ok := paramByName(op, "tag")
	require.True(t, ok)
	entry := unmodeledEntry(t, tag.Unmodeled, "openapi:xml")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `{"name":"Tag","namespace":"urn:example:docs"}`, string(entry.Value))

	body, ok := paramByName(op, "body")
	require.True(t, ok)
	entry = unmodeledEntry(t, body.Unmodeled, "openapi:xml")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `{"name":"Body","attribute":true}`, string(entry.Value))
	assert.Equal(t, "application/xml", op.Bindings.HTTP[0].ParamBindings[1].ContentType,
		"the media type the hints are conditioned on is the one the binding records")

	for _, pointer := range []string{
		"/paths/~1docs/get/parameters/0/schema/xml",
		"/paths/~1docs/get/parameters/1/content/application~1xml/schema/xml",
	} {
		assert.Equal(t, []ir.Severity{ir.SeverityInfo},
			diagsAt(diags, "openapi/degraded-construct", pointer))
	}
}

// assertDependentRequired pins dependentRequired as a §4.7 validation-only
// keyword. The library binds no field for it, which is why it was the one member
// of that family being dropped silently (GitHub #125).
func assertDependentRequired(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	card, ok := doc.Types[namedID("Card")].(*ir.Model)
	require.True(t, ok, "the structural body still lowers")
	assert.Len(t, card.Properties, 2)
	entry := unmodeledEntry(t, card.Unmodeled, "openapi:dependentRequired")
	assert.Equal(t, ir.ReasonValidationOnly, entry.Reason)
	assert.JSONEq(t, `{"number":["cvv"]}`, string(entry.Value))
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/validation-only-keyword", "/components/schemas/Card"))
}

// assertContentVocabulary pins the 2020-12 content vocabulary: contentEncoding
// and contentMediaType are an encoding and lower into ir.Encoding, contentSchema
// is a schema and has no IR home anywhere, and a position with no Encoding field
// at all keeps both of the first two verbatim (GitHub #125).
func assertContentVocabulary(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	thumb, ok := doc.Types[namedID("Thumbnail")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, thumb.Encoding)
	assert.Equal(t, "base64", thumb.Encoding.Name)
	assert.Equal(t, "image/png", thumb.Encoding.MediaType)
	assert.Empty(t, thumb.Unmodeled, "a string position lowers both keywords structurally")

	env, ok := doc.Types[namedID("Envelope")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, env.Encoding)
	assert.Equal(t, "application/json", env.Encoding.MediaType)
	entry := unmodeledEntry(t, env.Unmodeled, "openapi:contentSchema")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `{"type":"object","properties":{"id":{"type":"string"}}}`, string(entry.Value))

	bag, ok := doc.Types[namedID("Bag")].(*ir.Model)
	require.True(t, ok)
	for _, key := range []string{"openapi:contentEncoding", "openapi:contentMediaType"} {
		assert.Equal(t, ir.ReasonNoIRHome, unmodeledEntry(t, bag.Unmodeled, key).Reason,
			"an object has no Encoding field, so %s is kept", key)
	}
}

// assertDialectKeywords pins the JSON Schema resource and dialect keywords as out
// of scope rather than a gap: the IR identifies types by pointer-derived ID and
// describes one API surface, so it has no axis for these and none is coming.
func assertDialectKeywords(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	res, ok := doc.Types[namedID("Resource")].(*ir.Model)
	require.True(t, ok)
	for _, keyword := range []string{"$id", "$schema", "$vocabulary"} {
		entry := unmodeledEntry(t, res.Unmodeled, "openapi:"+keyword)
		assert.Equal(t, ir.ReasonOutOfScope, entry.Reason)
		assert.Equal(t, []ir.Severity{ir.SeverityInfo},
			diagsAt(diags, "openapi/degraded-construct", "/components/schemas/Resource/"+keyword))
	}
	declared := unmodeledEntry(t, res.Unmodeled, "openapi:$id")
	assert.JSONEq(t, `"https://example.com/schemas/resource"`, string(declared.Value))
	for id := range doc.Types {
		assert.NotContains(t, string(id), "example.com",
			"$id is kept, never honoured: no type is keyed by the identity it declares")
	}
}

// assertDynamicRef pins both halves of $dynamicRef lowering (GitHub #125). A name
// declared exactly once on a component schema has one possible target whatever
// path evaluation took, so it expands; anything else is irreducible and the
// reference is kept verbatim beside whatever did lower.
func assertDynamicRef(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	tree, ok := doc.Types[namedID("Tree")].(*ir.Model)
	require.True(t, ok)

	child, ok := propByWire(tree, "child")
	require.True(t, ok)
	assert.Equal(t, namedID("Node"), child.Type.Target,
		"the single matching $dynamicAnchor is the property's type")
	assert.Empty(t, child.Unmodeled, "an expanded reference must not also be preserved")
	assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, "openapi/dynamic-ref-expanded",
		"/components/schemas/Tree/properties/child/$dynamicRef"))

	ghost, ok := propByWire(tree, "ghost")
	require.True(t, ok)
	entry := unmodeledEntry(t, ghost.Unmodeled, "openapi:$dynamicRef")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, `"#Missing"`, string(entry.Value))
	assert.Equal(t, []ir.Severity{ir.SeverityInfo}, diagsAt(diags, "openapi/degraded-construct",
		"/components/schemas/Tree/properties/ghost/$dynamicRef"))
}

// assertInlineResidue pins the other half of ir-design §14's OpenAPI row: a
// keyword that binds a *use* of a type reaches referencing properties, and an
// inline position that owns a node has no referencing property at all, so what a
// declaration there wrote would otherwise be lost (GitHub #138).
//
// The carrier case is asserted too, because it is what stops the fix from
// double-recording: a property lands all three in its own fields, so neither it
// nor the node its own shape owns may keep residue.
func assertInlineResidue(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "getThing")
	require.True(t, ok)
	bodyID := op.Responses[0].Payload.Contents[0].Type.Target
	body, ok := doc.Types[bodyID]
	require.True(t, ok, "the response body owns a node")
	assertResidue(t, body.Common().Unmodeled, map[string]string{
		"openapi:default": `"x"`, "openapi:readOnly": `true`,
	})

	batch, ok := doc.Types[namedID("Batch")].(*ir.List)
	require.True(t, ok)
	elem, ok := doc.Types[batch.Elem.Target]
	require.True(t, ok, "the annotated element owns a node")
	assertResidue(t, elem.Common().Unmodeled, map[string]string{
		"openapi:default": `7`, "openapi:writeOnly": `true`,
	})

	carried, ok := doc.Types[namedID("Carried")].(*ir.Model)
	require.True(t, ok)
	p, ok := propByWire(carried, "p")
	require.True(t, ok)
	require.NotNil(t, p.Default)
	require.Len(t, p.Default.List, 1)
	assert.Equal(t, ir.BigVal("3"), p.Default.List[0].Num,
		"a carrier lands the default in its own field")
	assert.Equal(t, []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery},
		p.Visibility.Only)
	assert.Empty(t, p.Unmodeled, "a carrier records no residue for what it already holds")

	// The carrier's array shape interns a node at the property's own pointer,
	// which is where a recorder that stopped distinguishing the two homes would
	// put the residue.
	pNode, ok := doc.Types[p.Type.Target].(*ir.List)
	require.True(t, ok, "the carrier's array shape owns a node")
	assert.Empty(t, pNode.Unmodeled, "nor on the node that shape owns")
}

// assertResidue requires p to hold exactly the given keys, each under
// ReasonNoIRHome with the given JSON value.
func assertResidue(t *testing.T, p ir.Unmodeled, want map[string]string) {
	t.Helper()
	assert.Len(t, p, len(want))
	for key, value := range want {
		entry := unmodeledEntry(t, p, key)
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
		assert.JSONEq(t, value, string(entry.Value))
	}
}

// assertResponseLinks pins a response's links: ir.Response has no field for the
// link objects OpenAPI declares there, so they are kept verbatim on the response
// rather than dropped while the operation they name lowers normally.
func assertResponseLinks(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "createOrder")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	entry := unmodeledEntry(t, op.Responses[0].Unmodeled, "openapi:links")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t,
		`{"GetOrder":{"operationId":"getOrder","parameters":{"orderId":"$response.body#/id"}}}`,
		string(entry.Value))
	_, ok = opByName(doc, "getOrder")
	assert.True(t, ok, "the operation a link names is an ordinary operation")
}

// assertCoDeclaredKeywords pins the keywords a schema writes that compete for
// one position (GitHub #35).
//
// The dispatch elects one of them and the rest were dropped outright — a whole
// relationship to a Base, or half a union, gone with no Unmodeled entry and no
// diagnostic at all. Every case here asserts the same three things: the elected
// keyword still lowers, the ones passed over are kept verbatim under
// ReasonDegradedLowering, and the position says so.
func assertCoDeclaredKeywords(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	base := `[{"$ref":"#/components/schemas/Base"}]`
	e, ok := doc.Types[namedID("NarrowedEnum")].(*ir.Enum)
	require.True(t, ok, "the enum is the value")
	assertKeptRaw(t, e.Unmodeled, "openapi:allOf", base)

	lit, ok := doc.Types[namedID("NarrowedConst")].(*ir.Literal)
	require.True(t, ok, "const outranks the composition beside it")
	assertKeptRaw(t, lit.Unmodeled, "openapi:allOf", base)

	within, ok := doc.Types[namedID("ConstWithinEnum")].(*ir.Literal)
	require.True(t, ok, "const is the narrower of the two")
	assertKeptRaw(t, within.Unmodeled, "openapi:enum", `["a","b"]`)

	anyOf := `[{"type":"number"},{"type":"boolean"}]`
	both, ok := doc.Types[namedID("BothCombinators")].(*ir.Union)
	require.True(t, ok, "oneOf becomes the union")
	assert.Len(t, both.Variants, 2, "with a variant per oneOf branch")
	assertKeptRaw(t, both.Unmodeled, "openapi:anyOf", anyOf)

	// Suppressing the {X, null} collapse is what gives this one a node of its
	// own: collapsing would resolve the position to the shared string primitive,
	// which must never carry one declaration's keywords.
	nullable, ok := doc.Types[namedID("NullableWithCombinators")].(*ir.Union)
	require.True(t, ok, "a co-declared anyOf stops the {X, null} collapse")
	assert.Len(t, nullable.Variants, 1, "the null branch still lifts off the variant list")
	assertKeptRaw(t, nullable.Unmodeled, "openapi:anyOf", anyOf)

	for _, name := range []string{
		"NarrowedEnum", "NarrowedConst", "ConstWithinEnum",
		"BothCombinators", "NullableWithCombinators",
	} {
		assert.Equal(t, []ir.Severity{ir.SeverityInfo},
			diagsAt(diags, "openapi/degraded-construct", "/components/schemas/"+name),
			"%s announces the keyword it passed over", name)
	}
}

// assertKeptRaw requires p to hold key under ReasonDegradedLowering with the
// given JSON payload.
func assertKeptRaw(t *testing.T, p ir.Unmodeled, key, want string) {
	t.Helper()
	entry := unmodeledEntry(t, p, key)
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.JSONEq(t, want, string(entry.Value))
}
