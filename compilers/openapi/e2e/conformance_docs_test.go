package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func assertDeprecation(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "oldOp")
	require.True(t, ok)
	assert.NotNil(t, op.Deprecation)
	m, ok := doc.Types[namedID("OldModel")].(*ir.Model)
	require.True(t, ok)
	assert.NotNil(t, m.Deprecation)
	require.Len(t, m.Properties, 1)
	assert.NotNil(t, m.Properties[0].Deprecation)

	// A parameter is its own carrier: deprecated written on the parameter object
	// reaches it without any schema being involved.
	legacy, ok := paramByName(op, "legacy")
	require.True(t, ok)
	assert.NotNil(t, legacy.Deprecation)
}

func assertExamples(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok)

	// A schema that reduced to a shared primitive keeps its examples on the
	// declaring property; one that hoists a node of its own keeps them there.
	n, ok := propByWire(m, "n")
	require.True(t, ok)
	assert.Len(t, n.Examples, 2)
	inner, ok := propByWire(m, "inner")
	require.True(t, ok)
	assert.Empty(t, inner.Examples, "an inline object holds its own examples")
	require.Len(t, doc.Types[inner.Type.Target].Common().Examples, 1)

	// A component's example belongs to the component, not to a use site.
	c, ok := doc.Types[namedID("C")]
	require.True(t, ok)
	require.Len(t, c.Common().Examples, 1)
	require.NotNil(t, c.Common().Examples[0].Value)
	assert.Equal(t, "component-level", c.Common().Examples[0].Value.Str)

	// A parameter carries its own examples, keyed by their map key like a media
	// type's.
	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	q, ok := paramByName(op, "q")
	require.True(t, ok)
	require.Len(t, q.Examples, 1)
	assert.Equal(t, "one", q.Examples[0].Name)
	require.NotNil(t, q.Examples[0].Value)
	assert.Equal(t, "first", q.Examples[0].Value.Str)

	assertResponseExamples(t, doc)
}

// assertResponseExamples pins the response-side example sites: the plural map on
// a media type, in every spelling the spec allows, and a response header.
func assertResponseExamples(t *testing.T, doc *ir.Document) {
	t.Helper()
	op, ok := opByName(doc, "getItem")
	require.True(t, ok)
	require.Len(t, op.Responses[0].Payload.Contents, 1)
	ex := op.Responses[0].Payload.Contents[0].Examples
	require.Len(t, ex, 4, "every entry lowers, in source order")

	// An inline entry and one written as a $ref, which must resolve to the
	// referenced component's value. Both are named by their map key.
	require.NotNil(t, ex[0].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "hello"}, *ex[0].Value)
	assert.Equal(t, "inline", ex[0].Name)
	require.NotNil(t, ex[1].Value)
	assert.Equal(t, ir.Value{Kind: ir.ValueString, Str: "world"}, *ex[1].Value)
	assert.Equal(t, "shared", ex[1].Name)

	// The annotations that surround a value travel with it.
	assert.Equal(t, "a summary", ex[2].Summary)
	assert.Equal(t, "a description", ex[2].Description)

	// An externalValue entry carries no inline value, and is kept for the URL.
	assert.Equal(t, "https://example.com/e.json", ex[3].ExternalURL)
	assert.Nil(t, ex[3].Value)
	assert.Equal(t, "hosted elsewhere", ex[3].Summary)

	headers := op.Responses[0].Headers
	require.Len(t, headers, 1)
	require.Len(t, headers[0].Examples, 1)
	require.NotNil(t, headers[0].Examples[0].Value)
	assert.Equal(t, ir.BigVal("5"), headers[0].Examples[0].Value.Num)
}

// assertSelfURI pins that a declared `$self` reaches the source's entry rather
// than a lowered node, since it says where one input document lives. The case
// witnesses no matrix row: the rows are what an API can express, and the URI a
// description gives itself is not part of the API it describes.
func assertSelfURI(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	t.Helper()
	require.Len(t, doc.Sources, 1)
	assert.Equal(t, "openapi@3.2", doc.Sources[0].Format)
	assert.Equal(t, "https://example.com/api/openapi.yaml", doc.Sources[0].SelfURI)
}

