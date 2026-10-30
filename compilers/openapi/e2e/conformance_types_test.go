// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func assertNamedTypes(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	user, ok := doc.Types[componentID("User")].(*ir.Model)
	require.True(t, ok, "named schema User present under its pointer-derived ID")
	assert.False(t, user.Anonymous)
	addr, ok := propByWire(user, "address")
	require.True(t, ok)
	assert.Equal(t, componentID("Address"), addr.Type.Target)
	_, ok = doc.Types[componentID("Address")].(*ir.Model)
	assert.True(t, ok, "referenced Address resolves in the registry")
}

// assertNeutralNaming covers invariant 4's second half: Naming.Canonical is a
// neutral word sequence whatever the source spelled the word boundaries as.
// Real specs use dots (namespaced components, versioned fields, enum values),
// brackets (deep-object query parameters) and hyphens (headers), so this spec
// writes each where a segmentation fault would show. Naming.Hint is covered by
// the same spec: its context strings carry the punctuation, and a hint such as
// "rollout.state" is a name no emitter can render (GitHub #54).
func assertNeutralNaming(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	svc := doc.Services[0]
	assert.Equal(t, "neutral_naming_api", svc.Name.Canonical, "info.title")
	require.Len(t, svc.Groups, 1)
	assert.Equal(t, "inventory_core", svc.Groups[0].Name.Canonical, "tag")

	op, ok := opByName(doc, "widgets.list")
	require.True(t, ok, "operation present under its source operationId")
	assert.Equal(t, "widgets_list", op.Name.Canonical, "operationId")
	byParam := map[string]string{}
	for _, p := range op.Params {
		byParam[p.Name.Source] = p.Name.Canonical
	}
	assert.Equal(t, "widget_id", byParam["widget.id"], "path parameter")
	assert.Equal(t, "filter_name", byParam["filter[name]"], "bracketed query parameter")

	widget, ok := doc.Types[componentID("com.example.Widget")].(*ir.Model)
	require.True(t, ok, "namespaced component keeps its pointer-derived ID")
	assert.Equal(t, "com_example_widget", widget.Name.Canonical, "component name")
	assert.Equal(t, "com.example.Widget", widget.Name.Source, "the source spelling is kept beside it")
	name, ok := propByWire(widget, "widget.name")
	require.True(t, ok)
	assert.Equal(t, "widget_name", name.Name.Canonical, "property name")
	version, ok := propByWire(widget, "api.version.v2")
	require.True(t, ok)
	assert.Equal(t, "api_version_v_2", version.Name.Canonical,
		"the letter/digit boundary still splits inside a dotted segment")

	assertNeutralNamingLeaves(t, doc, widget, op)
}

// assertNeutralNamingLeaves checks the three canonicals that hang off another
// node rather than off the service walk: an enum member under a property, a
// header under a response, a scheme in the auth registry.
func assertNeutralNamingLeaves(t *testing.T, doc *ir.Document, widget *ir.Model, op ir.Operation) {
	t.Helper()
	state, ok := propByWire(widget, "rollout.state")
	require.True(t, ok)
	rollout, ok := doc.Types[state.Type.Target].(*ir.Enum)
	require.True(t, ok, "the enum property hoists an Enum node")
	require.NotEmpty(t, rollout.Members)
	assert.Equal(t, "in_progress", rollout.Members[0].Name.Canonical, "enum member")
	assert.Equal(t, "rollout_state", rollout.Name.Hint,
		"the hoisted node's hint is words too, not the property key verbatim")

	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)
	assert.Equal(t, "x_rate_limit", op.Responses[0].Headers[0].Name.Canonical, "response header")

	require.Len(t, doc.Auth, 1)
	for _, scheme := range doc.Auth {
		assert.Equal(t, "oauth_2_client", scheme.Name.Canonical, "security scheme")
	}
}

