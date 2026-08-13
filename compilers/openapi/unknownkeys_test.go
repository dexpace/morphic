// This file covers one property: a key the OpenAPI model names no field for
// reaches the IR rather than vanishing between the parser and the lowering. It
// is separate from annotations_test.go because the subject is the complement of
// what the model names, not any one annotation.
package openapi_test // external test package — exercises only the public API

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// specFile reads a census fixture the corpus sweeps also drive, so the
// assertions below and the six oracles run over the same bytes: putting one
// under testdata is what gets it compiled in both declaration orders,
// round-tripped and verified, none of which a spec written inline reaches.
func specFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "openapi", name))
	require.NoError(t, err)
	return string(data)
}

// unknownKeysSpec is the census fixture writing one undeclared key per object.
func unknownKeysSpec(t *testing.T) string {
	t.Helper()
	return specFile(t, "unknown_keys.yaml")
}

// TestUnknownKeys_KeptAtEveryObject holds the whole rule rather than the
// positions that happened to be noticed. A key the model does not name reached
// no IR field, no Unmodeled entry and no diagnostic at every one of these
// objects, so two documents differing only in it compiled to the same IR
// (GitHub #297).
//
// Carriers are derived from the value graph rather than named: each row says
// which Unmodeled map the entry must land on by the path the walk reaches it at,
// so an entry written to the wrong carrier fails here rather than passing
// because the assertion looked only where it expected.
func TestUnknownKeys_KeptAtEveryObject(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "unknown-keys", unknownKeysSpec(t))
	requireNoErrorDiagnostics(t, diags)
	sites := unmodeledSites(doc)

	for _, tc := range []struct {
		object  string
		key     string
		want    string
		carrier string
	}{
		{"openapi root", "openapi:basePath", `"ROOT"`, "doc.Unmodeled"},
		{"info", "openapi:info/contactEmail", `"INFO"`, "doc.Unmodeled"},
		{"contact", "openapi:info/contact/slack", `"CONTACT"`, "doc.Unmodeled"},
		{"license", "openapi:info/license/spdx", `"LICENSE"`, "doc.Unmodeled"},
		{"externalDocs", "openapi:externalDocs/title", `"EXTERNALDOCS"`, "doc.Unmodeled"},
		{"server", "openapi:host", `"SERVER"`, "doc.Servers[0].Unmodeled"},
		{"server variable", "openapi:example", `"SERVERVARIABLE"`, "doc.Servers[0].Variables[0].Unmodeled"},
		{"tag", "openapi:tags/0/color", `"TAG"`, "doc.Unmodeled"},
		{"components", "openapi:components/definitions", `"COMPONENTS"`, "doc.Unmodeled"},
		{"tag externalDocs", "openapi:tags/0/externalDocs/title", `"TAGEXTERNALDOCS"`, "doc.Unmodeled"},
		{"operation", "openapi:operationid", `"OPERATION"`, ".Unmodeled"},
		// On each operation of the item, like its servers and its x-*: the path
		// item lowers to no node of its own, so the scope is what tells its keys
		// from the operation's own on the one map they share.
		{"path item", "openapi:pathItem/GET", `{"responses":{"200":{"description":"PATHITEM"}}}`,
			"Operations[0].Unmodeled"},
		// The carrier is named down to the operation rather than left at
		// ".Unmodeled": the document writes this exact key too, and the row is only
		// evidence of the operation's if it cannot match the document's.
		{"operation externalDocs", "openapi:externalDocs/title", `"OPERATIONEXTERNALDOCS"`,
			"Operations[0].Unmodeled"},
		{"parameter", "openapi:collectionFormat", `"PARAMETER"`, ".Params[0].Unmodeled"},
		{"example", "openapi:name", `"EXAMPLE"`, ".Params[0].Examples[0].Unmodeled"},
		{"request body", "openapi:schema", `"REQUESTBODY"`, ".Request.Unmodeled"},
		{"media type", "openapi:format", `"MEDIATYPE"`, ".Contents[0].Unmodeled"},
		// On the content rather than on the part: ir.PartEncoding holds no
		// Unmodeled map, and one content can carry an entry per part, so the part
		// name is what tells two encodings' keys apart on that one map.
		{"encoding", "openapi:encoding/part/contentEncoding", `"ENCODING"`, ".Contents[1].Unmodeled"},
		{"response", "openapi:status", `"RESPONSE"`, ".Responses[0].Unmodeled"},
		{"error response", "openapi:status", `"ERRORRESPONSE"`, ".Errors[0].Unmodeled"},
		{"header", "openapi:in", `"HEADER"`, ".Headers[0].Unmodeled"},
		{"components", "openapi:components/definitions", `"COMPONENTS"`, "doc.Unmodeled"},
		{"security scheme", "openapi:tokenUrl", `"SECURITYSCHEME"`,
			"doc.Auth[auth/openapi/components/securitySchemes/k].Unmodeled"},
		{"oauth flows", "openapi:flows/application", `"OAUTHFLOWS"`,
			"doc.Auth[auth/openapi/components/securitySchemes/o].Unmodeled"},
		{"oauth flow", "openapi:scope", `"OAUTHFLOW"`,
			"doc.Auth[auth/openapi/components/securitySchemes/o].Flows[0].Unmodeled"},
		{"schema", "openapi:additionalItems", `"SCHEMA"`,
			"doc.Types[t/openapi/components/schemas/S].Unmodeled"},
		// The property's schema reduced to a shared primitive, so it owns no node
		// and its keywords stay on the declaring property — the carrier rule
		// fillPropertyAnnotations already applies to every other annotation.
		{"property schema", "openapi:divisibleBy", `"PROPERTYSCHEMA"`,
			"doc.Types[t/openapi/components/schemas/S].Properties[0].Unmodeled"},
	} {
		site, found := findUnmodeled(sites, tc.key, tc.want)
		if !assert.True(t, found, "%s drops %s = %s: it is nowhere in the document",
			tc.object, tc.key, tc.want) {
			continue
		}
		assert.Contains(t, site.path, tc.carrier, "%s key lands on the wrong carrier", tc.object)
	}
}

