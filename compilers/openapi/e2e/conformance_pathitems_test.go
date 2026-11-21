// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func assertWebhooks(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	irOperation, ok := operationByName(doc, "onNewPet")
	require.True(t, ok)
	require.Len(t, irOperation.Bindings.HTTP, 1)
	assert.Equal(t, ir.HTTPInboundWebhook, irOperation.Bindings.HTTP[0].Inbound, "a webhook operation is marked inbound as one")
	assert.Equal(t, "newPet", irOperation.Bindings.HTTP[0].WebhookName, "the webhooks-map key names the event")
	assert.Empty(t, irOperation.Bindings.HTTP[0].URITemplate, "a webhook carries no URI template")
	assertOwnServersOverrideThePathItems(t, doc, irOperation, "https://hooks-override.example.com",
		"/webhooks/newPet/servers/0")
	assertWebhookGroupIsAHint(t, doc)

	// Two keys a URI template would misstate: braces that belong to the event's
	// name, and the empty key, which names nothing. Neither declares an
	// operationId, so the hint is built from the method and the key.
	operations := allOperations(doc)
	for _, hook := range []struct {
		id        ir.OperationID
		key, hint string
	}{
		{"operation/openapi/webhooks/{tenant}.created/post", "{tenant}.created", "post_tenant_created"},
		{"operation/openapi/webhooks//post", "", "post"},
	} {
		i := slices.IndexFunc(operations, func(operation ir.Operation) bool { return operation.ID == hook.id })
		require.GreaterOrEqual(t, i, 0, "the webhook keyed %q is mounted", hook.key)
		require.Len(t, operations[i].Bindings.HTTP, 1)
		b := operations[i].Bindings.HTTP[0]
		assert.Equal(t, ir.HTTPInboundWebhook, b.Inbound)
		assert.Equal(t, hook.key, b.WebhookName, "the key, verbatim")
		assert.Empty(t, b.URITemplate, "a webhook has no URI template")
		assert.Equal(t, hook.hint, operations[i].Name.Hint)
	}
}

// assertOwnServersOverrideThePathItems pins the overriding half of the servers
// pair in the corpus, so the two-order oracle and the JSON round-trip see it.
// OpenAPI says an operation's own servers override its path item's, so the
// operation is sent to its own, `url`; the path item's, declared at
// overriddenAt, are registered all the same and named by none of its
// operations.
func assertOwnServersOverrideThePathItems(t *testing.T, doc *ir.Document, operation ir.Operation, url, overriddenAt string) {
	t.Helper()
	require.Len(t, operation.Servers, 1, "the operation names its own server")
	assert.Equal(t, url, doc.Servers[operation.Servers[0]].URLTemplate)
	overridden := ir.ServerID("server/openapi" + overriddenAt)
	require.Contains(t, doc.Servers, overridden, "the overridden path item's server is still registered")
	assert.NotContains(t, operation.Servers, overridden)
	assert.NotContains(t, operation.Unmodeled, "openapi:servers", "neither level is kept verbatim")
	assert.NotContains(t, operation.Unmodeled, "openapi:operationServers")
}

// assertPathItemServersNamed pins that a path item's servers reach its
// operations whichever parent the path item hangs from. They used to be read
// only on the `paths` walk, so the same override written on a webhook or a
// callback disappeared with nothing said (GitHub #39).
func assertPathItemServersNamed(t *testing.T, doc *ir.Document, operation ir.Operation, url string) {
	t.Helper()
	require.Len(t, operation.Servers, 1, "the operation names its path item's server")
	assert.Equal(t, url, doc.Servers[operation.Servers[0]].URLTemplate)
	assert.NotContains(t, operation.Unmodeled, "openapi:servers", "and keeps nothing of it verbatim")
}