func assertDocsSummaryDesc(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "ping")
	require.True(t, ok)
	assert.Equal(t, "Ping the server", op.Docs.Summary)
	assert.Equal(t, "Returns pong", op.Docs.Description)
	require.Len(t, op.Docs.ExternalDocs, 1)
	assert.Equal(t, ir.Link{URL: "https://example.com/docs/ping", Description: "Ping reference"},
		op.Docs.ExternalDocs[0])

	// A documented scalar alias is documented too: its docs must not depend on
	// what its body happens to lower to (GitHub #114).
	sc, ok := doc.Types[namedID("UserId")].(*ir.Scalar)
	require.True(t, ok)
	assert.Equal(t, "User identifier", sc.Docs.Summary)
	assert.Equal(t, "Stable identifier for a user.", sc.Docs.Description)

	// info and the root externalDocs are one Docs on the document: summary and
	// description come from info, the link from beside it.
	assert.Equal(t, "A one-line summary of the API.", doc.Docs.Summary)
	assert.Equal(t, "The long-form description of the API.", doc.Docs.Description)
	require.Len(t, doc.Docs.ExternalDocs, 1)
	assert.Equal(t, ir.Link{URL: "https://example.com/docs", Description: "Full guide"},
		doc.Docs.ExternalDocs[0])
	require.NotNil(t, doc.License)
	assert.Equal(t, ir.License{Name: "MIT", Identifier: "MIT"}, *doc.License,
		"the 3.1 SPDX identifier is its own field, never folded into the name")
}

// assertRequestBodyDocs pins GitHub #609: a Request Body Object's `description`
// describes the body rather than any media type inside it, so it reaches
// Payload.Docs. Both spellings are covered — a body $ref'd from components and
// one written inline — and the shared component is reached from two operations,
// so the declaration-pointer path is exercised and one declaration is still one
// type however many mounts read its docs.
func assertRequestBodyDocs(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	created, ok := opByName(doc, "createOrder")
	require.True(t, ok)
	require.NotNil(t, created.Request)
	assert.Equal(t, "A shared order body.", created.Request.Docs.Description,
		"a $ref'd body keeps the component's description")

	replaced, ok := opByName(doc, "replaceOrder")
	require.True(t, ok)
	require.NotNil(t, replaced.Request)
	assert.Equal(t, "A shared order body.", replaced.Request.Docs.Description,
		"the second mount reads the same declaration's docs")

	shared := ir.TypeID("t/anon/components/requestBodies/OrderBody/content/application~1json/schema")
	assert.Equal(t, shared, openapitest.BodyTarget(t, created.Request),
		"a shared body interns at its component pointer (issue #107)")
	assert.Equal(t, shared, openapitest.BodyTarget(t, replaced.Request),
		"...once, whichever mount lowered it first")

	draft, ok := opByName(doc, "saveDraft")
	require.True(t, ok)
	require.NotNil(t, draft.Request)
	assert.Equal(t, "A draft saved inline.", draft.Request.Docs.Description,
		"an inline body's own description reaches the payload too")
}

// assertRefSiteDocs pins GitHub #610 across every site the fold reaches, in one
// document: a Reference Object may write `summary` and `description` beside its
// `$ref`, and OpenAPI says they override the referenced object's. Nothing read
// them, so a mount's own documentation was silently replaced by the
// declaration's. The one component referenced from two operations with
// different overrides also pins the other half: each mount keeps its own and
// neither mutates the declaration.
func assertRefSiteDocs(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	widgets, ok := opByName(doc, "listWidgets")
	require.True(t, ok)
	gadgets, ok := opByName(doc, "listGadgets")
	require.True(t, ok)

	// parameters — the two-mount case: the first writes both siblings, the second
	// only a description, and the declaration's own description is overridden by
	// both rather than surviving on either.
	widgetLimit, ok := paramByName(widgets, "limit")
	require.True(t, ok)
	assert.Equal(t, "Page size", widgetLimit.Docs.Summary)
	assert.Equal(t, "How many widgets to return.", widgetLimit.Docs.Description)
	gadgetLimit, ok := paramByName(gadgets, "limit")
	require.True(t, ok)
	assert.Empty(t, gadgetLimit.Docs.Summary, "the second mount writes no summary, so none is invented")
	assert.Equal(t, "How many gadgets to return.", gadgetLimit.Docs.Description)

	// responses — one mount overrides the description, the other keeps the
	// declaration's, so neither mutates it.
	require.Len(t, widgets.Responses, 1)
	assert.Equal(t, "The listing, as this operation returns it.", widgets.Responses[0].Docs.Description)
	require.Len(t, gadgets.Responses, 1)
	assert.Equal(t, "A page of results.", gadgets.Responses[0].Docs.Description,
		"a mount with no siblings leaves the declaration's description alone")

	// error cases are responses too, and reach the same shared lowering.
	require.Len(t, widgets.Errors, 1)
	assert.Equal(t, "No such widget.", widgets.Errors[0].Docs.Description)

	// headers
	require.Len(t, widgets.Responses[0].Headers, 1)
	assert.Equal(t, "The unit a rate limit is stated in.", widgets.Responses[0].Headers[0].Docs.Description)

	// examples — the siblings override the Example Object's own pair.
	require.NotNil(t, widgets.Responses[0].Payload)
	require.Len(t, widgets.Responses[0].Payload.Contents[0].Examples, 1)
	example := widgets.Responses[0].Payload.Contents[0].Examples[0]
	assert.Equal(t, "One page", example.Summary)
	assert.Equal(t, "A single page of results.", example.Description)

	// request bodies
	require.NotNil(t, widgets.Request)
	assert.Equal(t, "The widget to create.", widgets.Request.Docs.Description)

	// security schemes — the aliasing entry takes its own description, and the
	// declaration it names keeps its own; an alias of the alias writes none, so
	// it takes the description of the hop beneath it.
	require.Len(t, doc.Auth, 3)
	byName := map[string]string{}
	for _, scheme := range doc.Auth {
		byName[scheme.Name.Source] = scheme.Docs.Description
	}
	assert.Equal(t, "The key this API expects.", byName["ApiKey"])
	assert.Equal(t, "The declaration's own scheme description.", byName["BaseKey"],
		"the aliasing entry's siblings do not reach the declaration")
	assert.Equal(t, "The key this API expects.", byName["KeyAlias"])

	assertAliasHopDocs(t, doc)
}