// TestUnknownKeys_SchemaAndObjectAreGradedApart pins the one distinction the
// census turns on, at the two keys the fixture picks for it: a draft-07
// additionalItems in a schema, and an operationId with the case wrong on an
// operation.
//
// JSON Schema states that an implementation must ignore a keyword it does not
// recognize, so an unrecognized keyword in a schema is legal input and may carry
// meaning for other tooling; OpenAPI states that an extension key must be
// prefixed x-, so an undefined key on one of its objects is not an extension but
// a defect in the document.
//
// Both are kept — invariant 2 does not bend for invalid input — and both carry
// the same reason, because "no IR node is coming" is true of each. What differs
// is what the document did, which is the diagnostic channel's subject: an
// unrecognized keyword records a decision at info, and an undefined key reports
// a fault at warning.
func TestUnknownKeys_SchemaAndObjectAreGradedApart(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "unknown-keys", unknownKeysSpec(t))
	requireNoErrorDiagnostics(t, diags)

	schema, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")]
	require.True(t, ok)
	keyword := unmodeledEntry(t, schema.Common().Unmodeled, "openapi:additionalItems")
	assert.Equal(t, ir.ReasonOutOfScope, keyword.Reason,
		"no IR node is coming for a keyword this compiler does not model")
	assert.Equal(t, "/components/schemas/S/additionalItems", keyword.Provenance.Pointer)
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/unknown-schema-keyword", "/components/schemas/S/additionalItems"))

	op, ok := opByName(doc, "listWidgets")
	require.True(t, ok)
	key := unmodeledEntry(t, op.Unmodeled, "openapi:operationid")
	assert.Equal(t, ir.ReasonOutOfScope, key.Reason,
		"OpenAPI defines no such key, so no IR node is coming for it either")
	assert.Equal(t, "/paths/~1widgets/get/operationid", key.Provenance.Pointer)
	assert.Equal(t, []ir.Severity{ir.SeverityWarning},
		diagsAt(diags, "openapi/unknown-object-key", "/paths/~1widgets/get/operationid"))
}

