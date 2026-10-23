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
	op, ok := opByName(doc, "onNewPet")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	assert.True(t, op.Bindings.HTTP[0].IsWebhook, "webhook operation carries IsWebhook")
	assert.Equal(t, "newPet", op.Bindings.HTTP[0].WebhookName, "the webhooks-map key names the event")
	assert.Empty(t, op.Bindings.HTTP[0].URITemplate, "a webhook carries no URI template")
	assertPathItemServersKept(t, op, "https://hooks.example.com")
	assertOwnServersKeptBesideThem(t, op, "https://hooks-override.example.com")
	assertWebhookGroupIsAHint(t, doc)

	// Two keys a URI template would misstate: braces that belong to the event's
	// name, and the empty key, which names nothing. Neither declares an
	// operationId, so the hint is built from the method and the key.
	ops := allOperations(doc)
	for _, hook := range []struct {
		id        ir.OpID
		key, hint string
	}{
		{"op/openapi/webhooks/{tenant}.created/post", "{tenant}.created", "post_tenant_created"},
		{"op/openapi/webhooks//post", "", "post"},
	} {
		i := slices.IndexFunc(ops, func(op ir.Operation) bool { return op.ID == hook.id })
		require.GreaterOrEqual(t, i, 0, "the webhook keyed %q is mounted", hook.key)
		require.Len(t, ops[i].Bindings.HTTP, 1)
		b := ops[i].Bindings.HTTP[0]
		assert.True(t, b.IsWebhook)
		assert.Equal(t, hook.key, b.WebhookName, "the key, verbatim")
		assert.Empty(t, b.URITemplate, "a webhook has no URI template")
		assert.Equal(t, hook.hint, ops[i].Name.Hint)
	}
}

// assertOwnServersKeptBesideThem pins the overriding half of the servers pair in
// the corpus, so the two-order oracle and the JSON round-trip see it. OpenAPI
// says an operation's own servers override its path item's, so keeping only the
// path item's recorded the superseded list and dropped the effective one.
//
// The two keys are asserted together because the hazard is that they collapse
// into one: a single key holding both would leave the surviving list depending
// on which lowering ran last, which only a two-order diff can see.
func assertOwnServersKeptBesideThem(t *testing.T, op ir.Operation, url string) {
	t.Helper()
	entry, ok := op.Unmodeled["openapi:operationServers"]
	require.True(t, ok, "the operation's own servers are kept beside its path item's")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `[{"url":"`+url+`"}]`, string(entry.Value))
	assert.NotEqual(t, entry.Value, op.Unmodeled["openapi:servers"].Value,
		"and the two keys hold different declarations, not one overwriting the other")
}

// assertPathItemServersKept pins that a path item's servers survive whichever
// parent the path item hangs from. The preserve-plus-diagnostic path used to be
// reached only from the `paths` walk, so the same override written on a webhook
// or a callback disappeared with nothing said (GitHub #39).
func assertPathItemServersKept(t *testing.T, op ir.Operation, url string) {
	t.Helper()
	entry, ok := op.Unmodeled["openapi:servers"]
	require.True(t, ok, "the path item's servers are kept on the operation")
	assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
	assert.JSONEq(t, `[{"url":"`+url+`"}]`, string(entry.Value))
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
	for _, g := range doc.Services[0].Groups {
		if g.Name.Hint != "webhooks" {
			continue
		}
		assert.Empty(t, g.Name.Source, "a group no document declares carries no source spelling")
		assert.Empty(t, g.Name.Canonical, "and no words derived from one")
		return
	}
	t.Fatal("no group carries the webhooks hint")
}

func assertCallbacks(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "subscribe")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].Callbacks, 1)
	assert.Equal(t, "{$request.body#/callbackUrl}", op.Bindings.HTTP[0].Callbacks[0].Expression)
	assert.NotEmpty(t, op.Bindings.HTTP[0].Callbacks[0].Operations)
	cb, ok := opByName(doc, "onEvent")
	require.True(t, ok, "the callback operation is registered alongside its parent")
	assertPathItemServersKept(t, cb, "https://callbacks.example.com")
	assert.NotContains(t, op.Unmodeled, "openapi:servers",
		"the callback's own servers stay on the callback, not on the parent")
}

