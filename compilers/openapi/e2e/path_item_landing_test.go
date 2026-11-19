// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// sharedItemSpec writes a path item at /x-shared/item, beside its $ref, which
// /a names, and the /b it refers to; itemFirst writes the x-shared key before
// the paths, and otherwise after them.
func sharedItemSpec(itemFirst bool) string {
	const shared = `x-shared:
  item:
    $ref: '#/paths/~1b'
    put:
      operationId: putShared
      responses: {"200": {description: ok}}
`
	const paths = `paths:
  /a:
    $ref: '#/x-shared/item'
  /b:
    get:
      operationId: getB
      responses: {"200": {description: ok}}
`
	const head = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	if itemFirst {
		return head + shared + paths
	}
	return head + paths + shared
}

// operationWithID returns the operation of doc with the ID, failing the test
// where there is none.
func operationWithID(t *testing.T, doc *ir.Document, id ir.OperationID) ir.Operation {
	t.Helper()
	i := slices.IndexFunc(allOperations(doc), func(operation ir.Operation) bool { return operation.ID == id })
	require.GreaterOrEqual(t, i, 0, "no operation %s", id)
	return allOperations(doc)[i]
}

// operationIDs returns the ID of each operation of doc, which says what an
// assertion on their number found where the operations themselves would fill a
// failure with their fields.
func operationIDs(doc *ir.Document) []ir.OperationID {
	operations := allOperations(doc)
	ids := make([]ir.OperationID, 0, len(operations))
	for _, operation := range operations {
		ids = append(ids, operation.ID)
	}
	return ids
}

// findingsAt returns each diagnostic of diags as "severity code pointer", sorted,
// so two documents' findings compare as the multisets they are.
func findingsAt(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		out = append(out, fmt.Sprintf("%s %s %s", diagnostic.Severity, diagnostic.Code, diagnostic.Provenance.Pointer))
	}
	slices.Sort(out)
	return out
}

// TestCompile_FieldsBesideAPathItemRefAtAnyLandingAreMounted pins that a path
// item a $ref lands on at a position no walk of the document visits, an x-*
// key, is modelled with the fields written beside its own $ref: /a mounts the
// put written there, declared at the landing, and the get of /b it refers to.
// Written before or after the paths, the document and its findings are the same.
func TestCompile_FieldsBesideAPathItemRefAtAnyLandingAreMounted(t *testing.T) {
	t.Parallel()
	docs := map[bool]*ir.Document{}
	findings := map[bool][]string{}
	for _, itemFirst := range []bool{true, false} {
		doc, diags := parseVerified(t, sharedItemSpec(itemFirst))
		docs[itemFirst], findings[itemFirst] = doc, findingsAt(diags)

		put := operationWithID(t, doc, "operation/openapi/paths/~1a/put")
		assert.Equal(t, "putShared", put.Name.Source)
		assert.Equal(t, jsontext.Pointer("/x-shared/item/put"), put.Provenance.Pointer, "declared at the landing")
		assert.Equal(t, "getB", operationWithID(t, doc, "operation/openapi/paths/~1a/get").Name.Source)
		assert.Equal(t, "getB", operationWithID(t, doc, "operation/openapi/paths/~1b/get").Name.Source)
		assert.Len(t, operationIDs(doc), 3, "itemFirst %t", itemFirst)
		for _, diagnostic := range diags {
			assert.NotContains(t, diagnostic.Message, "does not reach", "itemFirst %t", itemFirst)
		}
	}
	assert.Empty(t, cmp.Diff(docs[true], docs[false],
		cmpopts.IgnoreFields(ir.SourceInfo{}, "Hash"), cmpopts.IgnoreFields(ir.Provenance{}, "Position"),
		cmpopts.IgnoreFields(ir.Document{}, "Diagnostics")),
		"the two declaration orders lower to one document")
	assert.Equal(t, findings[true], findings[false], "and draw the same findings")
}