// TestUnknownKeys_SpellingDecidesNeitherEntryNorCarrier holds the census to the
// two spellings that used to lose a key outright, each of which put a document
// writing it and a document writing nothing at all into the same IR.
//
// An aliased key is reported by the parser under the name it resolves to, while
// the mapping node it was written on still holds an alias whose own value is the
// anchor. Searching that mapping for the resolved name found nothing, and a key
// with no node kept nothing and said nothing.
//
// A key holding a "/" collided with the scope of the object that path names:
// entries for the objects with no Unmodeled map of their own are keyed by the
// path from the carrier down, so a root key spelled "info/contact/slack" was the
// same entry as the contact object's own "slack", and the site that reached the
// carrier second found it taken and dropped its key in silence. Escaping the key
// as one segment is what separates them, per ids.Scope.
func TestUnknownKeys_SpellingDecidesNeitherEntryNorCarrier(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "spellings", specFile(t, "unknown_key_spellings.yaml"))
	requireNoErrorDiagnostics(t, diags)
	sites := unmodeledSites(doc)

	for _, tc := range []struct {
		spelling string
		key      string
		want     string
	}{
		{"a key written as an alias", "openapi:info/license/spdx", `"LICENSE_ALIASED"`},
		{"a root key spelling another object's scope", "openapi:info~1contact~1slack", `"ROOT_SLASHED"`},
		{"the scope that key spells", "openapi:info/contact/slack", `"CONTACT_PLAIN"`},
	} {
		site, found := findUnmodeled(sites, tc.key, tc.want)
		if !assert.True(t, found, "%s drops %s = %s: it is nowhere in the document",
			tc.spelling, tc.key, tc.want) {
			continue
		}
		assert.Equal(t, ir.ReasonOutOfScope, site.entry.Reason,
			"%s is the census's, not the extension reader's", tc.spelling)
	}
	assert.Len(t, diagsAt(diags, "openapi/unknown-object-key", "/info/license/spdx"), 1,
		"an aliased key is announced at the name it resolves to")
}

// TestUnknownKeys_ClashingCarrierEntryIsReportedNotDropped covers the one
// carrier the census cannot key its way out of. A parameter and its schema are
// two objects at two pointers whose entries share one unscoped map, so a key both
// of them write is one entry, and the schema's reaches it first.
//
// The key that loses is announced with the pointer of the construct holding the
// entry, rather than skipped by the branch meant for a keyword another reader
// already said better. It is still in the IR in no form at all: separating the
// namespaces moves keys #345, #348 and the validation-only reader already
// publish, which is GitHub #396 and not this census's to settle.
func TestUnknownKeys_ClashingCarrierEntryIsReportedNotDropped(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "clash", specFile(t, "unknown_key_carrier_clash.yaml"))
	requireNoErrorDiagnostics(t, diags)
	sites := unmodeledSites(doc)

	for _, tc := range []struct{ carrier, held, lost string }{
		{"parameter", "/paths/~1widgets/get/parameters/0/schema/divisibleBy",
			"/paths/~1widgets/get/parameters/0/divisibleBy"},
		{"header", "/paths/~1widgets/get/responses/200/headers/X-Trace/schema/divisibleBy",
			"/paths/~1widgets/get/responses/200/headers/X-Trace/divisibleBy"},
	} {
		assert.Equal(t, []ir.Severity{ir.SeverityWarning},
			diagsAt(diags, "openapi/unknown-key-entry-taken", tc.lost),
			"the %s key that lost the entry is announced at its own pointer", tc.carrier)
		_, found := findUnmodeled(sites, "openapi:divisibleBy", `"FROM_`+strings.ToUpper(tc.carrier)+`_OBJECT"`)
		assert.False(t, found, "the %s's own key is in the IR in no form at all, as reported", tc.carrier)
	}
}

// TestUnknownKeys_WellFormedDocumentRecordsNothing is the control the census
// needs: a document writing only what the model names keeps no entry and reports
// no finding, so the rows above are evidence of the keys they name rather than
// of a sweep that fires on everything.
func TestUnknownKeys_WellFormedDocumentRecordsNothing(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "clean", `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses: {"200": {description: ok}}
components:
  schemas:
    S: {type: object, properties: {a: {type: string}}}
`)
	requireNoErrorDiagnostics(t, diags)

	assert.Empty(t, unmodeledSites(doc), "nothing undeclared, so nothing kept")
	for _, d := range diags {
		assert.NotContains(t, d.Code, "unknown-", "a well-formed document reports no unknown key: %+v", d)
	}
}

