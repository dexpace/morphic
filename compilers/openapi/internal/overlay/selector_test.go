// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay_test

import (
	"fmt"
	"slices"
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

// TestReadSelector_RefusesWhatRFC9535DoesNotRead pins each selector RFC 9535
// refuses: an invalid overlay, before any action applies, naming the selector
// and the offset the reader stops at, and the spelling to write instead where
// the compiler can say one, on one line.
func TestReadSelector_RefusesWhatRFC9535DoesNotRead(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		target, says string
		offset       int
	}{
		"no query at all":            {"paths", "", 0},
		"an unclosed bracket":        {"$.paths[", "", 8},
		"the property-name operator": {"$.paths.*~", "~", 9},
		"a raw control character":    {"$['a\tb']", "control character", 4},
		"a member name with a dash":  {"$.paths.*.get.x-internal", "write it ['x-internal']", 15},
		"a member name with a digit": {"$.paths.*.*.responses.200", "write it ['200']", 22},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, lax := range []bool{false, true} {
				ov := v11 + "  - target: $.paths\n    update: {x-first: 1}\n" +
					"  - target: \"" + escapeQuotes(tc.target) + "\"\n    update: {x-hit: true}\n"
				origin, root, diags := applied(t, selectorSpec, ov, lax)

				assert.False(t, origin.Applied())
				sameCodes(t, []string{"error " + diag.OverlayInvalid}, diags)
				assert.Contains(t, diags[0].Message, fmt.Sprintf("does not read as an RFC 9535 query at offset %d: ", tc.offset))
				assert.Contains(t, diags[0].Message, tc.says)
				assert.Contains(t, diags[0].Message, "(update "+strconv.Quote(tc.target)+"): its target ")
				assert.NotContains(t, diags[0].Message, "^", "the parser's caret line is not carried")
				sameValue(t, selectorSpec, root)
			}
		})
	}
}

// TestReadSelector_SelectsWhatTheCompilerOnceRefused pins the targets that were
// refused as unsupported and are queries like any other: a member named for a
// literal, an escape in a name, and the functions match and search, whose
// selections are the operations their patterns match. A pattern that is no
// I-Regexp matches nothing, and says so.
func TestReadSelector_SelectsWhatTheCompilerOnceRefused(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		target string
		hits   int
		dead   bool
	}{
		"a member named true":  {"$.paths.true", 0, false},
		"a member named null":  {"$..null", 0, false},
		"a \\u escape":         {`$['caf\` + `u00e9']`, 0, false},
		"a \\r escape":         {`$['a\rb']`, 0, false},
		"match()":              {"$.paths.*[?match(@.operationId, 'put.*')]", 1, false},
		"match() is anchored":  {"$.paths.*[?match(@.operationId, 'Blob')]", 0, false},
		"search()":             {"$.paths.*[?search(@.operationId, 'Blob')]", 2, false},
		"a failing pattern":    {"$.paths.*[?search(@.operationId, '[')]", 0, true},
		"a function's absence": {"$.paths.*[?!match(@.operationId, 'put.*')]", 2, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ov := v11 + "  - target: \"" + escapeQuotes(tc.target) + "\"\n    update: {x-hit: true}\n"
			origin, root, diags := applied(t, selectorSpec, ov, true)

			assert.True(t, origin.Applied(), "%+v", diags)
			assert.Equal(t, tc.hits, hits(root))
			if tc.dead {
				sameCodes(t, []string{"warning " + diag.OverlayAction, "warning " + diag.OverlayAction}, diags)
				return
			}
			if tc.hits == 0 {
				sameCodes(t, []string{"warning " + diag.OverlayAction}, diags)
				return
			}
			sameCodes(t, nil, diags)
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

// TestReadSelector_WarnsOfAPatternThatMatchesNoString pins the warning for a
// pattern written to match no string: `$` before a character, which RFC
// 9485's mapping to Go's engine leaves an anchor, and `\$`, which I-Regexp has
// no escape for. Each is an openapi/overlay-action warning naming the class
// that writes the character, and the action then selects nothing, which lax
// application warns of too. A pattern that writes the class draws neither.
func TestReadSelector_WarnsOfAPatternThatMatchesNoString(t *testing.T) {
	t.Parallel()
	const src = head + "x-r: [\"$ref\", \"a$refb\"]\n"
	for pattern, want := range map[string]string{
		"$ref":   "write [$] to match the character",
		`\\$ref`: "no I-Regexp",
		"[$]ref": "",
	} {
		ov := v11 + "  - target: \"$['x-r'][?search(@, '" + escapeQuotes(pattern) + "')]\"\n    update: hit\n"
		origin, _, diags := applied(t, src, ov, true)

		require.True(t, origin.Applied(), "%+v", diags)
		if want == "" {
			assert.NotContains(t, codes(diags), "warning "+diag.OverlayAction, pattern)
			continue
		}
		sameCodes(t, []string{"warning " + diag.OverlayAction, "warning " + diag.OverlayAction}, diags)
		assert.Contains(t, diags[0].Message, want, pattern)
		assert.Contains(t, diags[1].Message, "selects nothing", pattern)
	}
}

// TestReadSelector_NotesAPatternWhoseAnchorIsReadAsOne pins the info a
// pattern writing `^` or `$` outside a class draws where it may still match,
// at the action, naming the class that writes the character: `search(@, '$')`
// selects every string. A pattern holding neither outside a class or an
// escape draws none, and one matching no string draws its warning alone.
func TestReadSelector_NotesAPatternWhoseAnchorIsReadAsOne(t *testing.T) {
	t.Parallel()
	const src = head + "x-r: [\"$ref\", \"a$refb\"]\n"
	for pattern, want := range map[string][]string{
		"$":      {"info " + diag.OverlayAnchoredPattern},
		"^a":     {"info " + diag.OverlayAnchoredPattern},
		"[$]ref": nil,
		"ref":    nil,
		"$ref":   {"warning " + diag.OverlayAction, "warning " + diag.OverlayAction},
	} {
		ov := v11 + "  - target: \"$['x-r'][?search(@, '" + escapeQuotes(pattern) + "')]\"\n    update: hit\n"
		_, _, diags := applied(t, src, ov, true)

		got := slices.DeleteFunc(codes(diags), func(c string) bool { return c == "info "+diag.OverlayReplacedValue })
		assert.ElementsMatch(t, want, got, "%s: %+v", pattern, diags)
		for _, d := range diags {
			if d.Code == diag.OverlayAnchoredPattern {
				assert.Equal(t, "/actions/0", string(d.Provenance.Pointer), pattern)
				assert.Regexp(t, `write \[(\$|\\\^)\] to match it`, d.Message, pattern)
			}
		}
	}
}

// TestApply_SelectsAScalarAsTheCompilerLowersIt pins a filter's reading of a
// scalar to the value the compile keeps of it: a scalar under the document's
// own tag and a !!binary are the strings the source wrote, as an extension
// keeps them, so a filter comparing the text the compiled document shows
// selects them.
func TestApply_SelectsAScalarAsTheCompilerLowersIt(t *testing.T) {
	t.Parallel()
	const src = head + "x-a: {x: !foo 1}\nx-b: {x: !!binary aGk=}\nx-c: {x: 1}\n"
	ov := v11 + "  - target: \"$[?@.x == '1' || @.x == 'aGk=']\"\n    update: {x-hit: true}\n"

	origin, root, diags := applied(t, src, ov, false)

	require.True(t, origin.Applied(), "%+v", diags)
	assert.Equal(t, 2, hits(root), "x-a and x-b, not the number of x-c")
}