// assertWebhookGroupIsAHint pins which of the two things the group's name is.
// The compiler synthesizes the group to hold webhook operations, so no document
// declares it and Naming.Source — the spelling the source used — is the wrong
// channel; the "default" group the tag walk synthesizes carries a Hint for the
// same reason (GitHub #184).
//
// Asserted here rather than left to the golden because the golden records the
// value either way: a reader diffing it sees a name move between two fields
// without being told which one is correct.
func assertWebhookGroupIsAHint(t *testing.T, doc *ir.Document) {
	t.Helper()
	require.Len(t, doc.Services, 1)
	for _, operationGroup := range doc.Services[0].Groups {
		if operationGroup.Name.Hint != "webhooks" {
			continue
		}
		assert.Empty(t, operationGroup.Name.Source, "a group no document declares carries no source spelling")
		assert.Empty(t, operationGroup.Name.Canonical, "and no words derived from one")
		return
	}
	t.Fatal("no group carries the webhooks hint")
}

func assertCallbacks(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	operation, ok := operationByName(doc, "subscribe")
	require.True(t, ok)
	require.Len(t, operation.Bindings.HTTP, 1)
	require.Len(t, operation.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, "{$request.body#/callbackUrl}", operation.Bindings.HTTP[0].Callbacks[0].Expression)
	assert.NotEmpty(t, operation.Bindings.HTTP[0].Callbacks[0].Operations)
	cb, ok := operationByName(doc, "onEvent")
	require.True(t, ok, "the callback operation is registered alongside its parent")
	assertPathItemServersNamed(t, doc, cb, "https://callbacks.example.com")
	assert.Empty(t, operation.Servers, "the callback's servers stay on the callback, not on the parent")

	reply, ok := operationByName(doc, "onReply")
	require.True(t, ok, "a callback declared on a callback's operation lowers too")
	require.Len(t, cb.Bindings.HTTP[0].Callbacks, 1)
	nested := cb.Bindings.HTTP[0].Callbacks[0]
	assert.Equal(t, ir.Callback{Name: "onReply", Expression: "{$response.body#/replyUrl}", Operations: []ir.OperationID{reply.ID}}, nested)
	assert.Equal(t, ir.HTTPInboundCallback, reply.Bindings.HTTP[0].Inbound)
	assert.Empty(t, reply.Servers, "the servers of the item declaring it are not its own")
}

// assertPathItemAnchoredKey pins that an undeclared path-item key whose value
// carries a YAML anchor is kept and reported exactly as a plainly-valued one
// beside it is (GitHub #412), rather than reaching the IR in no form and with no
// diagnostic. Each key is asserted with its own marker so an entry recovered
// under the wrong key cannot pass for the right one.
func assertPathItemAnchoredKey(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	operation, ok := operationByName(doc, "getX")
	require.True(t, ok)

	for _, tc := range []struct{ key, marker, at string }{
		{"openapi:pathItem/bogusAnchored", "ANCHORED", "/paths/~1x/bogusAnchored"},
		{"openapi:pathItem/plainBogus", "PLAIN", "/paths/~1x/plainBogus"},
	} {
		entry := unmodeledEntry(t, operation.Unmodeled, tc.key)
		assert.JSONEq(t, `{"responses":{"200":{"description":"`+tc.marker+`"}}}`, string(entry.Value),
			"%s keeps the value the source wrote", tc.key)
		assert.Equal(t, ir.ReasonOutOfScope, entry.Reason,
			"OpenAPI defines no such key on a path item, so no IR node is coming for it")
		assert.Equal(t, jsontext.Pointer(tc.at), entry.Provenance.Pointer)
		assert.Equal(t, []ir.Severity{ir.SeverityWarning},
			diagsAt(diags, "openapi/unknown-object-key", tc.at),
			"%s is announced once, at the key's own pointer, graded as any undeclared key", tc.key)
	}
}