// assertEmptyNames is the complement of assertNeutralNaming: every position
// where the source names an entity with the empty string, which is a legal
// OpenAPI document at each of them. The compiler used to pass "" through as a
// Naming with nothing in any channel, leaving an emitter no identifier to render
// and satisfying every neutrality check vacuously (GitHub #251). Each of these
// now carries the minted hint instead, and irverify's presence rule — swept over
// this corpus by TestVerify_Corpus — is what holds the general case.
func assertEmptyNames(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	assert.Equal(t, "empty", doc.Services[0].Groups[0].Name.Hint, "tag declared as \"\"")

	require.Len(t, doc.Auth, 1)
	for _, scheme := range doc.Auth {
		assert.Equal(t, "empty", scheme.Name.Hint, "security scheme keyed \"\"")
	}

	blank, ok := doc.Types[ir.TypeID("t/anon/components/schemas/")]
	require.True(t, ok, "a component schema keyed \"\" is hoisted at its own pointer")
	assert.Equal(t, "empty", blank.Common().Name.Hint, "component schema keyed \"\"")

	assertEmptyEnumMember(t, doc)
	assertEmptyNamedProperty(t, doc)
	assertEmptyDerivedHints(t, doc)
}

// assertEmptyEnumMember covers the enum member the issue was reported against,
// and beside it the collision the minted word can cause: "" and "empty" are two
// distinct values, and a member named for each lands on the same word.
//
// The IR keeps them apart — two members, two values, one in each name channel —
// because uniquifying identifiers is an emitter's job and always has been:
// "red" and "Red" already canonicalize to the same words. This pins that the
// minted name does not merge or drop a member, which is the only part of the
// collision the IR is answerable for.
func assertEmptyEnumMember(t *testing.T, doc *ir.Document) {
	t.Helper()
	rollout, ok := doc.Types[componentID("Rollout")].(*ir.Enum)
	require.True(t, ok)
	require.Len(t, rollout.Members, 3, "no member is merged away by the shared word")

	assert.Equal(t, "empty", rollout.Members[0].Name.Hint, "enum member whose value is \"\"")
	assert.Empty(t, rollout.Members[0].Name.Source, "nothing is fabricated into the source channel")
	assert.Empty(t, rollout.Members[0].Value.Str, "the value itself is kept as the empty string")

	declared := rollout.Members[1]
	assert.Equal(t, "empty", declared.Name.Source, "a member whose value really is \"empty\"")
	assert.Equal(t, "empty", declared.Name.Canonical, "named through the declared channels, not the hint")
	assert.Empty(t, declared.Name.Hint)

	assert.Equal(t, "done", rollout.Members[2].Name.Source, "the ordinary sibling is unaffected")
}

// assertEmptyNamedProperty covers the two entities one property keyed "" makes:
// the property, and the inline schema hoisted at its position, whose context
// hint is derived from the same empty name.
func assertEmptyNamedProperty(t *testing.T, doc *ir.Document) {
	t.Helper()
	widget, ok := doc.Types[componentID("Widget")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, widget.Properties, 1)
	prop := widget.Properties[0]
	assert.Equal(t, "empty", prop.Name.Hint, "property keyed \"\"")
	assert.Empty(t, prop.WireName, "the wire key itself is still the empty string")

	inline, ok := doc.Types[prop.Type.Target]
	require.True(t, ok, "the property's inline object is hoisted")
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Widget/properties/"), prop.Type.Target)
	assert.Equal(t, "empty", inline.Common().Name.Hint,
		"a hint derived from an empty position is minted, not passed through empty")
}

// composedHints is every node in the corpus spec whose hint is built out of an
// enclosing node's. Each pairs an unnamed position with the role or index that
// distinguishes the child from its siblings — a list element, a map value, a
// pattern group, a tuple slot or rest, an enum branch, a composed union
// variant. A slice rather than a map so a failure names them in a fixed order.
var composedHints = []struct {
	id   ir.TypeID
	hint string
}{
	{"t/anon/components/schemas/List/properties//items", "empty_item"},
	{"t/anon/components/schemas/Maps/properties//additionalProperties", "empty_value"},
	{"t/anon/components/schemas/Patterns/properties//patternProperties/^x", "empty_pattern"},
	{"t/anon/components/schemas/Tuple/properties//prefixItems/0", "empty_0"},
	{"t/anon/components/schemas/Tuple/properties//items", "empty_item"},
	{"t/anon/components/schemas/Mixed/properties//enum/0", "empty_0"},
	{"t/anon/components/schemas/Mixed/properties//enum/1", "empty_1"},
	{"t/composed/components/schemas/Host/properties//oneOf/0", "empty_alt"},
}

