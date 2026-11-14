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

// opWithID returns the operation of doc with the ID, failing the test where
// there is none.
func opWithID(t *testing.T, doc *ir.Document, id ir.OpID) ir.Operation {
	t.Helper()
	i := slices.IndexFunc(allOperations(doc), func(op ir.Operation) bool { return op.ID == id })
	require.GreaterOrEqual(t, i, 0, "no operation %s", id)
	return allOperations(doc)[i]
}

// operationIDs returns the ID of each operation of doc, which says what an
// assertion on their number found where the operations themselves would fill a
// failure with their fields.
func operationIDs(doc *ir.Document) []ir.OpID {
	ops := allOperations(doc)
	ids := make([]ir.OpID, 0, len(ops))
	for _, op := range ops {
		ids = append(ids, op.ID)
	}
	return ids
}

// findingsAt returns each diagnostic of diags as "severity code pointer", sorted,
// so two documents' findings compare as the multisets they are.
func findingsAt(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s", d.Severity, d.Code, d.Provenance.Pointer))
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

		put := opWithID(t, doc, "op/openapi/paths/~1a/put")
		assert.Equal(t, "putShared", put.Name.Source)
		assert.Equal(t, jsontext.Pointer("/x-shared/item/put"), put.Provenance.Pointer, "declared at the landing")
		assert.Equal(t, "getB", opWithID(t, doc, "op/openapi/paths/~1a/get").Name.Source)
		assert.Equal(t, "getB", opWithID(t, doc, "op/openapi/paths/~1b/get").Name.Source)
		assert.Len(t, operationIDs(doc), 3, "itemFirst %t", itemFirst)
		for _, d := range diags {
			assert.NotContains(t, d.Message, "does not reach", "itemFirst %t", itemFirst)
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
				op := opWithID(t, doc, ir.OpID(fmt.Sprintf("op/openapi/paths/~1a/%s", methods[i])))
				assert.Equal(t, fmt.Sprintf("op%d", i), op.Name.Source)
				assert.Equal(t, jsontext.Pointer(fmt.Sprintf("/x-chain/i%d/%s", i, methods[i])), op.Provenance.Pointer)
			}
			assert.Equal(t, "getB", opWithID(t, doc, "op/openapi/paths/~1a/get").Name.Source)
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

		put := opWithID(t, doc, "op/openapi/paths/~1a/put")
		assert.Equal(t, "putOther", put.Name.Source, "second %t", second)
		assert.Equal(t, jsontext.Pointer("/items/I/put"), put.Provenance.Pointer)
		require.Less(t, put.Provenance.Source, len(doc.Sources))
		assert.Equal(t, "other.yaml", filepath.Base(doc.Sources[put.Provenance.Source].Path),
			"declared in the document that writes it")
		get := opWithID(t, doc, "op/openapi/paths/~1a/get")
		assert.Equal(t, "getJ", get.Name.Source)
		assert.Equal(t, jsontext.Pointer("/items/J/get"), get.Provenance.Pointer)
		assert.Len(t, operationIDs(doc), 2)
		for _, d := range diags {
			assert.NotContains(t, d.Message, "another document", "second %t", second)
		}
	}
	assert.Empty(t, cmp.Diff(docs[0], docs[1],
		cmpopts.IgnoreFields(ir.SourceInfo{}, "Hash"), cmpopts.IgnoreFields(ir.Provenance{}, "Position")),
		"the two orders of the items lower to one document")
}
