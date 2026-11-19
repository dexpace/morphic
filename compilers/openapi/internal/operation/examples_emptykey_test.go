// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
)

// TestExamples_AnEmptyKeyAndAWrittenNameAreKeptApart holds an examples entry
// keyed by the empty string whose Example Object writes an undeclared name to
// keeping both, the key under its own entry and the name under the object's;
// an entry with a key whose object writes an empty name keeps the name alone,
// so the two read apart by key and not only by reason.
func TestExamples_AnEmptyKeyAndAWrittenNameAreKeptApart(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    get:
      operationId: a
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: integer}
              examples: {"": {value: 1, name: x}, named: {value: 2, name: ""}}
`)
	const entry = "/paths/~1a/get/responses/200/content/application~1json/examples/"
	assert.Equal(t, []string{"openapi/degraded-construct info " + entry}, byCode(diags, "openapi/degraded-construct"))
	examples := valueAtPath(opTree(t, doc, "a"), "responses", "0", "payload", "contents", "0", "examples")
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:examples/key": `degraded_lowering "" ` + entry,
		"openapi:name":         `out_of_scope "x" ` + entry + `/name`,
	}, entries(t, valueAtPath(examples, "0", "unmodeled"))))
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:name": `out_of_scope "" ` + entry + `named/name`,
	}, entries(t, valueAtPath(examples, "1", "unmodeled"))))
}

// TestExamples_ANamedEntryKeepsNoNameKey holds an examples entry with a name
// to keeping no entry for its key: only the empty key, which Example.Name
// cannot tell from none, is kept.
func TestExamples_ANamedEntryKeepsNoNameKey(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths:
  /a:
    get:
      operationId: a
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {type: integer}
              examples: {one: {value: 1}}
`)
	assert.Empty(t, diags)
	got := valueAtPath(opTree(t, doc, "a"), "responses", "0", "payload", "contents", "0", "examples")
	assert.JSONEq(t, `[{"name":"one","value":{"kind":"number","number":"1"}}]`, toJSON(t, got))
}