// assertEmptyDerivedHints covers hints a child composes from an enclosing
// node's. Concatenating onto an empty hint once yielded a leading "_", which is
// non-empty so irverify's presence check passes it, yet no grammar produces it.
//
// composedHints has no row for contentSchema, which an empty-named property
// reaches (#743). The sequential media type's itemSchema cannot need one: both
// callers of lowerPayload pass a non-empty hint, since ids.ComponentEntry
// refuses a component keyed "" and the fallbacks are "response" and "request".
// A composed variant of an inline branch is absent too:
// docs/compilers/openapi.md#union-beside-structure keeps that union verbatim,
// so only the $ref form the Host case uses reaches composedVariant.
func assertEmptyDerivedHints(t *testing.T, doc *ir.Document) {
	t.Helper()
	mixed, ok := doc.Types[componentID("Mixed")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, mixed.Properties, 1)
	union, ok := doc.Types[mixed.Properties[0].Type.Target].(*ir.Union)
	require.True(t, ok, "a heterogeneous enum lowers to a union of literals")
	assert.Equal(t, "empty", union.Name.Hint, "the enclosing node keeps the plain minted hint")

	for _, tc := range composedHints {
		node, found := doc.Types[tc.id]
		require.True(t, found, "no node at %s; the composition site moved", tc.id)
		assert.Equal(t, tc.hint, node.Common().Name.Hint, "composed hint at %s", tc.id)
	}
}

func assertInlineTypes(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	order, ok := doc.Types[componentID("Order")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, order.Properties, 1)
	shipping := order.Properties[0]
	// hoisted exactly once, under its pointer-derived anonymous ID.
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Order/properties/shipping"),
		shipping.Type.Target)
	inline, ok := doc.Types[shipping.Type.Target].(*ir.Model)
	require.True(t, ok, "the inline object was hoisted as its own type")
	assert.True(t, inline.Anonymous)
	assert.Equal(t, "shipping", inline.Name.Hint)
}

// assertInlineHoistPositions pins the six operation-side positions an inline
// composite can be declared at against the ID each one's source pointer derives.
// Neither inline-types.yaml nor the inlinePositions table in
// compilers/openapi/internal/schema reaches a parameter, response header,
// webhook or callback, which are lowered a layer up.
//
// The referring site must point at the derived ID, which a moved position
// changes. The six targets must also be six distinct nodes, which survives a
// synchronized edit: a hoist collapsing identical bodies and an updated
// expectation would otherwise agree. The fixture writes one body at all six so a
// collapse is reachable.
func assertInlineHoistPositions(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	got := inlineHoistPositionRefs(t, doc)
	if diff := cmp.Diff(inlineHoistPositionIDs(), got); diff != "" {
		t.Errorf("hoisted node per source position (-want +got):\n%s", diff)
	}

	owner := map[ir.TypeID]string{}
	for position, id := range got {
		assert.NotContains(t, owner, id,
			"the %s and %s positions share one node, %s", owner[id], position, id)
		owner[id] = position

		node, found := doc.Types[id]
		require.True(t, found, "%s: nothing is interned at %s", position, id)
		model, ok := node.(*ir.Model)
		require.True(t, ok, "%s: the inline object hoisted as a model", position)
		assert.True(t, model.Anonymous, "%s: a minted node is anonymous", position)
		assert.Len(t, model.Properties, 1, "%s: it kept the body it was declared with", position)
	}
}

