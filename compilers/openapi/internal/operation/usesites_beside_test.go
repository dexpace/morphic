// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
)

// TestContentDocs_ChainAndDeclarationKeepApart holds a content entry written as
// a $ref to a media type, with a summary on each side: the use's summary is
// kept under openapi:content/<mt>/reference/0/ as documentation with no home,
// the declaration's under the entry's own scope as a key its object does not
// define, and neither takes the other's place.
func TestContentDocs_ChainAndDeclarationKeepApart(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, specsDir, "content_ref_summary_keys"))
	q := namedItem(t, valueAtPath(opTree(t, doc, "a"), "params"), "q")
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:content/application~1json/reference/0/summary": `no_ir_home "The use." ` +
			`/paths/~1a/get/parameters/0/content/application~1json/summary`,
		"openapi:content/application~1json/summary": `out_of_scope "The declaration." /components/mediaTypes/M/summary`,
	}, entries(t, valueAtPath(q, "unmodeled"))))
	assert.Equal(t, []string{
		"openapi/unhomed-construct info /paths/~1a/get/parameters/0/content/application~1json/summary",
		"openapi/unknown-object-key warning /components/mediaTypes/M/summary",
	}, findings(diags), "no collision finding: the two keys no longer share an entry")
}

// TestContentDocs_AKeyIsReadWhole holds a key to the entry it spells and never
// to a prefix of it: a media type object writing keys named docs, docs/summary
// and, on a parameter object, schema/x-a sits beside the scopes this change
// writes, and each is its own entry.
func TestContentDocs_AKeyIsReadWhole(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, specFrom(t, specsDir, "content_key_read_whole"))
	a := opTree(t, doc, "a")

	const mt = `openapi:content/application~1json; profile="a~1b c"/`
	q := namedItem(t, valueAtPath(a, "params"), "q")
	values := entries(t, valueAtPath(q, "unmodeled"))
	assert.Len(t, values, 6)
	for key, want := range map[string]string{
		mt + `docs`:                `{"summary":"undeclared-docs"}`,
		mt + `docs~1summary`:       `"slash-key"`,
		mt + `reference/0/summary`: `"The use."`,
		mt + `summary`:             `"The declaration."`,
		mt + `x-a`:                 `"MT"`,
		mt + `schema/x-a`:          `"S"`,
	} {
		assert.Contains(t, values[key], want, key)
	}

	p := namedItem(t, valueAtPath(a, "params"), "p")
	pg := entries(t, valueAtPath(p, "unmodeled"))
	assert.Len(t, pg, 2)
	assert.Contains(t, pg["openapi:schema~1x-a"], `"FROM_OBJECT_KEY"`)
	assert.Contains(t, pg["openapi:schema/x-a"], `"FROM_SCHEMA"`)
}

// TestCallbacks_AUseKeepsItsDocs holds a callback written as a $ref with a
// description, a summary and an extension beside it to the operation's HTTP
// binding: the callback has no docs to hold the first two, so they are kept
// whole as documentation with no home, and the extension as an undefined key.
func TestCallbacks_AUseKeepsItsDocs(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, specFrom(t, conformanceSpecsDir, "ref-beside-keys"))
	binding := valueAtPath(opTree(t, doc, "a"), "bindings", "http", "0", "unmodeled")
	assert.Empty(t, cmp.Diff(map[string]string{
		"openapi:callbacks/cb/reference/0/description": `no_ir_home "cbd" /paths/~1a/post/callbacks/cb/description`,
		"openapi:callbacks/cb/reference/0/summary":     `no_ir_home "cbs" /paths/~1a/post/callbacks/cb/summary`,
		"openapi:callbacks/cb/reference/0/x-cb":        `out_of_scope 1 /paths/~1a/post/callbacks/cb/x-cb`,
	}, filterPrefix(entries(t, binding), "openapi:callbacks/cb/reference/")))
	assert.Equal(t, []string{
		"openapi/unhomed-construct info /paths/~1a/post/callbacks/cb/description",
		"openapi/unhomed-construct info /paths/~1a/post/callbacks/cb/summary",
	}, byCode(diags, "openapi/unhomed-construct"))
}

// filterPrefix returns the entries of m whose key starts with prefix.
func filterPrefix(m map[string]string, prefix string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			out[k] = v
		}
	}
	return out
}
