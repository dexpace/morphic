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