// inlineHoistPositionIDs is the ID each position's source pointer derives,
// spelled out so a node that moves to another pointer is a diff rather than a
// silently different document.
func inlineHoistPositionIDs() map[string]ir.TypeID {
	const order = "t/anon/paths/~1orders/post"
	return map[string]ir.TypeID{
		"parameter schema":       order + "/parameters/0/schema",
		"response-header schema": order + "/responses/200/headers/X-Order-Trace/schema",
		"request-body property":  order + "/requestBody/content/application~1json/schema/properties/shipping",
		"response-body property": order + "/responses/200/content/application~1json/schema/properties/receipt",
		"webhook body":           "t/anon/webhooks/onShipped/post/requestBody/content/application~1json/schema",
		"callback body": order + "/callbacks/onProgress/{$request.body#~1callbackUrl}" +
			"/post/requestBody/content/application~1json/schema",
	}
}

// inlineHoistPositionRefs reads back what each declaring site actually refers
// to. It walks from the operation rather than from doc.Types so a node interned
// at the right ID but wired to nothing still fails.
func inlineHoistPositionRefs(t *testing.T, doc *ir.Document) map[string]ir.TypeID {
	t.Helper()
	order, ok := opByName(doc, "placeOrder")
	require.True(t, ok)
	audit, ok := openapitest.ParamBySource(order, "audit")
	require.True(t, ok, "the operation declares its inline-schema parameter")
	require.Len(t, order.Responses, 1)
	require.Len(t, order.Responses[0].Headers, 1, "the response declares its inline-schema header")
	webhook, ok := opByName(doc, "onShipped")
	require.True(t, ok, "the webhook operation is registered")
	callback, ok := opByName(doc, "onProgress")
	require.True(t, ok, "the callback operation is registered")

	return map[string]ir.TypeID{
		"parameter schema":       audit.Type.Target,
		"response-header schema": order.Responses[0].Headers[0].Type.Target,
		"request-body property":  inlinePropTarget(t, doc, openapitest.BodyTarget(t, order.Request), "shipping"),
		"response-body property": inlinePropTarget(t, doc, openapitest.BodyTarget(t, order.Responses[0].Payload), "receipt"),
		"webhook body":           openapitest.BodyTarget(t, webhook.Request),
		"callback body":          openapitest.BodyTarget(t, callback.Request),
	}
}

// inlinePropTarget returns the type the named property of the model at id refers
// to — the body-property positions, one level inside a body root.
func inlinePropTarget(t *testing.T, doc *ir.Document, id ir.TypeID, wire string) ir.TypeID {
	t.Helper()
	model, ok := doc.Types[id].(*ir.Model)
	require.True(t, ok, "the body at %s hoisted as a model", id)
	prop, ok := propByWire(model, wire)
	require.True(t, ok, "the body declares the property %q", wire)
	return prop.Type.Target
}

// assertSharedResponseAcrossStatus reads the one shape that puts lowerResponse
// and lowerErrorCase on the same declaration: a components/responses entry
// mounted at both a success and an error status. Both intern the body type at
// the component's pointer, so the two mints race for it and the loser's naming
// hint is discarded, which the order-invariance oracle reports as an
// order-dependent registry. The hint derives from the declaration pointer, the
// same whichever side reaches it first. No other corpus spec reaches one
// response component from both sides, so the oracle needs this one to ask.
func assertSharedResponseAcrossStatus(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op := operationAt(t, doc, "GET", "/widgets")
	require.Len(t, op.Responses, 1, "the success mount")
	require.Len(t, op.Errors, 1, "and the error mount, of the one component")

	success := op.Responses[0].Payload.Contents[0].Type.Target
	failure := op.Errors[0].Payload.Contents[0].Type.Target
	assert.Equal(t, success, failure, "one declaration is one type, reached from either status")

	td, ok := doc.Types[success]
	require.True(t, ok)
	assert.Equal(t, "envelope", td.Common().Name.Hint,
		"the hint comes from the declaration, not from whichever status class minted it first")

	created := operationAt(t, doc, "POST", "/widgets").Responses[0].Payload.Contents[0].Type.Target
	rejected := operationAt(t, doc, "POST", "/gadgets").Errors[0].Payload.Contents[0].Type.Target
	assert.Equal(t, created, rejected, "a path-pointer $ref reaches the same one type")
	td, ok = doc.Types[created]
	require.True(t, ok)
	assert.Equal(t, "response", td.Common().Name.Hint,
		"with no component to name it, both sides fall back to the one word")
}

