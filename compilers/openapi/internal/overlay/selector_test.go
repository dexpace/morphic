// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// selectorSpec holds an operation of each kind a filter tells apart, under
// path keys that need bracket quoting, and members named as RFC 9535's
// functions are.
const selectorSpec = `paths:
  /blob/{id}:
    put: {operationId: putBlob, tags: [a, b], parameters: [{name: id, in: path}]}
    get: {operationId: getBlob, deprecated: true}
  /other:
    get: {operationId: other, tags: [a]}
components:
  schemas:
    count: {type: integer}
    value: {type: string}
`

// TestReadSelector_ReadsEveryTargetAsRFC9535 pins the dialect a selector is
// read in: RFC 9535, which both versions of the specification name, whatever
// the version, and whatever an extension of the overlay says, so a filter
// written as RFC 9535 writes it selects in an Overlay 1.0 document too.
func TestReadSelector_ReadsEveryTargetAsRFC9535(t *testing.T) {
	t.Parallel()
	const action = "actions:\n  - target: '$.paths.*[?@.operationId==\"putBlob\"]'\n    update: {description: Updated}\n"
	for name, head := range map[string]string{
		"1.0":                     "overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\n",
		"1.1":                     "overlay: 1.1.0\ninfo: {title: O, version: \"1\"}\n",
		"1.0 naming another one":  "overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\nx-speakeasy-jsonpath: legacy\n",
		"1.1 naming another one":  "overlay: 1.1.0\ninfo: {title: O, version: \"1\"}\nx-speakeasy-jsonpath: legacy\n",
		"1.0 naming RFC 9535 too": "overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\nx-speakeasy-jsonpath: rfc9535\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, root, diags := applied(t, selectorSpec, head+action, false)

			require.Empty(t, diags)
			_, description := member(t, root, "paths", "/blob/{id}", "put", "description")
			assert.Equal(t, "Updated", description.Value)
		})
	}
}

// TestReadSelector_SelectsWithEachKindOfSelector pins a target of each kind
// RFC 9535 defines, as an overlay writes them: the update lands on what each
// selects and nowhere else.
func TestReadSelector_SelectsWithEachKindOfSelector(t *testing.T) {
	t.Parallel()
	put, get, other := []string{"paths", "/blob/{id}", "put"}, []string{"paths", "/blob/{id}", "get"}, []string{"paths", "/other", "get"}
	tests := map[string]struct {
		target string
		hits   [][]string
	}{
		"bracketed names":            {"$.paths['/blob/{id}'].put", [][]string{put}},
		"a union of names":           {`$.paths['/blob/{id}', "/other"].get`, [][]string{get, other}},
		"a wildcard":                 {"$.paths.*.get", [][]string{get, other}},
		"a comparison":               {"$.paths.*[?@.operationId == 'other']", [][]string{other}},
		"a parenthesized one":        {"$.paths.*[?(@.operationId == 'other')]", [][]string{other}},
		"an existence test":          {"$.paths.*[?@.deprecated]", [][]string{get}},
		"a negated test":             {"$.paths['/blob/{id}'][?!@.deprecated]", [][]string{put}},
		"a conjunction":              {"$.paths.*[?@.tags && @.operationId != 'other']", [][]string{put}},
		"a nested filter":            {"$.paths.*[?@.tags[?@ == 'b']]", [][]string{put}},
		"a descendant segment":       {"$..[?@.operationId == 'getBlob']", [][]string{get}},
		"length()":                   {"$.paths.*[?length(@.tags) == 2]", [][]string{put}},
		"count()":                    {"$.paths.*[?count(@.tags[*]) == 1]", [][]string{other}},
		"value()":                    {"$.paths.*[?value(@.parameters[0].in) == 'path']", [][]string{put}},
		"an index":                   {"$.paths['/blob/{id}'].put.parameters[0]", [][]string{append(put, "parameters", "0")}},
		"a slice":                    {"$.paths['/blob/{id}'].put.parameters[0:1]", [][]string{append(put, "parameters", "0")}},
		"a negative index":           {"$.paths['/blob/{id}'].put.parameters[-1]", [][]string{append(put, "parameters", "0")}},
		"blank space":                {"$ .paths[ '/other' ] .get", [][]string{other}},
		"members named as functions": {"$.components.schemas.count", [][]string{{"components", "schemas", "count"}}},
		"a union of function names":  {"$.components.schemas['count','value']", [][]string{{"components", "schemas", "count"}, {"components", "schemas", "value"}}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ov := v10 + "  - target: \"" + escapeQuotes(tc.target) + "\"\n    update: {x-hit: true}\n"
			_, root, diags := applied(t, selectorSpec, ov, false)

			require.Empty(t, diags)
			for _, at := range tc.hits {
				hit := nodeAt(t, root, append(at, "x-hit")...)
				assert.Equal(t, "true", hit.Value, "%v", at)
			}
			assert.Equal(t, len(tc.hits), hits(root), "and nowhere else")
		})
	}
}

