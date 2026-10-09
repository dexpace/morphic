package operation_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// opsByGroup indexes each group's operation IDs, sorted, by the group's ID. It
// is the whole observable shape of a grouping that does not depend on the order
// groups were met in.
func opsByGroup(t *testing.T, groups []ir.OperationGroup) map[ir.GroupID][]ir.OpID {
	t.Helper()
	out := make(map[ir.GroupID][]ir.OpID, len(groups))
	for _, g := range groups {
		require.NotContains(t, out, g.ID, "two groups share the ID %q", g.ID)
		for _, op := range g.Operations {
			out[g.ID] = append(out[g.ID], op.ID)
		}
		slices.Sort(out[g.ID])
	}
	return out
}

// TestGrouping_DeclaredDefaultTagAndFallbackAreDistinct is the reported shape
// (GitHub #673). A declared tag called "default" and the group untagged
// operations fall into render from the same words, with nothing but the naming
// channel — Source in one, Hint in the other — to tell them apart. Their IDs
// must, because an emitter keys everything it keeps about a sub-client by ID.
func TestGrouping_DeclaredDefaultTagAndFallbackAreDistinct(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.0.3
info: {title: T, version: "1"}
tags: [{name: default}]
paths:
  /tagged: {get: {operationId: tagged, tags: [default], responses: {"200": {description: ok}}}}
  /untagged: {get: {operationId: untagged, responses: {"200": {description: ok}}}}
`
	_, svc, diags := lowerServiceSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	require.Len(t, svc.Groups, 2)
	declared, fallback := svc.Groups[0], svc.Groups[1]
	assert.Equal(t, "default", declared.Name.Source, "the declared tag is named by what the source wrote")
	assert.Equal(t, "default", fallback.Name.Hint, "the fallback group is named by a minted hint")
	assert.Equal(t, ir.GroupID("g/tags/default"), declared.ID)
	assert.Equal(t, ir.GroupID("g/default"), fallback.ID)
}

// TestGrouping_PathPrefixGroupsAreIdentifiedByTheirSegment pins the second rule.
// A path-prefix group named "webhooks" must not take the ID of the group webhook
// operations fall into, and the root path, whose first segment is empty, still
// gets an ID of its own.
func TestGrouping_PathPrefixGroupsAreIdentifiedByTheirSegment(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /users/{id}: {get: {operationId: getUser, responses: {"200": {description: ok}}}}
  /users: {get: {operationId: listUsers, responses: {"200": {description: ok}}}}
  /webhooks/register: {post: {operationId: register, responses: {"200": {description: ok}}}}
  /: {get: {operationId: root, responses: {"200": {description: ok}}}}
webhooks:
  onEvent: {post: {operationId: onEvent, responses: {"200": {description: ok}}}}
`
	svc, diags := serviceWithGrouping(t, spec, lowering.GroupByPathPrefix)
	openapitest.RequireNoErrorDiags(t, diags)

	want := map[ir.GroupID][]ir.OpID{
		"g/path-prefix/users":    {"op/openapi/paths/~1users/get", "op/openapi/paths/~1users~1{id}/get"},
		"g/path-prefix/webhooks": {"op/openapi/paths/~1webhooks~1register/post"},
		"g/path-prefix":          {"op/openapi/paths/~1/get"},
		"g/webhooks":             {"op/openapi/webhooks/onEvent/post"},
	}
	assert.Empty(t, cmp.Diff(want, opsByGroup(t, svc.Groups)), "group ID to operations (-want +got)")
}

// TestGrouping_RepeatedTagDeclarationIsOneGroup pins that a group is identified
// by the tag's name and not by a Tag Object. A document that declares a tag
// twice still groups its operations once, and the group keeps one ID.
func TestGrouping_RepeatedTagDeclarationIsOneGroup(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
tags:
  - {name: pets, description: first}
  - {name: pets, description: second}
paths:
  /pets: {get: {operationId: listPets, tags: [pets], responses: {"200": {description: ok}}}}
`
	_, svc, _ := lowerServiceSpec(t, spec)

	require.Len(t, svc.Groups, 1)
	assert.Equal(t, ir.GroupID("g/tags/pets"), svc.Groups[0].ID)
}

// TestGrouping_IDsDoNotDependOnDeclarationOrder compiles one document twice,
// with the paths and the tag list in opposite orders, and diffs what each
// group's ID holds. The groups themselves come out in first-seen order, so the
// two documents list them differently; the IDs and what they hold must not move.
// A group keyed by where its tag happened to be declared would.
func TestGrouping_IDsDoNotDependOnDeclarationOrder(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	const (
		tagsAscending  = "tags: [{name: a}, {name: b}]\n"
		tagsDescending = "tags: [{name: b}, {name: a}]\n"
		pathA          = "  /a: {get: {operationId: opA, tags: [a], responses: {\"200\": {description: ok}}}}\n"
		pathB          = "  /b: {get: {operationId: opB, tags: [b], responses: {\"200\": {description: ok}}}}\n"
		pathC          = "  /c: {get: {operationId: opC, responses: {\"200\": {description: ok}}}}\n"
	)
	forward := head + tagsAscending + "paths:\n" + pathA + pathB + pathC
	reverse := head + tagsDescending + "paths:\n" + pathC + pathB + pathA

	_, first, _ := lowerServiceSpec(t, forward)
	_, second, _ := lowerServiceSpec(t, reverse)

	require.Len(t, first.Groups, 3)
	assert.Empty(t, cmp.Diff(opsByGroup(t, first.Groups), opsByGroup(t, second.Groups)),
		"group ID to operations, forward order against reverse (-forward +reverse)")
}