// assertComponentReuse covers the non-schema half of `$ref`: OpenAPI lets a
// parameter, requestBody, response, header, callback, or whole path item be
// declared once under components and referenced from many operations. Each
// lowers at its declaration, so the shared node is interned once however many
// operations reach it, while the operations that reach it stay distinct.
func assertComponentReuse(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	widgets := operationAt(t, doc, "GET", "/widgets")
	gadgets := operationAt(t, doc, "GET", "/gadgets")
	order := operationAt(t, doc, "POST", "/orders")
	assert.NotEqual(t, widgets.ID, gadgets.ID, "one path item mounted twice is two operations")
	assert.Equal(t, widgets.Provenance.Pointer, gadgets.Provenance.Pointer,
		"...both written at the one component that declares them")

	// Every hoisted type in this document belongs to a component declaration:
	// no operation contributes a pointer of its own, because none declares a
	// schema of its own.
	for id, td := range doc.Types {
		if td.Common().Provenance.Pointer == "" {
			continue // shared primitives are pointerless by construction
		}
		assert.True(t, strings.HasPrefix(string(td.Common().Provenance.Pointer), "/components/"),
			"type %s hoists at a component declaration, not a use site", id)
	}

	sortID := ir.TypeID("t/anon/components/parameters/Sort/schema")
	require.Len(t, widgets.Params, 1)
	require.Len(t, gadgets.Params, 1)
	assert.Equal(t, sortID, widgets.Params[0].Type.Target)
	assert.Equal(t, sortID, gadgets.Params[0].Type.Target, "the shared parameter interns once")

	listedID := ir.TypeID("t/anon/components/responses/Listed/content/application~1json/schema")
	assert.Equal(t, listedID, openapitest.BodyTarget(t, widgets.Responses[0].Payload))
	assert.Equal(t, listedID, openapitest.BodyTarget(t, order.Responses[0].Payload),
		"a response reused across unrelated operations interns once")

	bodyID := ir.TypeID("t/anon/components/requestBodies/OrderBody/content/application~1json/schema")
	require.NotNil(t, order.Request)
	assert.Equal(t, bodyID, openapitest.BodyTarget(t, order.Request))
	assert.Equal(t, "order_body", doc.Types[bodyID].Common().Name.Hint,
		"a shared body is named after its component, not the operation that reached it first")

	require.Len(t, widgets.Responses[0].Headers, 1)
	header := widgets.Responses[0].Headers[0]
	assert.Equal(t, ir.TypeID("t/anon/components/headers/RateUnit/schema"), header.Type.Target)
	assert.Equal(t, jsontext.Pointer("/components/responses/Listed/headers/X-Rate-Unit"), header.Provenance.Pointer,
		"the header's identity is the map entry that binds its name")

	require.Len(t, order.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, "{$request.body#/callbackUrl}", order.Bindings.HTTP[0].Callbacks[0].Expression)
	_, ok := opByName(doc, "onShipped")
	assert.True(t, ok, "the shared callback's operation is registered alongside its parent")
}

func assertReadOnlyWriteOnly(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	m, ok := doc.Types[componentID("S")].(*ir.Model)
	require.True(t, ok)
	r, ok := propByWire(m, "r")
	require.True(t, ok)
	w, ok := propByWire(m, "w")
	require.True(t, ok)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}, r.Visibility)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}, w.Visibility)

	// A declaration-site readOnly reaches the referencing property, and the
	// declaration keeps it verbatim because the type node has no field for it.
	viaRef, ok := propByWire(m, "viaRef")
	require.True(t, ok)
	assert.Equal(t, r.Visibility, viaRef.Visibility, "the referent's readOnly binds the reference site")

	decl, ok := doc.Types[componentID("DeclaredReadOnly")].(*ir.Scalar)
	require.True(t, ok)
	entry, ok := decl.Unmodeled["openapi:readOnly"]
	require.True(t, ok, "the declaration's own readOnly is kept as residue")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `true`, string(entry.Value))

	assertBothFlagsAreVisibleNowhere(t, m, diags)
}