// assertPathItemRefSiblings pins what the fields a path item writes beside its
// $ref lower to, and that the item the $ref names still mounts unchanged.
//
// The operation written beside the reference is a declaration of its own: it
// mounts at the path's own pointers, and the item-level constructs written
// beside it reach every operation mounted at that path — the referent's
// included — which is why the summary is asserted on the referenced item's
// operation as well as on the sibling's (GitHub #577).
func assertPathItemRefSiblings(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	sibling, ok := operationByName(doc, "putA")
	require.True(t, ok, "the operation written beside the $ref reaches the IR")
	assert.Equal(t, ir.OperationID("operation/openapi/paths/~1a/put"), sibling.ID)
	assert.Equal(t, jsontext.Pointer("/paths/~1a/put"), sibling.Provenance.Pointer,
		"a sibling operation is declared where it is mounted, not at the item it references")
	assert.Equal(t, []string{"PUT"}, methodsOf(sibling))

	assert.Equal(t, "written beside the ref", sibling.Bindings.HTTP[0].PathDocs.Summary,
		"the summary the use site declares documents the path")
	for _, tc := range []struct{ key, value, at string }{
		{"openapi:pathItem/x-beside", `"use"`, "/paths/~1a/x-beside"},
		{"openapi:pathItem/bogusBeside", `{"responses":{"200":{"description":"BESIDE"}}}`,
			"/paths/~1a/bogusBeside"},
	} {
		entry := unmodeledEntry(t, sibling.Unmodeled, tc.key)
		assert.JSONEq(t, tc.value, string(entry.Value), "%s keeps what the use site declared", tc.key)
		assert.Equal(t, jsontext.Pointer(tc.at), entry.Provenance.Pointer)
	}
	assertPathItemServersNamed(t, doc, sibling, "https://beside.example")
	assert.Equal(t, []ir.ServerID{"server/openapi/paths/~1a/servers/0"}, sibling.Servers,
		"named where the use site declares them")
	assert.Equal(t, []ir.Severity{ir.SeverityWarning},
		diagsAt(diags, "openapi/unknown-object-key", "/paths/~1a/bogusBeside"),
		"the use site's undeclared key is announced at its own pointer, as any other is")

	require.Len(t, sibling.Params, 1, "the use site's item parameter merges into its operation")
	assert.Equal(t, "beside", sibling.Params[0].Name.Source)
	assert.Equal(t, jsontext.Pointer("/paths/~1a/parameters/0"), sibling.Params[0].Provenance.Pointer)

	// The referenced item's own operation keeps mounting at both paths, and the
	// constructs written beside the reference reach it too.
	atA := operationByID(t, doc, "operation/openapi/paths/~1a/get")
	assert.Equal(t, "getB", atA.Name.Source)
	assert.Equal(t, jsontext.Pointer("/paths/~1b/get"), atA.Provenance.Pointer)
	assert.Equal(t, "written beside the ref", atA.Bindings.HTTP[0].PathDocs.Summary)
	atB := operationByID(t, doc, "operation/openapi/paths/~1b/get")
	assert.Equal(t, jsontext.Pointer("/paths/~1b/get"), atB.Provenance.Pointer)
	assert.Empty(t, atB.Bindings.HTTP[0].PathDocs.Summary,
		"the referent's own path keeps only what it declares")
}

// methodsOf returns the wire methods an operation's HTTP bindings declare.
func methodsOf(operation ir.Operation) []string {
	methods := make([]string, 0, len(operation.Bindings.HTTP))
	for _, b := range operation.Bindings.HTTP {
		methods = append(methods, b.Method)
	}
	return methods
}

// operationByID returns the operation carrying id, which is what tells two
// mounts of one $ref'd declaration apart where a source name cannot.
func operationByID(t *testing.T, doc *ir.Document, id string) ir.Operation {
	t.Helper()
	for _, operationGroup := range doc.Services[0].Groups {
		for _, operation := range operationGroup.Operations {
			if string(operation.ID) == id {
				return operation
			}
		}
	}
	t.Fatalf("operation %s not found", id)
	return ir.Operation{}
}