// TestUnknownKeys_SchemaSubObjects covers the three objects that hang off a
// schema — its xml, its discriminator and its externalDocs — which the fixture
// above deliberately leaves out.
//
// It leaves them out because the OpenAPI dialect meta-schema closes all three to
// anything but an x- key, so the library reports a validation error on each and
// harness.Check returns at the first one, before the oracles that fixture exists
// to reach. The keys are kept and announced all the same, which is what this
// asserts: an invalid document is still not a document whose keys may vanish.
//
// Graded as an OpenAPI object's keys rather than as schema keywords, at warning
// rather than info: JSON Schema's rule that an unrecognized keyword is legal
// governs the schema, and these three are OpenAPI objects the schema vocabulary
// says nothing about. All three ride on the schema's own map, since none of
// ir.XMLHints, ir.Discriminator or ir.Link holds one, and the keyword each was
// written under is what keeps them apart there.
func TestUnknownKeys_SchemaSubObjects(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "schema-sub-objects", `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      xml: {name: s, attribute2: XML}
      discriminator: {propertyName: k, mapping2: DISCRIMINATOR}
      externalDocs: {url: 'https://d.example', title: SCHEMAEXTERNALDOCS}
      properties: {k: {type: string}}
`)
	schema, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")]
	require.True(t, ok)

	// assert rather than require on the lookup, so a keyword missing from the map
	// reports itself and leaves the other two still checked. The three share one
	// reader, and a row that never runs is no evidence about the keyword it names.
	for _, tc := range []struct{ keyword, key, want string }{
		{"xml", "openapi:xml/attribute2", `"XML"`},
		{"discriminator", "openapi:discriminator/mapping2", `"DISCRIMINATOR"`},
		{"externalDocs", "openapi:externalDocs/title", `"SCHEMAEXTERNALDOCS"`},
	} {
		entry, found := schema.Common().Unmodeled[tc.key]
		if !assert.True(t, found, "schema %s drops %s: it is nowhere on the schema", tc.keyword, tc.key) {
			continue
		}
		assert.Equal(t, tc.want, string(entry.Value), "%s key keeps what the source wrote", tc.keyword)
		assert.Equal(t, ir.ReasonOutOfScope, entry.Reason, "%s key reason", tc.keyword)
		at := "/components/schemas/S/" + tc.keyword + "/" + tc.key[len("openapi:"+tc.keyword+"/"):]
		assert.Equal(t, at, entry.Provenance.Pointer, "%s key provenance", tc.keyword)
		assert.Equal(t, []ir.Severity{ir.SeverityWarning},
			diagsAt(diags, "openapi/unknown-object-key", at),
			"%s key is announced as an object's, not as a schema keyword's", tc.keyword)
	}
}

// TestUnknownKeys_PathItemKeyKeepsItsValue compiles the reproducer from GitHub
// #377 and holds the half of it that used to go missing.
//
// The other half never did: folding the key into the item's operations map makes
// the library reject the scalar sitting where an Operation should be, so the
// error below is the diagnostic that named the key all along. What no channel
// carried is the 1 it was written with, which is what the entry keeps — and the
// warning beside it is the census's own, graded like every other undeclared key.
func TestUnknownKeys_PathItemKeyKeepsItsValue(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "path-item-key", `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    bogusPathItem: 1
    get: {operationId: getX, responses: {"200": {description: ok}}}
`)
	op, ok := opByName(doc, "getX")
	require.True(t, ok)

	entry := unmodeledEntry(t, op.Unmodeled, "openapi:pathItem/bogusPathItem")
	assert.JSONEq(t, `1`, string(entry.Value), "the key keeps the value the source wrote")
	assert.Equal(t, ir.ReasonOutOfScope, entry.Reason,
		"OpenAPI defines no such key on a path item, so no IR node is coming for it")
	assert.Equal(t, "/paths/~1x/bogusPathItem", entry.Provenance.Pointer)
	assert.Equal(t, []ir.Severity{ir.SeverityWarning},
		diagsAt(diags, "openapi/unknown-object-key", "/paths/~1x/bogusPathItem"))
	_, isError := ir.FirstError(diags)
	assert.True(t, isError, "the library still rejects the scalar the key was written with")
}

