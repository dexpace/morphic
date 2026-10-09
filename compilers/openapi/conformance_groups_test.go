package openapi_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// groupsByID indexes a one-service document's top-level groups by ID, failing on
// a repeated ID so the index cannot hide a collision.
func groupsByID(t *testing.T, doc *ir.Document) map[ir.GroupID]ir.OperationGroup {
	t.Helper()
	require.Len(t, doc.Services, 1)
	out := make(map[ir.GroupID]ir.OperationGroup)
	for _, g := range doc.Services[0].Groups {
		_, dup := out[g.ID]
		require.False(t, dup, "group id %s is held by two groups", g.ID)
		out[g.ID] = g
	}
	return out
}

func assertGroupIdentity(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	groups := groupsByID(t, doc)
	require.Len(t, groups, 8)

	declared := groups["g/openapi/tags/default"]
	assert.Equal(t, "default", declared.Name.Source, "the tag keeps its own spelling")
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/tags/1"}, declared.Provenance,
		"a declared group points at its declaration")
	assert.Equal(t, "A declared tag, not the fallback group", declared.Docs.Description)

	fallback := groups["g/synth/openapi/default"]
	assert.Equal(t, "default_2", fallback.Name.Hint, "the fallback yields the spelling a tag claimed")
	assert.Empty(t, fallback.Name.Source)

	hook := groups["g/synth/openapi/webhooks"]
	assert.Equal(t, "webhooks_2", hook.Name.Hint, "the webhook group yields the spelling a tag claimed")

	assert.Contains(t, groups, ir.GroupID("g/openapi/tags/a~1b"))
	assert.Contains(t, groups, ir.GroupID("g/openapi/tags/a~0b"))
	assert.Contains(t, groups, ir.GroupID("g/openapi/tags/empty"))
	undeclared := groups["g/openapi/tags/never-declared"]
	assert.Equal(t, "never-declared", undeclared.Name.Source, "a used tag declares its own group")
	assert.Empty(t, undeclared.Provenance.Pointer, "an undeclared tag has no declaration to point at")
}

// TestGroupIdentity_TagOrderDoesNotChangeAnyID compiles one API twice with its
// tags and paths in opposite orders. An index-derived ID would rebind to a
// different tag when the declarations swap; a name-derived one cannot.
func TestGroupIdentity_TagOrderDoesNotChangeAnyID(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: T, version: '1'}\n"
	const tagA, tagB = "  - {name: alpha, description: A}\n", "  - {name: beta, description: B}\n"
	const pathA = "  /a: {get: {operationId: a, tags: [alpha], responses: {'200': {description: ok}}}}\n"
	const pathB = "  /b: {get: {operationId: b, tags: [beta], responses: {'200': {description: ok}}}}\n"
	forward := head + "tags:\n" + tagA + tagB + "paths:\n" + pathA + pathB
	reversed := head + "tags:\n" + tagB + tagA + "paths:\n" + pathB + pathA

	summary := func(spec string) map[ir.GroupID][2]string {
		doc, _, err := compileSpec(t.Context(), "t.yaml", []byte(spec))
		require.NoError(t, err)
		out := make(map[ir.GroupID][2]string)
		for id, g := range groupsByID(t, doc) {
			out[id] = [2]string{g.Name.Source, g.Docs.Description}
		}
		return out
	}
	got, want := summary(reversed), summary(forward)
	require.Len(t, want, 2)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("reordering tags and paths changed a group's identity (-forward +reversed):\n%s", diff)
	}
}

// TestGroupIdentity_PathPrefixGroupsAreSynthesized pins the path-prefix rule: its
// groups are minted, so they take the synthesized space, and one collides with
// the webhook group's spelling exactly as a declared tag does under tags.
func TestGroupIdentity_PathPrefixGroupsAreSynthesized(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: '1'}
paths:
  /webhooks/x: {get: {operationId: x, responses: {'200': {description: ok}}}}
webhooks:
  ping: {post: {operationId: ping, responses: {'200': {description: ok}}}}
`
	doc, _, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: "t.yaml", Data: []byte(spec)}},
		compilers.Options{FormatOptions: openapi.Options{Grouping: openapi.GroupByPathPrefix}})
	require.NoError(t, err)

	groups := groupsByID(t, doc)
	require.Len(t, groups, 2)
	assert.Equal(t, "webhooks", groups["g/synth/openapi/path-prefix/webhooks"].Name.Source)
	assert.Equal(t, "webhooks_2", groups["g/synth/openapi/webhooks"].Name.Hint)
}