// TestCompile_ALongChainOfLandingsMountsEachOnesFields pins a chain of path
// items at x-* positions, each writing an operation of its own beside its $ref
// and the last naming /b: /a mounts one operation for each landing, each
// declared at its own landing, and /b's get, at every length of the chain.
func TestCompile_ALongChainOfLandingsMountsEachOnesFields(t *testing.T) {
	t.Parallel()
	methods := []string{"put", "post", "delete", "patch", "options", "head", "trace"}
	for _, n := range []int{1, 2, len(methods)} {
		t.Run(fmt.Sprintf("%d landings", n), func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			b.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\nx-chain:\n")
			for i := range n {
				next := fmt.Sprintf("'#/x-chain/i%d'", i+1)
				if i == n-1 {
					next = "'#/paths/~1b'"
				}
				fmt.Fprintf(&b, "  i%d:\n    $ref: %s\n    %s: {operationId: op%d, responses: {\"200\": {description: ok}}}\n",
					i, next, methods[i], i)
			}
			b.WriteString("paths:\n  /a: {$ref: '#/x-chain/i0'}\n" +
				"  /b: {get: {operationId: getB, responses: {\"200\": {description: ok}}}}\n")

			doc, diags := parseVerified(t, b.String())
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Len(t, operationIDs(doc), n+2, "one for each landing, /a's get and /b's")
			for i := range n {
				operation := operationWithID(t, doc, ir.OperationID(fmt.Sprintf("operation/openapi/paths/~1a/%s", methods[i])))
				assert.Equal(t, fmt.Sprintf("op%d", i), operation.Name.Source)
				assert.Equal(t, jsontext.Pointer(fmt.Sprintf("/x-chain/i%d/%s", i, methods[i])), operation.Provenance.Pointer)
			}
			assert.Equal(t, "getB", operationWithID(t, doc, "operation/openapi/paths/~1a/get").Name.Source)
		})
	}
}

// foreignLandings is a root naming a path item in another file, which writes a
// method beside its own $ref to a second item there; second declares the items
// the other way round.
func foreignLandings(second bool) string {
	const i = `  I:
    $ref: '#/items/J'
    put: {operationId: putOther, responses: {"200": {description: ok}}}
`
	const j = `  J:
    get: {operationId: getJ, responses: {"200": {description: ok}}}
`
	if second {
		return "items:\n" + j + i
	}
	return "items:\n" + i + j
}

// TestCompile_FieldsBesideAForeignPathItemRefAreMounted pins the same where the
// landing is in another document: /a mounts that document's put, declared in
// that document at the landing, and the get of the item it names, and nothing
// is reported unread, in either order of the items in the file, which lower to
// one document.
func TestCompile_FieldsBesideAForeignPathItemRefAreMounted(t *testing.T) {
	t.Parallel()
	const root = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a: {$ref: 'other.yaml#/items/I'}\n"
	dir := t.TempDir()
	docs := make([]*ir.Document, 0, 2)
	for _, second := range []bool{false, true} {
		layout(t, dir, map[string]string{"other.yaml": foreignLandings(second)})
		doc, diags := compileVerified(t, compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)},
			openapi.Options{AllowExternalRefs: true})
		require.NotNil(t, doc)
		openapitest.RequireNoErrorDiags(t, diags)
		docs = append(docs, doc)

		put := operationWithID(t, doc, "operation/openapi/paths/~1a/put")
		assert.Equal(t, "putOther", put.Name.Source, "second %t", second)
		assert.Equal(t, jsontext.Pointer("/items/I/put"), put.Provenance.Pointer)
		require.Less(t, put.Provenance.Source, len(doc.Sources))
		assert.Equal(t, "other.yaml", filepath.Base(doc.Sources[put.Provenance.Source].Path),
			"declared in the document that writes it")
		get := operationWithID(t, doc, "operation/openapi/paths/~1a/get")
		assert.Equal(t, "getJ", get.Name.Source)
		assert.Equal(t, jsontext.Pointer("/items/J/get"), get.Provenance.Pointer)
		assert.Len(t, operationIDs(doc), 2)
		for _, diagnostic := range diags {
			assert.NotContains(t, diagnostic.Message, "another document", "second %t", second)
		}
	}
	assert.Empty(t, cmp.Diff(docs[0], docs[1],
		cmpopts.IgnoreFields(ir.SourceInfo{}, "Hash"), cmpopts.IgnoreFields(ir.Provenance{}, "Position")),
		"the two orders of the items lower to one document")
}

// repeatedLists is a document whose path items under an x- key list a
// parameter twice: one beside its $ref, one with no $ref, mounted by two
// paths, an operation of it, and a callback's path item of that operation,
// which a path mounts as well; a path item under paths repeats one beside its
// $ref too.
const repeatedLists = `openapi: 3.1.0
info: {title: T, version: "1"}
x-shared:
  item:
    $ref: '#/paths/~1b'
    parameters:
      - {name: d, in: query, schema: {type: string}}
      - {name: d, in: query, schema: {type: string}}
  plain:
    parameters:
      - {name: e, in: query, schema: {type: string}}
      - {name: e, in: query, schema: {type: string}}
    put:
      operationId: putPlain
      parameters:
        - {name: f, in: query, schema: {type: string}}
        - {name: f, in: query, schema: {type: string}}
      callbacks:
        cb:
          next:
            parameters:
              - {name: g, in: query, schema: {type: string}}
              - {name: g, in: query, schema: {type: string}}
      responses: {"200": {description: ok}}
paths:
  /direct:
    $ref: '#/paths/~1b'
    parameters:
      - {name: d, in: query, schema: {type: string}}
      - {name: d, in: query, schema: {type: string}}
  /a: {$ref: '#/x-shared/item'}
  /w: {$ref: '#/x-shared/plain'}
  /v: {$ref: '#/x-shared/plain'}
  /n: {$ref: '#/x-shared/plain/put/callbacks/cb/next'}
  /b: {get: {operationId: getB, responses: {"200": {description: ok}}}}
`

