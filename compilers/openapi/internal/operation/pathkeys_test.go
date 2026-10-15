package operation_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// pathKeySpec wraps one Paths Object key in a minimal document of the given
// version. The key is Go-quoted, which YAML reads as the same double-quoted
// scalar, so a key with a space, a control byte or a quote stays one key.
func pathKeySpec(version, key string) string {
	return openapitest.PathsSpecVer(version, fmt.Sprintf(
		"  %s:\n    get:\n      operationId: op\n      responses: {\"200\": {description: ok}}\n",
		strconv.Quote(key)))
}

// TestPathKeys_MalformedKeyIsReportedOnce is GitHub #641: a Paths Object key
// that is not a path template lowered in silence. One warning now names every
// rule the key breaks, at the key's path item, and the key still reaches the
// IR verbatim, which is what makes a warning the honest severity.
func TestPathKeys_MalformedKeyIsReportedOnce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key, ptr, msg string
	}{
		{
			key: "a/b c?x",
			ptr: "/paths/a~1b c?x",
			msg: `path key "a/b c?x" is not a valid path template: does not begin with "/"; ` +
				`contains " ", which a path admits only percent-encoded; carries the query "?x"; ` +
				`the key is used as written`,
		},
		{
			key: "/foo/{param",
			ptr: "/paths/~1foo~1{param",
			msg: `path key "/foo/{param" is not a valid path template: has a brace that is not part ` +
				`of a "{name}" template expression; the key is used as written`,
		},
		{
			key: "/users/{id}/issues/{id}",
			ptr: "/paths/~1users~1{id}~1issues~1{id}",
			msg: `path key "/users/{id}/issues/{id}" is not a valid path template: repeats "{id}"; ` +
				`the key is used as written`,
		},
		{
			key: "/a\tb",
			ptr: "/paths/~1a\tb",
			msg: `path key "/a\tb" is not a valid path template: contains "\t", which a path ` +
				`admits only percent-encoded; the key is used as written`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := lowerServiceSpec(t, pathKeySpec("3.1.0", tc.key))
			openapitest.RequireNoErrorDiags(t, diags)

			msg := openapitest.DiagMessageAt(t, diags, diag.InvalidPathKey, ir.SeverityWarning, tc.ptr)
			assert.Equal(t, tc.msg, msg)
			assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.InvalidPathKey, ir.SeverityWarning),
				"one diagnostic per key, not per defect")
			assert.Equal(t, tc.key, openapitest.FirstOp(t, svc).Bindings.HTTP[0].URITemplate,
				"the key reaches the IR verbatim")
		})
	}
}

// TestPathKeys_ValidKeysAreNotReported is the overreach guard: a rule refusing
// anything unusual would pass the test above while warning on correct keys,
// such as a template name with a space, which the grammar admits.
func TestPathKeys_ValidKeysAreNotReported(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"/", "/a/b", "/a/", "/a/{id}", "/a/%20", "/a/{test id}", "/{a}{b}"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := lowerServiceSpec(t, pathKeySpec("3.1.0", key))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.False(t, openapitest.HasDiag(diags, diag.InvalidPathKey), "%q is a path template; got %+v", key, diags)
			assert.Equal(t, key, openapitest.FirstOp(t, svc).Bindings.HTTP[0].URITemplate)
		})
	}
}

// TestPathKeys_RulesAreNotVersionGated pins that every version is read by the
// same rules. The key breaks the two only 3.2.0 states, an empty segment and
// an expression used twice, so a check gated to that version's text would pass
// over it in 3.0 and 3.1.
func TestPathKeys_RulesAreNotVersionGated(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			_, _, diags := lowerServiceSpec(t, pathKeySpec(version, "/a//{id}/{id}"))
			openapitest.RequireNoErrorDiags(t, diags)
			msg := openapitest.DiagMessageAt(t, diags, diag.InvalidPathKey, ir.SeverityWarning,
				"/paths/~1a~1~1{id}~1{id}")
			assert.Contains(t, msg, `repeats "{id}"; has an empty segment ("//")`)
		})
	}
}

// TestPathKeys_FragmentIsLeftToGitHub602 pins that a "#" ends what is read.
// GitHub #602 will strip the fragment and report it, so nothing in or after it
// draws a warning here, while what precedes it is read as any path is.
func TestPathKeys_FragmentIsLeftToGitHub602(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key, ptr, msg string // ptr and msg empty: no warning
	}{
		{key: "/a#frag"},
		{key: "/a#b c?x//{id}{id}{"},
		{
			key: "/a b#c<d",
			ptr: "/paths/~1a b#c<d",
			msg: `path key "/a b#c<d" is not a valid path template: contains " ", which a path ` +
				`admits only percent-encoded; the key is used as written`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := lowerServiceSpec(t, pathKeySpec("3.1.0", tc.key))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Equal(t, tc.key, openapitest.FirstOp(t, svc).Bindings.HTTP[0].URITemplate)
			if tc.msg == "" {
				assert.False(t, openapitest.HasDiag(diags, diag.InvalidPathKey), "got %+v", diags)
				return
			}
			msg := openapitest.DiagMessageAt(t, diags, diag.InvalidPathKey, ir.SeverityWarning, tc.ptr)
			assert.Equal(t, tc.msg, msg, "the fragment's own characters are not named")
		})
	}
}

// TestPathKeys_KeyIsReadWhateverItsItemLowers pins that a key is judged before
// its path item resolves: one whose $ref resolves to nothing, one holding an
// empty item and one whose item declares no operation are each reported,
// though none reaches the IR as a binding.
func TestPathKeys_KeyIsReadWhateverItsItemLowers(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  "a b": {$ref: '#/components/pathItems/missing'}
  "c d": {}
  "e f":
    parameters: [{name: x, in: query, schema: {type: string}}]
components: {pathItems: {}}
`
	_, _, diags := lowerServiceSpec(t, spec)
	require.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, "/paths/a b"),
		"the $ref must be the unresolved one this case is about; got %+v", diags)
	for _, ptr := range []string{"/paths/a b", "/paths/c d", "/paths/e f"} {
		msg := openapitest.DiagMessageAt(t, diags, diag.InvalidPathKey, ir.SeverityWarning, ptr)
		assert.Contains(t, msg, `does not begin with "/"; contains " "`)
	}
}

// TestPathKeys_OnlyPathsKeysAreRead pins what the check leaves alone: a
// webhook key names an event, a callback key is a runtime expression, and a
// Paths Object x- key is an extension. Read as paths, each would lack its "/".
func TestPathKeys_OnlyPathsKeysAreRead(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  x-meta: {a: 1}
  /a:
    post:
      operationId: a
      callbacks:
        cb:
          "{$request.body#/url}":
            post: {operationId: cb, responses: {"200": {description: ok}}}
      responses: {"200": {description: ok}}
webhooks:
  newPet:
    post: {operationId: newPet, responses: {"200": {description: ok}}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.False(t, openapitest.HasDiag(diags, diag.InvalidPathKey), "got %+v", diags)

	keys := map[string]bool{}
	for _, grp := range svc.Groups {
		for _, op := range grp.Operations {
			for _, hb := range op.Bindings.HTTP {
				keys[hb.URITemplate+hb.WebhookName] = true
			}
		}
	}
	assert.Equal(t, map[string]bool{"/a": true, "{$request.body#/url}": true, "newPet": true}, keys,
		"each key the case writes is lowered, so each was met")
}