// TestUnknownKeys_PathItemDeclaredFieldsAreNotUndeclared is the control the path
// item's census needs, and the one that decides whether reading its operations
// map is sound at all.
//
// `query` is the case that decides it. OpenAPI 3.2 adds the method, and the
// library puts it in the same map as every other operation, so a census taken
// against the eight methods this compiler lowers would report a valid operation
// as a key the specification does not define. Taking it against the library's own
// method vocabulary is what keeps the two questions apart: whether a key names a
// method, and whether this compiler lowers it — the second being GitHub #293.
//
// The rest of a path item's fields are here because a census is only evidence
// about the keys it leaves alone. Each is a field of the library's model and so
// never reaches the operations map, which is the property being pinned:
// additionalOperations included, since #293 has it dropped rather than
// undeclared.
func TestUnknownKeys_PathItemDeclaredFieldsAreNotUndeclared(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "path-item-fields", `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /x:
    $ref: '#/components/pathItems/P'
  /y:
    summary: s
    description: d
    servers: [{url: 'https://a.example'}]
    parameters: [{name: p, in: query, schema: {type: string}}]
    additionalOperations:
      PURGE: {operationId: purgeY, responses: {"200": {description: ok}}}
    get:   {operationId: getY, responses: {"200": {description: ok}}}
    query: {operationId: queryY, responses: {"200": {description: ok}}}
components:
  pathItems:
    P:
      summary: ps
      description: pd
      get: {operationId: getP, responses: {"200": {description: ok}}}
`)
	requireNoErrorDiagnostics(t, diags)

	for _, d := range diags {
		assert.NotEqual(t, "openapi/unknown-object-key", d.Code,
			"every key here is one a path item declares: %+v", d)
	}
	for _, site := range unmodeledSites(doc) {
		assert.NotContains(t, site.key, "openapi:pathItem/",
			"nothing a path item declares is kept as a key it does not: %s at %s", site.key, site.path)
	}
	// An absence is only evidence where the reader ran, and the census runs once
	// per operation lowered off a path item. Both items here have to reach it,
	// the $ref'd one included, or the rows above hold over a walk that never
	// looked.
	for _, name := range []string{"getP", "getY"} {
		_, ok := opByName(doc, name)
		assert.True(t, ok, "%s lowers, so the census ran on the path item declaring it", name)
	}
}

// requireNoErrorDiagnostics fails the test on the first error-severity
// diagnostic, naming it.
func requireNoErrorDiagnostics(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	d, ok := ir.FirstError(diags)
	require.False(t, ok, "unexpected error diagnostic: %+v", d)
}

// TestUnknownKeys_PathItemWithNoOperationKeepsWhatItWrote covers the one place
// the census had no carrier at all.
//
// applyPathItem runs once per operation an item produces, so an item producing
// none — because it declares no method this compiler lowers, or because the only
// keys it holds are ones the Path Item Object does not define — reached the
// census through nothing, and its servers, extensions and undeclared keys were
// dropped whole with no diagnostic naming the loss.
//
// The service is where they go, which is where the Paths Object's own extensions
// already go for the same reason: a path item lowers to no node, so the nearest
// node holding an Unmodeled map is what holds them. The key carries the item's
// own pointer, because one service holds every such item and a bare prefix would
// let two of them collide.
func TestUnknownKeys_PathItemWithNoOperationKeepsWhatItWrote(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "path-item-unmounted", `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /mounted:
    x-on-mounted: {a: 1}
    get: {operationId: getX, responses: {"200": {description: ok}}}
  /unmounted:
    servers: [{url: 'https://a.example'}]
    x-on-unmounted: {b: 2}
    undeclaredKey: {c: 3}
webhooks:
  hook:
    x-on-hook: {d: 4}
`)
	// Not requireNoErrorDiagnostics: an object-valued undeclared key folds into
	// the operations map as an operation with no responses, so the library reports
	// a required-field error naming it. That is the fold this census exists to see
	// past, not a problem with the fixture.

	keys := map[string]bool{}
	for _, site := range unmodeledSites(doc) {
		keys[site.key] = true
	}
	assert.True(t, keys["openapi:pathItem/x-on-mounted"],
		"an item with an operation still keeps its own on that operation")
	for _, want := range []string{
		"openapi:pathItem/paths/~1unmounted/x-on-unmounted",
		"openapi:pathItem/paths/~1unmounted/undeclaredKey",
		"openapi:pathItem/paths/~1unmounted/servers",
		"openapi:pathItem/webhooks/hook/x-on-hook",
	} {
		assert.True(t, keys[want], "kept on the service: %s (have %v)", want, keys)
	}

	// And the loss is announced rather than merely repaired: a reader finding
	// these on the service needs to know why they are not on an operation.
	var announced int
	for _, d := range diags {
		if d.Code == "openapi/degraded-construct" &&
			strings.Contains(d.Message, "declares no operation this compiler lowers") {
			announced++
			assert.Equal(t, ir.SeverityWarning, d.Severity)
		}
	}
	assert.Equal(t, 2, announced, "one per unmounted item: the path and the webhook")
}