// assertBothFlagsAreVisibleNowhere pins the contradictory pairing in both its
// spellings: written on one schema, and written across a $ref that already
// carries the other flag. Each admits no lifecycle at all and each is reported,
// where declaring both used to lower exactly as readOnly alone did and say
// nothing (GitHub #276).
func assertBothFlagsAreVisibleNowhere(t *testing.T, m *ir.Model, diags []ir.Diagnostic) {
	t.Helper()
	for _, wire := range []string{"both", "bothViaRef"} {
		p, ok := propByWire(m, wire)
		require.True(t, ok)
		assert.Equal(t, ir.Visibility{None: true}, p.Visibility,
			"readOnly and writeOnly both in force admit no lifecycle: %s", wire)
		assert.Equal(t, []ir.Severity{ir.SeverityWarning},
			diagsAt(diags, diag.DisjointVisibility, "/components/schemas/S/properties/"+wire),
			"the contradiction is reported once, at the position that wrote it: %s", wire)
	}
}

func assertRecursive(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	n, ok := doc.Types[componentID("Node")].(*ir.Model)
	require.True(t, ok)
	next, ok := propByWire(n, "next")
	require.True(t, ok)
	assert.Equal(t, componentID("Node"), next.Type.Target, "self-reference terminates on the interned ID")
}

// assertMaps pins ir-design §4.6's line between the two map shapes: an object
// whose one member fact is its catch-all is a MapT, carrying its value type and
// entry bounds, and a catch-all beside anything else is a Model's
// AdditionalProps. An explicit `true` types the catch-all as the top type in
// either.
func assertMaps(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	i64 := func(v int64) *int64 { return &v }
	for name, want := range map[string]*ir.MapT{
		"OpenMap":    {Value: ir.TypeRef{Target: "t/prim/integer"}},
		"AnyMap":     {Value: ir.TypeRef{Target: "t/prim/any"}},
		"BoundedMap": {Value: ir.TypeRef{Target: "t/prim/string"}, Constraints: &ir.Constraints{MinProps: i64(1), MaxProps: i64(10)}},
	} {
		mt, ok := doc.Types[componentID(name)].(*ir.MapT)
		require.True(t, ok, "%s is a map and nothing else; got %T", name, doc.Types[componentID(name)])
		assert.Nil(t, mt.Key, "%s has string keys", name)
		assert.Equal(t, want.Value, mt.Value, name)
		assert.Empty(t, cmp.Diff(want.Constraints, mt.Constraints), name)
	}
	pm, ok := doc.Types[componentID("PatternMap")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, pm.AdditionalProps)
	require.Len(t, pm.AdditionalProps.Patterns, 1)
	assert.Equal(t, "^x-", pm.AdditionalProps.Patterns[0].Pattern)
	rec, ok := doc.Types[componentID("OpenRecord")].(*ir.Model)
	require.True(t, ok)
	require.NotNil(t, rec.AdditionalProps, "an explicit true is a catch-all, not an unwritten keyword")
	assert.Equal(t, ir.TypeRef{Target: "t/prim/any"}, rec.AdditionalProps.Value)
	cr, ok := doc.Types[componentID("ClosedRecord")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.AdditionalClosed, cr.Additional)
	ca, ok := doc.Types[componentID("ClosedAfter")].(*ir.Model)
	require.True(t, ok)
	assert.Equal(t, ir.AdditionalClosedAfterComposition, ca.Additional)
}

