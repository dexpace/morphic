package resolve_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
)

// referenceKindsSpec holds a reference of each kind but a schema's that
// resolve.ReferenceEnd names, each to an object holding extensions, keys it
// merges from x-base, a key written twice and keys that need escaping.
const referenceKindsSpec = `openapi: 3.1.0
info: {title: T, version: '1'}
x-base: &base {x-m: {q: 1}, x-shared: base}
paths:
  /p: {$ref: '#/components/pathItems/X'}
components:
  responses:
    X: {description: s, <<: *base, x-foo: {a: 1}, x-bar: [1, 2], x-dup: one, x-dup: two, 'x-a/b': {c: 1}, 'x-t~': 1}
    R: {$ref: '#/components/responses/X'}
  parameters:
    X: {name: p, in: query, schema: {type: string}, <<: *base, x-foo: {a: 1}, x-bar: [1, 2], x-dup: one, x-dup: two, 'x-a/b': {c: 1}, 'x-t~': 1}
    R: {$ref: '#/components/parameters/X'}
  examples:
    X: {value: 1, <<: *base, x-foo: {a: 1}, x-bar: [1, 2], x-dup: one, x-dup: two, 'x-a/b': {c: 1}, 'x-t~': 1}
    R: {$ref: '#/components/examples/X'}
  requestBodies:
    X: {content: {application/json: {schema: {type: string}}}, <<: *base, x-foo: {a: 1}, x-bar: [1, 2], x-dup: one, x-dup: two, 'x-a/b': {c: 1}, 'x-t~': 1}
    R: {$ref: '#/components/requestBodies/X'}
  headers:
    X: {schema: {type: string}, <<: *base, x-foo: {a: 1}, x-bar: [1, 2], x-dup: one, x-dup: two, 'x-a/b': {c: 1}, 'x-t~': 1}
    R: {$ref: '#/components/headers/X'}
  securitySchemes:
    X: {type: apiKey, name: k, in: header, <<: *base, x-foo: {a: 1}, x-bar: [1, 2], x-dup: one, x-dup: two, 'x-a/b': {c: 1}, 'x-t~': 1}
    R: {$ref: '#/components/securitySchemes/X'}
  links:
    X: {operationId: op, <<: *base, x-foo: {a: 1}, x-bar: [1, 2], x-dup: one, x-dup: two, 'x-a/b': {c: 1}, 'x-t~': 1}
    R: {$ref: '#/components/links/X'}
  callbacks:
    X: {'{$request.body#/u}': {post: {responses: {'200': {description: ok}}}}, <<: *base, x-foo: {a: 1}, x-bar: [1, 2], x-dup: one, x-dup: two, 'x-a/b': {c: 1}, 'x-t~': 1}
    R: {$ref: '#/components/callbacks/X'}
  pathItems:
    X: {get: {responses: {'200': {description: ok}}}, <<: *base, x-foo: {a: 1}, x-bar: [1, 2], x-dup: one, x-dup: two, 'x-a/b': {c: 1}, 'x-t~': 1}
`

// TestScope_Locate_HoldsAnswersAsTheLibraryAtEveryReferenceKind holds a walk
// asking Holds, as the lowering's does, to one asking the library, where the
// walk leaves the model at a reference of each kind: for tokens the target
// holds itself, through a merge, escaped or written twice, and tokens it does
// not hold. Every reference reads as ending in another document, so each one
// passed shows in the scope. At 6a9f53d, no corpus position left the model at
// a reference into a token its target holds.
func TestScope_Locate_HoldsAnswersAsTheLibraryAtEveryReferenceKind(t *testing.T) {
	t.Parallel()
	loaded, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(referenceKindsSpec)},
		load.Options{})
	require.NoError(t, err)
	require.NotNil(t, loaded)
	library := resolve.Scope{SelfPath: "spec.yaml", Doc: loaded.Doc, Ends: endsElsewhere}
	asked, held := 0, 0
	indexed := library
	indexed.Holds = func(raw *yaml.Node, token string) bool {
		asked++
		answer := loaded.Targets.Holds(raw, token)
		if answer {
			held++
		}
		return answer
	}
	for _, base := range []string{"/components/responses/R", "/components/parameters/R", "/components/examples/R",
		"/components/requestBodies/R", "/components/headers/R", "/components/securitySchemes/R",
		"/components/links/R", "/components/callbacks/R", "/paths/~1p"} {
		for _, tail := range []string{"x-foo", "x-foo/a", "x-bar/1", "x-m/q", "x-shared", "x-dup", "x-a~1b/c", "x-t~0",
			"x-missing", "x-missing/a", "<<", "", "description"} {
			pointer := jsontext.Pointer(base + "/" + tail)
			want, got := library.Locate(pointer), indexed.Locate(pointer)
			assert.Equal(t, want.At().Foreign, got.At().Foreign, "%q", pointer)
			assert.Same(t, want.Model(), got.Model(), "%q", pointer)
		}
	}
	assert.Greater(t, held, 50, "the targets hold most of the tokens asked")
	assert.Greater(t, asked-held, 10, "and not every one")
}

// endsElsewhere reads every reference the loader resolved as ending in another
// document, so a walk that passes one shows it in the scope it reads.
func endsElsewhere(node any) (resolve.End, bool) {
	if _, ok := resolve.ReferenceEnd(node); !ok {
		return resolve.End{}, false
	}
	return resolve.End{Document: "elsewhere", Path: "x.yaml"}, true
}