// assertAliasHopDocs pins the siblings of an alias, a component that is itself a
// $ref: they override the declaration it names at every position, and a use
// site's own override them in turn.
func assertAliasHopDocs(t *testing.T, doc *ir.Document) {
	sprocket, ok := opByName(doc, "makeSprocket")
	require.True(t, ok)
	limit, ok := paramByName(sprocket, "limit")
	require.True(t, ok)
	assert.Equal(t, ir.Docs{Summary: "Sprocket page size", Description: "The alias's own description."}, limit.Docs,
		"the use site's summary over the alias's description over the declaration's")
	require.NotNil(t, sprocket.Request)
	assert.Equal(t, "The alias's own body description.", sprocket.Request.Docs.Description)
	require.Len(t, sprocket.Responses, 1)
	require.Len(t, sprocket.Responses[0].Headers, 1)
	assert.Equal(t, "The alias's own header description.", sprocket.Responses[0].Headers[0].Docs.Description)
	require.Len(t, sprocket.Responses[0].Payload.Contents[0].Examples, 1)
	example := sprocket.Responses[0].Payload.Contents[0].Examples[0]
	assert.Equal(t, "The alias's own summary", example.Summary)
	assert.Equal(t, "Declared description", example.Description, "the field the alias omits stands")
	require.Len(t, sprocket.Errors, 1)
	assert.Equal(t, "The alias's own response description.", sprocket.Errors[0].Docs.Description)
}

// assertExamples32 pins GitHub #612 at every site the issue names: the 3.2
// dataValue lowers like `value`, including through a components/examples $ref,
// and a serializedValue is kept verbatim under Unmodeled wherever it is written,
// alone or beside a dataValue, rather than dropped.
func assertExamples32(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	op, ok := opByName(doc, "list")
	require.True(t, ok)

	// parameters
	limit, ok := paramByName(op, "limit")
	require.True(t, ok)
	require.Len(t, limit.Examples, 1)
	require.NotNil(t, limit.Examples[0].Value, "a parameter's dataValue reaches Example.Value")
	assert.Equal(t, ir.BigVal("5"), limit.Examples[0].Value.Num)

	// headers
	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)
	headerExample := op.Responses[0].Headers[0].Examples
	require.Len(t, headerExample, 1)
	assert.Equal(t, ir.ReasonNoIRHome, headerExample[0].Unmodeled["openapi:serializedValue"].Reason,
		"a header's serializedValue keeps the entry verbatim")

	// content, and through it components/examples
	examples := op.Responses[0].Payload.Contents[0].Examples
	require.Len(t, examples, 4)
	byName := map[string]ir.Example{}
	for _, e := range examples {
		byName[e.Name] = e
	}
	require.NotNil(t, byName["five"].Value)
	assert.Equal(t, ir.BigVal("5"), byName["five"].Value.Num)
	require.NotNil(t, byName["one"].Value, "a $ref'd components/examples entry lowers its dataValue")
	assert.Equal(t, ir.BigVal("1"), byName["one"].Value.Num)
	assert.Equal(t, ir.ReasonNoIRHome, byName["serial"].Unmodeled["openapi:serializedValue"].Reason)
	assert.Nil(t, byName["serial"].Value)
	require.NotNil(t, byName["both"].Value, "a dataValue beside a serializedValue still lowers")
	assert.Equal(t, ir.BigVal("2"), byName["both"].Value.Num)
	assert.JSONEq(t, `"\"2\""`, string(byName["both"].Unmodeled["openapi:serializedValue"].Value),
		"...and the serialized form beside it is kept too")

	openapitest.AssertInfoDiagAt(t, diags,
		"/paths/~1a/get/responses/200/content/application~1json/examples/both/serializedValue")
	openapitest.AssertInfoDiagAt(t, diags, "/components/examples/Serial/serializedValue")
}