// assertTuples pins what each prefixItems tuple admits past its elements and
// the bounds beside them (GitHub #597). Only `items: false` closes a tuple, and
// a typed rest is lowered into the type graph as a List's items is, at the same
// pointer and under the same hint. All of it is modeled, so no tuple keeps
// anything beside itself or reports a degradation.
func assertTuples(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	i64 := func(v int64) *int64 { return &v }
	pair := []ir.TypeRef{{Target: "t/prim/string"}, {Target: "t/prim/integer"}}
	restID := ir.TypeID("t/anon/components/schemas/PairWithRest/items")
	for _, tc := range []struct {
		name   string
		elems  []ir.TypeRef
		rest   ir.TypeID // "" = no element may follow
		bounds *ir.Constraints
	}{
		{"ClosedPair", pair, "", nil},
		{"Pair", pair, "t/prim/any", nil},
		{"PairWithRest", pair[:1], restID, nil},
		{"ExactPair", pair, "", &ir.Constraints{MinItems: i64(2)}},
		{"BoundedClosedPair", pair, "", &ir.Constraints{MinItems: i64(1), MaxItems: i64(2), UniqueItems: true}},
	} {
		tup, ok := doc.Types[componentID(tc.name)].(*ir.Tuple)
		require.True(t, ok, "%s: prefixItems hoists a Tuple; got %T", tc.name, doc.Types[componentID(tc.name)])
		assert.Equal(t, tc.elems, tup.Elems, tc.name)
		if tc.rest == "" {
			assert.Nil(t, tup.Rest, "%s: no element may follow the head", tc.name)
		} else if assert.NotNil(t, tup.Rest, tc.name) {
			assert.Equal(t, tc.rest, tup.Rest.Target, tc.name)
		}
		assert.Equal(t, tc.bounds, tup.Constraints, tc.name)
		assert.Empty(t, tup.Unmodeled, "%s: a modeled rest keeps nothing beside it", tc.name)
		assert.Empty(t, diagsAt(diags, "openapi/degraded-construct", "/components/schemas/"+tc.name), tc.name)
	}

	rest, ok := doc.Types[restID].(*ir.Model)
	require.True(t, ok, "the inline rest is hoisted at its own pointer; got %T", doc.Types[restID])
	assert.Equal(t, "pair_with_rest_item", rest.Name.Hint, "named as a List's items would be")
	_, ok = propByWire(rest, "flag")
	assert.True(t, ok, "the rest's own properties reach the IR")
}

func assertLiteralConst(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	l, ok := doc.Types[componentID("Version")].(*ir.Literal)
	require.True(t, ok, "const hoists a Literal")
	assert.Equal(t, ir.ValueString, l.Value.Kind)
	assert.Equal(t, "v1", l.Value.Str)

	// An unrepresentable const still hoists a node, and a node that admits any
	// value is the only honest one: a Literal here would have to invent a value.
	_, ok = doc.Types[componentID("Opaque")].(*ir.Any)
	assert.True(t, ok, "an unconvertible const hoists the top type; got %T", doc.Types[componentID("Opaque")])
	assert.Equal(t, []ir.Severity{ir.SeverityWarning},
		diagsAt(diags, "openapi/degraded-construct", "/components/schemas/Opaque"),
		"degrading a declared value is a warning, not silent")
}

// assertUnreferencedComponents pins GitHub #616: every component entry no
// lowering followed a reference to is kept verbatim on the document, under the
// section and name it was declared with, a link a lowered response names among
// them, while the entry the paths do name lowers as it always did. No matrix
// row: the case is a preservation claim.
func assertUnreferencedComponents(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	for _, section := range []string{
		"responses", "parameters", "examples", "requestBodies",
		"headers", "links", "callbacks", "pathItems",
	} {
		key := "openapi:components/" + section + "/Unused"
		entry, ok := doc.Unmodeled[key]
		require.True(t, ok, "%s is kept verbatim; got %v", section, slices.Sorted(maps.Keys(doc.Unmodeled)))
		assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
		assert.Equal(t, "/components/"+section+"/Unused", string(entry.Provenance.Pointer),
			"the entry is located at its own declaration, not at the carrier holding it")
	}
	assert.NotContains(t, doc.Unmodeled, "openapi:components/responses/Used",
		"an entry a reference reaches is lowered, not also kept verbatim")
	assert.Contains(t, doc.Unmodeled, "openapi:components/links/Next",
		"a link the lowered response's links map names is kept: nothing follows that reference")
	assert.Contains(t, doc.Unmodeled, "openapi:components/mediaTypes",
		"3.2's mediaTypes is kept whole until a reference into it is followed (GitHub #856)")

	op, ok := opByName(doc, "getA")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	assert.Equal(t, "reached from the paths", op.Responses[0].Docs.Description)
	assert.False(t, openapitest.HasDiag(diags, diag.UnknownObjectKey))
}