// TestCompile_AListAtAPathItemLandingIsCheckedForRepeats pins the repeat check
// over each path item a reference lands on in the source where its grammar
// reads none: the list beside its $ref, its own, an operation's and that of a
// landing inside another, each reported once where it is written, as the same
// list under paths is, however many paths mount it.
func TestCompile_AListAtAPathItemLandingIsCheckedForRepeats(t *testing.T) {
	t.Parallel()
	_, diags := parseVerified(t, repeatedLists)

	for _, pointer := range []string{"/paths/~1direct/parameters/1", "/x-shared/item/parameters/1",
		"/x-shared/plain/parameters/1", "/x-shared/plain/put/parameters/1",
		"/x-shared/plain/put/callbacks/cb/next/parameters/1"} {
		assert.Equal(t, 1, countCode(diags, diag.DuplicateParameter, pointer), pointer)
	}
	var repeats int
	for _, diagnostic := range diags {
		if diagnostic.Code == diag.DuplicateParameter {
			repeats++
		}
	}
	assert.Equal(t, 5, repeats, "no list is reported twice")
}

// TestCompile_AListAtAPathItemLandingInAnotherDocumentIsCheckedForRepeats pins
// the same in another document: the list beside a $ref there and the list of
// the item it names, which two more paths mount, are each reported once, in
// that document.
func TestCompile_AListAtAPathItemLandingInAnotherDocumentIsCheckedForRepeats(t *testing.T) {
	t.Parallel()
	const root = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /a: {$ref: 'other.yaml#/items/I'}\n  /b: {$ref: 'other.yaml#/items/J'}\n  /c: {$ref: 'other.yaml#/items/J'}\n"
	const other = `items:
  I:
    $ref: '#/items/J'
    parameters:
      - {name: d, in: query, schema: {type: string}}
      - {name: d, in: query, schema: {type: string}}
  J:
    parameters:
      - {name: e, in: query, schema: {type: string}}
      - {name: e, in: query, schema: {type: string}}
    get: {operationId: getJ, responses: {"200": {description: ok}}}
`
	dir := t.TempDir()
	layout(t, dir, map[string]string{"other.yaml": other})
	doc, diags := compileVerified(t, compilers.Source{Path: filepath.Join(dir, "root.yaml"), Data: []byte(root)},
		openapi.Options{AllowExternalRefs: true})
	require.NotNil(t, doc)

	var at []string
	for _, diagnostic := range diags {
		if diagnostic.Code == diag.DuplicateParameter {
			require.Less(t, diagnostic.Provenance.Source, len(doc.Sources))
			at = append(at, filepath.Base(doc.Sources[diagnostic.Provenance.Source].Path)+"#"+string(diagnostic.Provenance.Pointer))
		}
	}
	slices.Sort(at)
	assert.Equal(t, []string{"other.yaml#/items/I/parameters/1", "other.yaml#/items/J/parameters/1"}, at)
}

// TestCompile_AServersListAtAPathItemLandingIsCheckedForRepeats pins the same
// check over a servers list of a path item under an x- key: a repeated name is
// reported once, at the repeat's name, as one under paths is.
func TestCompile_AServersListAtAPathItemLandingIsCheckedForRepeats(t *testing.T) {
	t.Parallel()
	_, diags := parseVerified(t, `openapi: 3.2.0
info: {title: T, version: "1"}
x-shared:
  plain:
    servers: [{url: 'https://a.example', name: s}, {url: 'https://b.example', name: s}]
    get: {operationId: getPlain, responses: {"200": {description: ok}}}
paths:
  /w: {$ref: '#/x-shared/plain'}
  /v: {$ref: '#/x-shared/plain'}
`)
	assert.Equal(t, []string{"warning openapi/duplicate-server-name /x-shared/plain/servers/1/name"},
		findingsAt(slices.DeleteFunc(diags, func(diagnostic ir.Diagnostic) bool { return diagnostic.Code != diag.DuplicateServerName })))
}