// assertPathItemAnchoredKey pins that an undeclared path-item key whose value
// carries a YAML anchor is kept and reported exactly as a plainly-valued one
// beside it is (GitHub #412), rather than reaching the IR in no form and with no
// diagnostic. Each key is asserted with its own marker so an entry recovered
// under the wrong key cannot pass for the right one.
func assertPathItemAnchoredKey(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	op, ok := opByName(doc, "getX")
	require.True(t, ok)

	for _, tc := range []struct{ key, marker, at string }{
		{"openapi:pathItem/bogusAnchored", "ANCHORED", "/paths/~1x/bogusAnchored"},
		{"openapi:pathItem/plainBogus", "PLAIN", "/paths/~1x/plainBogus"},
	} {
		entry := unmodeledEntry(t, op.Unmodeled, tc.key)
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
	t.Helper()
	sibling, ok := opByName(doc, "putA")
	require.True(t, ok, "the operation written beside the $ref reaches the IR")
	assert.Equal(t, ir.OpID("op/openapi/paths/~1a/put"), sibling.ID)
	assert.Equal(t, jsontext.Pointer("/paths/~1a/put"), sibling.Provenance.Pointer,
		"a sibling operation is declared where it is mounted, not at the item it references")
	assert.Equal(t, []string{"PUT"}, methodsOf(sibling))

	for _, tc := range []struct{ key, value, at string }{
		{"openapi:pathItemSummary", `"written beside the ref"`, "/paths/~1a/summary"},
		{"openapi:servers", `[{"url":"https://beside.example"}]`, "/paths/~1a/servers"},
		{"openapi:pathItem/x-beside", `"use"`, "/paths/~1a/x-beside"},
		{"openapi:pathItem/bogusBeside", `{"responses":{"200":{"description":"BESIDE"}}}`,
			"/paths/~1a/bogusBeside"},
	} {
		entry := unmodeledEntry(t, sibling.Unmodeled, tc.key)
		assert.JSONEq(t, tc.value, string(entry.Value), "%s keeps what the use site declared", tc.key)
		assert.Equal(t, jsontext.Pointer(tc.at), entry.Provenance.Pointer)
	}
	assert.Equal(t, []ir.Severity{ir.SeverityWarning},
		diagsAt(diags, "openapi/unknown-object-key", "/paths/~1a/bogusBeside"),
		"the use site's undeclared key is announced at its own pointer, as any other is")

	require.Len(t, sibling.Params, 1, "the use site's item parameter merges into its operation")
	assert.Equal(t, "beside", sibling.Params[0].Name.Source)
	assert.Equal(t, jsontext.Pointer("/paths/~1a/parameters/0"), sibling.Params[0].Provenance.Pointer)

	// The referenced item's own operation keeps mounting at both paths, and the
	// constructs written beside the reference reach it too.
	atA := opByID(t, doc, "op/openapi/paths/~1a/get")
	assert.Equal(t, "getB", atA.Name.Source)
	assert.Equal(t, jsontext.Pointer("/paths/~1b/get"), atA.Provenance.Pointer)
	assert.Contains(t, atA.Unmodeled, "openapi:pathItemSummary")
	atB := opByID(t, doc, "op/openapi/paths/~1b/get")
	assert.Equal(t, jsontext.Pointer("/paths/~1b/get"), atB.Provenance.Pointer)
	assert.NotContains(t, atB.Unmodeled, "openapi:pathItemSummary",
		"the referent's own path keeps only what it declares")
}

// methodsOf returns the wire methods an operation's HTTP bindings declare.
func methodsOf(op ir.Operation) []string {
	methods := make([]string, 0, len(op.Bindings.HTTP))
	for _, b := range op.Bindings.HTTP {
		methods = append(methods, b.Method)
	}
	return methods
}

// opByID returns the operation carrying id, which is what tells two mounts of
// one $ref'd declaration apart where a source name cannot.
func opByID(t *testing.T, doc *ir.Document, id string) ir.Operation {
	t.Helper()
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if string(op.ID) == id {
				return op
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
	for _, tc := range []struct{ op, summary, description string }{
		{"listPets", "Pet collection", "Everything addressable at /pets."},
		{"createPet", "Pet collection", "Everything addressable at /pets."},
		{"onPetCreated", "Creation callback", "Delivered once the pet exists."},
		{"onPetDeleted", "Pet deleted", "Delivered when a pet is removed."},
	} {
		op, ok := opByName(doc, tc.op)
		require.True(t, ok, "operation %s", tc.op)
		assertPathItemDocsKept(t, op, tc.summary, tc.description)
	}

	listPets, ok := opByName(doc, "listPets")
	require.True(t, ok)
	assert.Equal(t, "List pets", listPets.Docs.Summary,
		"the operation's own summary is what Docs holds")
	assert.Equal(t, "Returns every pet.", listPets.Docs.Description)

	createPet, ok := opByName(doc, "createPet")
	require.True(t, ok)
	assert.Empty(t, createPet.Docs.Summary,
		"and an operation that documents nothing has nothing invented for it")

	// And the decision is announced at the operation that carries the entry,
	// once, rather than being taken silently.
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/degraded-construct", "/paths/~1pets/get"))
	assert.Equal(t, []ir.Severity{ir.SeverityInfo},
		diagsAt(diags, "openapi/degraded-construct", "/webhooks/petDeleted/post"))
}

// assertPathItemDocsKept checks one operation kept the pair its own path item
// declared, under the keys that name which object the text came from.
func assertPathItemDocsKept(t *testing.T, op ir.Operation, summary, description string) {
	t.Helper()
	kept, ok := op.Unmodeled["openapi:pathItemSummary"]
	require.True(t, ok, "the path item's summary is kept on %s", op.ID)
	assert.Equal(t, ir.ReasonNoIRHome, kept.Reason)
	assert.JSONEq(t, `"`+summary+`"`, string(kept.Value))

	kept, ok = op.Unmodeled["openapi:pathItemDescription"]
	require.True(t, ok, "the path item's description is kept on %s", op.ID)
	assert.Equal(t, ir.ReasonNoIRHome, kept.Reason)
	assert.JSONEq(t, `"`+description+`"`, string(kept.Value))
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
	for _, tc := range []struct{ op, method string }{
		{"queryIndex", "QUERY"},
		{"purgeIndex", "PURGE"},
		{"mixedCaseIndex", "mIxEdCase"},
		{"purgeCallback", "PURGE"},
		{"onFlush", "FLUSH"},
	} {
		op, ok := opByName(doc, tc.op)
		require.True(t, ok, "operation %s", tc.op)
		require.Len(t, op.Bindings.HTTP, 1)
		assert.Equal(t, tc.method, op.Bindings.HTTP[0].Method,
			"%s binds the method as the source spelled it", tc.op)
	}

	onFlush, ok := opByName(doc, "onFlush")
	require.True(t, ok)
	assert.True(t, onFlush.Bindings.HTTP[0].IsWebhook,
		"a webhook mount marks the binding whichever field declared the operation")
	assert.Equal(t, "cacheFlushed", onFlush.Bindings.HTTP[0].WebhookName,
		"the webhooks-map key names the event")
	assert.Empty(t, onFlush.Bindings.HTTP[0].URITemplate)

	subscribe, ok := opByName(doc, "subscribeIndex")
	require.True(t, ok)
	require.Len(t, subscribe.Bindings.HTTP, 1)
	require.Len(t, subscribe.Bindings.HTTP[0].Callbacks, 1)
	purgeCallback, ok := opByName(doc, "purgeCallback")
	require.True(t, ok)
	assert.Equal(t, []ir.OpID{purgeCallback.ID}, subscribe.Bindings.HTTP[0].Callbacks[0].Operations,
		"an expression declaring only additionalOperations still binds to its parent")

	queryIndex, ok := opByName(doc, "queryIndex")
	require.True(t, ok)
	assert.NotNil(t, doc.Types[openapitest.BodyTarget(t, queryIndex.Request)],
		"the request body schema is interned, not merely referenced")
}