// assertPathItemDocs pins that a path item's own documentation survives at every
// route that reaches a path item, and that keeping it costs the operation
// nothing it declared for itself.
//
// It is kept rather than merged: ir.Docs holds the operation's summary and
// description, a path item's pair documents the path, and merging the two would
// need a precedence rule and would attach to an operation text its author never
// wrote. So the assertion is that both subjects survive side by side.
func assertPathItemDocs(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	for _, tc := range []struct{ operation, summary, description string }{
		{"listPets", "Pet collection", "Everything addressable at /pets."},
		{"createPet", "Pet collection", "Everything addressable at /pets."},
		{"onPetCreated", "Creation callback", "Delivered once the pet exists."},
		{"onPetDeleted", "Pet deleted", "Delivered when a pet is removed."},
	} {
		operation, ok := operationByName(doc, tc.operation)
		require.True(t, ok, "operation %s", tc.operation)
		assertPathItemDocsKept(t, operation, tc.summary, tc.description)
	}

	listPets, ok := operationByName(doc, "listPets")
	require.True(t, ok)
	assert.Equal(t, "List pets", listPets.Docs.Summary,
		"the operation's own summary is what Docs holds")
	assert.Equal(t, "Returns every pet.", listPets.Docs.Description)

	createPet, ok := operationByName(doc, "createPet")
	require.True(t, ok)
	assert.Empty(t, createPet.Docs.Summary,
		"and an operation that documents nothing has nothing invented for it")

	// The pair reaches a field, so nothing is announced.
	assert.Empty(t, diagsAt(diags, "openapi/degraded-construct", "/paths/~1pets/get"))
	assert.Empty(t, diagsAt(diags, "openapi/degraded-construct", "/webhooks/petDeleted/post"))
}

// assertPathItemDocsKept checks one operation's HTTP binding documents the path
// with the pair its own path item declared, and that nothing of it stayed
// verbatim.
func assertPathItemDocsKept(t *testing.T, operation ir.Operation, summary, description string) {
	t.Helper()
	require.Len(t, operation.Bindings.HTTP, 1)
	assert.Equal(t, ir.Docs{Summary: summary, Description: description}, operation.Bindings.HTTP[0].PathDocs,
		"the path item's pair documents the path on %s", operation.ID)
	assert.NotContains(t, operation.Unmodeled, "openapi:pathItem/summary", "nothing of the pair is kept verbatim")
	assert.NotContains(t, operation.Unmodeled, "openapi:pathItem/description")
}

// assertPathItemOperations pins that every operation a 3.2 path item declares
// becomes an ir.Operation, whichever field declared it and whichever of the
// three routes reaches the path item.
//
// The walk read the fixed method fields and nothing else, so an
// additionalOperations entry was dropped entire — and with it every type
// reachable only through it, which is why the request body's schema is asserted
// to be in the registry rather than merely referenced from an operation.
func assertPathItemOperations(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	for _, tc := range []struct{ operation, method string }{
		{"queryIndex", "QUERY"},
		{"purgeIndex", "PURGE"},
		{"mixedCaseIndex", "mIxEdCase"},
		{"purgeCallback", "PURGE"},
		{"onFlush", "FLUSH"},
	} {
		operation, ok := operationByName(doc, tc.operation)
		require.True(t, ok, "operation %s", tc.operation)
		require.Len(t, operation.Bindings.HTTP, 1)
		assert.Equal(t, tc.method, operation.Bindings.HTTP[0].Method,
			"%s binds the method as the source spelled it", tc.operation)
	}

	onFlush, ok := operationByName(doc, "onFlush")
	require.True(t, ok)
	assert.Equal(t, ir.HTTPInboundWebhook, onFlush.Bindings.HTTP[0].Inbound,
		"a webhook mount marks the binding whichever field declared the operation")
	assert.Equal(t, "cacheFlushed", onFlush.Bindings.HTTP[0].WebhookName,
		"the webhooks-map key names the event")
	assert.Empty(t, onFlush.Bindings.HTTP[0].URITemplate)

	subscribe, ok := operationByName(doc, "subscribeIndex")
	require.True(t, ok)
	require.Len(t, subscribe.Bindings.HTTP, 1)
	require.Len(t, subscribe.Bindings.HTTP[0].Callbacks, 1)
	purgeCallback, ok := operationByName(doc, "purgeCallback")
	require.True(t, ok)
	assert.Equal(t, []ir.OperationID{purgeCallback.ID}, subscribe.Bindings.HTTP[0].Callbacks[0].Operations,
		"an expression declaring only additionalOperations still binds to its parent")

	queryIndex, ok := operationByName(doc, "queryIndex")
	require.True(t, ok)
	assert.NotNil(t, doc.Types[openapitest.BodyTarget(t, queryIndex.Request)],
		"the request body schema is interned, not merely referenced")
}