// nodeAt returns the node at path, a mapping's key or a sequence's index at
// each step, from the document root.
func nodeAt(t *testing.T, root *yaml.Node, path ...string) *yaml.Node {
	t.Helper()
	n := root.Content[0]
	for _, token := range path {
		next := (*yaml.Node)(nil)
		if n.Kind == yaml.SequenceNode {
			i, err := strconv.Atoi(token)
			require.NoError(t, err)
			require.Less(t, i, len(n.Content))
			next = n.Content[i]
		}
		for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == token {
				next = n.Content[i+1]
			}
		}
		require.NotNil(t, next, "no %q under the path %v", token, path)
		n = next
	}
	return n
}

// escapeQuotes writes s for a double-quoted YAML scalar.
func escapeQuotes(s string) string {
	out := make([]byte, 0, len(s))
	for i := range len(s) {
		if s[i] == '"' || s[i] == '\\' {
			out = append(out, '\\')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// TestReadSelector_RefusesWhatItCannotReadAsRFC9535Does pins each selector
// the compiler refuses, and how: one RFC 9535 refuses is an invalid overlay,
// and one RFC 9535 reads that the compiler's JSONPath implementation refuses
// or misreads cannot be applied. Either refuses before any action applies,
// naming the selector.
func TestReadSelector_RefusesWhatItCannotReadAsRFC9535Does(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ target, code, says string }{
		"no query at all":            {"paths", diag.OverlayInvalid, "does not parse"},
		"an unclosed bracket":        {"$.paths[", diag.OverlayInvalid, "does not parse"},
		"the property-name operator": {"$.paths.*~", diag.OverlayInvalid, "~, which RFC 9535 does not define"},
		"a raw control character":    {"$['a\tb']", diag.OverlayInvalid, "control character"},
		"a member named true":        {"$.paths.true", diag.OverlayFailed, `"true"`},
		"a member named null":        {"$..null", diag.OverlayFailed, `"null"`},
		"a \\u escape":               {`$['caf\` + `u00e9']`, diag.OverlayFailed, `\u escape`},
		"a \\r escape":               {`$['a\rb']`, diag.OverlayFailed, `\r`},
		"match()":                    {"$.paths.*[?match(@.operationId, 'put.*')]", diag.OverlayFailed, "match()"},
		"search()":                   {"$.paths.*[?search(@.operationId, 'Blob')]", diag.OverlayFailed, "search()"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, lax := range []bool{false, true} {
				ov := v11 + "  - target: $.paths\n    update: {x-first: 1}\n" +
					"  - target: \"" + escapeQuotes(tc.target) + "\"\n    update: {x-hit: true}\n"
				origin, root, diags := applied(t, selectorSpec, ov, lax)

				assert.False(t, origin.Applied())
				sameCodes(t, []string{"error " + tc.code}, diags)
				assert.Contains(t, diags[0].Message, tc.says)
				assert.Contains(t, diags[0].Message, "(update "+strconv.Quote(tc.target)+"): its target ")
				sameValue(t, selectorSpec, root)
			}
		})
	}
}

// TestReadSelector_ReadsACopySelectorAsATargetIsRead pins the copy field to
// the target's reading: it is an RFC 9535 query too.
func TestReadSelector_ReadsACopySelectorAsATargetIsRead(t *testing.T) {
	t.Parallel()
	_, _, diags := applied(t, selectorSpec, v11+"  - target: $.paths\n    copy: $.paths.*~\n", false)

	sameCodes(t, []string{"error " + diag.OverlayInvalid}, diags)
	assert.Contains(t, diags[0].Message, "its copy ")
}

// hits counts the x-hit members in the tree under root.
func hits(root *yaml.Node) int {
	count := 0
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "x-hit" {
				count++
			}
		}
		stack = append(stack, n.Content...)
	}
	return count
}
