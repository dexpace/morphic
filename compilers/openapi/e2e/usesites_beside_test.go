// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// assertRefBesideKeys is the corpus row for the keys a Reference Object writes
// beside its $ref: each rides on the node its use lowers to, under
// openapi:reference/<i>/ where i is 0 for the use and counts the aliases the
// chain passes, and the summary and description a use folds into its docs are
// not kept a second time.
func assertRefBesideKeys(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	t.Helper()
	assert.Equal(t, []string{
		"openapi/unhomed-construct info /paths/~1a/post/callbacks/cb/description",
		"openapi/unhomed-construct info /paths/~1a/post/callbacks/cb/summary",
		"openapi/unknown-object-key warning /components/parameters/Alias/x-alias",
		"openapi/unknown-object-key warning /paths/~1a/post/callbacks/cb/x-cb",
		"openapi/unknown-object-key warning /paths/~1a/post/parameters/0/required",
		"openapi/unknown-object-key warning /paths/~1a/post/parameters/0/x-note",
		"openapi/unknown-object-key warning /paths/~1a/post/requestBody/x-note",
		"openapi/unknown-object-key warning /paths/~1a/post/responses/200/headers",
		"openapi/unknown-object-key warning /paths/~1a/post/responses/200/x-note",
		"openapi/unknown-object-key warning /paths/~1b/get/parameters/0/x-alias",
	}, findings(diags), "the table reports each key; the two unfolded docs of the callback are unhomed")

	a := opTree(t, doc, "a")
	p := namedItem(t, valueAtPath(a, "params"), "p")
	assert.Empty(t, cmp.Diff(map[string]any{
		"openapi:reference/0/x-note":   "kept?",
		"openapi:reference/0/required": true,
	}, keptValues(valueAtPath(p, "unmodeled"))))
	assert.Equal(t, map[string]any{
		"openapi:reference/0/x-note":   "out_of_scope",
		"openapi:reference/0/required": "out_of_scope",
	}, keptReasons(valueAtPath(p, "unmodeled")))
	assert.Equal(t, "/paths/~1a/post/parameters/0/x-note", keptPointers(valueAtPath(p, "unmodeled"))["openapi:reference/0/x-note"])
	assert.Equal(t, "use-summary", valueAtPath(p, "docs", "summary"), "the use's summary is folded into docs")
	assert.NotEqual(t, true, valueAtPath(p, "required"), "a key beside a $ref does not make the parameter required")

	qa := namedItem(t, valueAtPath(a, "params"), "q")
	assert.Empty(t, cmp.Diff(map[string]any{"openapi:reference/1/x-alias": float64(1)}, keptValues(valueAtPath(qa, "unmodeled"))))
	assert.Equal(t, "/components/parameters/Alias/x-alias", keptPointers(valueAtPath(qa, "unmodeled"))["openapi:reference/1/x-alias"])
	assert.Equal(t, "s", valueAtPath(qa, "docs", "summary"))
	qb := namedItem(t, valueAtPath(opTree(t, doc, "b"), "params"), "q")
	assert.Empty(t, cmp.Diff(map[string]any{
		"openapi:reference/0/x-alias": float64(0),
		"openapi:reference/1/x-alias": float64(1),
	}, keptValues(valueAtPath(qb, "unmodeled"))), "the use and the alias each keep their own entry")

	assert.Contains(t, keptValues(valueAtPath(a, "request", "unmodeled")), "openapi:reference/0/x-note")
	resp := valueAtPath(a, "responses", "0", "unmodeled")
	assert.Empty(t, cmp.Diff(map[string]any{
		"openapi:reference/0/x-note": "kept?",
		"openapi:reference/0/headers": map[string]any{
			"X-A": map[string]any{"schema": map[string]any{"type": "string"}},
		},
	}, keptValues(resp)))
	assert.Empty(t, valueAtPath(a, "responses", "0", "headers"), "headers beside a $ref lower nothing")

	whole := tree(t, doc)
	data, err := json.Marshal(whole, json.Deterministic(true))
	require.NoError(t, err)
	assert.Equal(t, 3, strings.Count(string(data), "kept?"), "the parameter, the body and the response each keep it once")
	for key := range keptValues(valueAtPath(whole, "unmodeled")) {
		assert.False(t, strings.HasPrefix(key, "openapi:reference/"), "the document keeps none: %s", key)
	}
}

// TestRefBesideKeys_AreKeptOnTheirUseAndVerify compiles ref-beside-keys
// through a verified run and holds it to assertRefBesideKeys.
func TestRefBesideKeys_AreKeptOnTheirUseAndVerify(t *testing.T) {
	t.Parallel()
	doc, diags := compileNamedVerified(t, "ref-beside-keys.yaml", corpusFile(t, "ref-beside-keys"))
	assertRefBesideKeys(t, doc, diags)
}

// TestBeside_AnAliasInAnotherDocumentKeepsItsKeyThere compiles a use whose
// alias lives in another document: the use's entry is placed in the root
// document and the alias's in the document that writes it.
func TestBeside_AnAliasInAnotherDocumentKeepsItsKeyThere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	layout(t, dir, map[string]string{
		"root.yaml": `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      parameters: [{$ref: 'other.yaml#/components/parameters/Alias', x-use: 1}]
      responses: {"200": {description: ok}}
`,
		"other.yaml": `openapi: 3.1.0
info: {title: O, version: "1"}
paths: {}
components:
  parameters:
    Alias: {$ref: '#/components/parameters/Q', x-alias: 2}
    Q: {name: q, in: query, schema: {type: string}}
`,
	})
	doc, _ := compileVerifiedOnDisk(t, dir, "root.yaml")
	q := namedItem(t, valueAtPath(opTree(t, doc, "a"), "params"), "q")
	un := valueAtPath(q, "unmodeled")
	assert.Empty(t, cmp.Diff([]string{"openapi:reference/0/x-use", "openapi:reference/1/x-alias"}, sortedKeys(keptValues(un))))
	use := valueAtPath(un, "openapi:reference/0/x-use", "provenance")
	alias := valueAtPath(un, "openapi:reference/1/x-alias", "provenance")
	assert.Equal(t, "/paths/~1a/get/parameters/0/x-use", valueAtPath(use, "pointer"))
	assert.Equal(t, "/components/parameters/Alias/x-alias", valueAtPath(alias, "pointer"))
	assert.Equal(t, float64(0), valueAtPath(use, "source"), "source 0 is the root document")
	assert.NotEqual(t, float64(0), valueAtPath(alias, "source"), "the alias's entry names the other document")
	assert.Len(t, doc.Sources, 2)
}

// TestBeside_ASecuritySchemeKeepsTheKeysBesideItsRef holds a scheme written as
// a $ref with an extension beside it to that key on the scheme the use lowers
// to.
func TestBeside_ASecuritySchemeKeepsTheKeysBesideItsRef(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  securitySchemes:
    A: {$ref: '#/components/securitySchemes/B', x-n: 1}
    B: {type: http, scheme: basic}
`
	doc, _ := compileNamedVerified(t, "schemes.yaml", src)
	a := tree(t, doc.Auth["auth/openapi/components/securitySchemes/A"])
	assert.Empty(t, cmp.Diff(map[string]any{"openapi:reference/0/x-n": float64(1)}, keptValues(valueAtPath(a, "unmodeled"))))
}
