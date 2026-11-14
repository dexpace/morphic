// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
	_, svc, diags := compileService(t, spec)
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
  /users/{id}:
    parameters: [{name: id, in: path, required: true, schema: {type: string}}]
    get: {operationId: getUser, responses: {"200": {description: ok}}}
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
	_, svc, _ := compileService(t, spec)

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

	_, first, _ := compileService(t, forward)
	_, second, _ := compileService(t, reverse)

	require.Len(t, first.Groups, 3)
	assert.Empty(t, cmp.Diff(opsByGroup(t, first.Groups), opsByGroup(t, second.Groups)),
		"group ID to operations, forward order against reverse (-forward +reverse)")
}

func TestGrouping_ByFirstTag(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
tags:
  - {name: users, description: User ops}
paths:
  /users:
    get:
      operationId: listUsers
      tags: [users, admin]
      responses: {"200": {description: ok}}
  /misc:
    get:
      operationId: misc
      responses: {"200": {description: ok}}
`
	doc, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 2)
	byName := openapitest.IndexBy(svc.Groups, func(g ir.OperationGroup) string { return g.Name.Source })
	users, ok := byName["users"]
	require.True(t, ok)
	assert.Equal(t, "User ops", users.Docs.Description)
	require.Len(t, users.Operations, 1)
	op := users.Operations[0]
	assert.Equal(t, ir.OpID("op/openapi/paths/~1users/get"), op.ID)
	assert.Equal(t, []string{"users", "admin"}, op.Tags)
	def, ok := byName[""]
	require.True(t, ok, "untagged op lands in the default group")
	assert.Equal(t, "default", def.Name.Hint)
	require.Len(t, doc.TagDefs, 1) // declared tag metadata registered once
}

// TestGrouping_ByPathPrefixInferred pins that grouping by path prefix marks
// every operation it places as inferred, a callback's operation included,
// since it is placed with the operation that declares it.
func TestGrouping_ByPathPrefixInferred(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /users/{id}:
    get:
      operationId: getUser
      parameters: [{name: id, in: path, required: true, schema: {type: string}}]
      responses: {"200": {description: ok}}
  /orders:
    get:
      operationId: listOrders
      callbacks:
        done:
          '{$request.query.cb}':
            post: {operationId: ordersDone, responses: {"200": {description: ok}}}
      responses: {"200": {description: ok}}
`)
	svc, diags := serviceWithGrouping(t, spec, lowering.GroupByPathPrefix)
	openapitest.RequireNoErrorDiags(t, diags)
	byName := openapitest.IndexBy(svc.Groups, func(g ir.OperationGroup) string { return g.Name.Source })
	_, hasUsers := byName["users"]
	_, hasOrders := byName["orders"]
	assert.True(t, hasUsers, "first path segment forms a group")
	assert.True(t, hasOrders)
	require.Len(t, byName["orders"].Operations, 2, "the callback operation is placed beside its parent")
	for _, g := range svc.Groups {
		for _, op := range g.Operations {
			assert.Equal(t, ir.Inferred("group-path-prefix"), op.Provenance.Inferred, "op %s must be marked inferred", op.ID)
		}
	}
}

func TestGrouping_PathPrefixRootPath(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /:
    get: {operationId: root, responses: {"200": {description: ok}}}
`)
	svc, _ := serviceWithGrouping(t, spec, lowering.GroupByPathPrefix)
	require.NotEmpty(t, svc.Groups)
	assert.Empty(t, svc.Groups[0].Name.Source, "root path yields empty first segment")
}

// groupBySource returns the top-level group whose name source is name.
func groupBySource(groups []ir.OperationGroup, name string) (ir.OperationGroup, bool) {
	for _, g := range groups {
		if g.Name.Source == name {
			return g, true
		}
	}
	return ir.OperationGroup{}, false
}

// TestGrouping_NonNavigationalTagIsSkipped pins GitHub #613's first half: an
// operation's first tag may be a badge, which is not a section it belongs to, so
// the group comes from the first *navigational* tag. The badge tag is declared
// first in the operation's `tags` on purpose — the order that was wrong before
// the fix, so reverting the skip groups under the badge and reddens this.
func TestGrouping_NonNavigationalTagIsSkipped(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: beta, kind: badge}
  - {name: books, kind: nav}
paths:
  /books:
    get:
      operationId: listBooks
      tags: [beta, books]
      responses: {"200": {description: ok}}
`
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1, "the badge tag contributes no group")
	books, ok := groupBySource(svc.Groups, "books")
	require.True(t, ok)
	require.Len(t, books.Operations, 1)
	assert.Equal(t, "listBooks", books.Operations[0].Name.Source)
	_, badge := groupBySource(svc.Groups, "beta")
	assert.False(t, badge, "a non-navigational tag never groups")
}

// TestGrouping_BadgeOnlyOperationFallsToDefault is the other arm: when every tag
// an operation names is non-navigational there is no section to group it under,
// and inventing one from a tag that does not group would be an inference.
func TestGrouping_BadgeOnlyOperationFallsToDefault(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: beta, kind: badge}
paths:
  /beta:
    get:
      operationId: betaOp
      tags: [beta]
      responses: {"200": {description: ok}}
`
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1)
	assert.Equal(t, "default", svc.Groups[0].Name.Hint)
	assert.Empty(t, svc.Groups[0].Name.Source)
	require.Len(t, svc.Groups[0].Operations, 1)
}

// TestGrouping_NavigationalChildNestsUnderItsParent pins the tree: a group whose
// tag declares a parent that is itself a navigational declared tag is nested
// under that parent's group. The child is declared before the parent on purpose
// — the parent chain must be built from the declarations rather than from the
// order they arrive in.
func TestGrouping_NavigationalChildNestsUnderItsParent(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: books, parent: catalog, kind: nav}
  - {name: catalog, description: Everything, kind: nav}
paths:
  /books:
    get:
      operationId: listBooks
      tags: [books]
      responses: {"200": {description: ok}}
`
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1, "the child is nested, not a second top-level group")
	catalog := svc.Groups[0]
	assert.Equal(t, "catalog", catalog.Name.Source)
	assert.Empty(t, catalog.Operations, "the ancestor exists to hold its children")
	require.Len(t, catalog.Groups, 1)
	assert.Equal(t, "books", catalog.Groups[0].Name.Source)
	require.Len(t, catalog.Groups[0].Operations, 1)
	assert.Equal(t, "listBooks", catalog.Groups[0].Operations[0].Name.Source)
}

// TestGrouping_AncestorDeclaredAfterItsChildIsStillNested is the same tree with
// the two declarations in the other order, so the nesting cannot depend on which
// of them the document wrote first.
func TestGrouping_AncestorDeclaredAfterItsChildIsStillNested(t *testing.T) {
	t.Parallel()
	childFirst := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: books, parent: catalog, kind: nav}
  - {name: catalog, kind: nav}
paths:
  /books:
    get:
      operationId: listBooks
      tags: [books]
      responses: {"200": {description: ok}}
`
	parentFirst := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: catalog, kind: nav}
  - {name: books, parent: catalog, kind: nav}
paths:
  /books:
    get:
      operationId: listBooks
      tags: [books]
      responses: {"200": {description: ok}}
`
	tree := func(spec string) []ir.OperationGroup {
		t.Helper()
		_, svc, diags := compileService(t, spec)
		openapitest.RequireNoErrorDiags(t, diags)
		return svc.Groups
	}
	assert.Empty(t, cmp.Diff(tree(childFirst), tree(parentFirst)),
		"the group tree must not depend on the declaration order")
}

// TestGrouping_DanglingParentStaysTopLevel pins that a parent naming no declared
// tag ends the walk: the group is top-level rather than nested under a group
// invented for the missing name. The parent MUST exist, so the document draws
// the table's error, and lowers all the same.
func TestGrouping_DanglingParentStaysTopLevel(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: books, parent: nope, kind: nav}
paths:
  /books:
    get:
      operationId: listBooks
      tags: [books]
      responses: {"200": {description: ok}}
`
	_, svc, diags := compileService(t, spec)
	assert.Equal(t, []string{"openapi/validation/validation-invalid-reference at /tags/0/parent"}, errorFindings(diags))
	require.Len(t, svc.Groups, 1)
	assert.Equal(t, "books", svc.Groups[0].Name.Source, "the group is top-level")
}

// TestGrouping_NonNavigationalParentStaysTopLevel holds the parent side of the
// kind rule: a parent that is declared but not navigational does not receive a
// group, so its child stays top-level.
func TestGrouping_NonNavigationalParentStaysTopLevel(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: books, parent: beta, kind: nav}
  - {name: beta, kind: badge}
paths:
  /books:
    get:
      operationId: listBooks
      tags: [books]
      responses: {"200": {description: ok}}
`
	_, svc, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1)
	assert.Equal(t, "books", svc.Groups[0].Name.Source)
}

// TestGrouping_ParentCycleNestsNoMemberUnderAnother pins that a cycle among the
// declared parents terminates and keeps every operation reachable: neither
// member of the cycle nests under the other, while a tag declaring a member as
// its parent still nests under it.
func TestGrouping_ParentCycleNestsNoMemberUnderAnother(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: a, parent: b, kind: nav}
  - {name: b, parent: a, kind: nav}
  - {name: c, parent: a, kind: nav}
paths:
  /a:
    get:
      operationId: opA
      tags: [a]
      responses: {"200": {description: ok}}
  /b:
    get:
      operationId: opB
      tags: [b]
      responses: {"200": {description: ok}}
  /c:
    get:
      operationId: opC
      tags: [c]
      responses: {"200": {description: ok}}
`
	_, svc, diags := compileService(t, spec)
	assert.Equal(t, []string{
		"openapi/validation/validation-circular-reference at /tags/0/parent",
		"openapi/validation/validation-circular-reference at /tags/1/parent",
	}, errorFindings(diags), "the table reports the cycle; the lowering only has to terminate")
	require.Len(t, svc.Groups, 2, "the cycle's members stay top-level")
	byName := openapitest.IndexBy(svc.Groups, func(g ir.OperationGroup) string { return g.Name.Source })
	assert.Empty(t, byName["b"].Groups)
	require.Len(t, byName["a"].Groups, 1, "c nests under a, which is on the cycle but top-level")
	assert.Equal(t, "c", byName["a"].Groups[0].Name.Source)
	for _, g := range []ir.OperationGroup{byName["a"], byName["b"], byName["a"].Groups[0]} {
		require.Len(t, g.Operations, 1, "no operation is lost to the cycle")
	}
}

// TestGrouping_UndeclaredTagIsNavigational pins that a tag an operation uses
// without declaring has no kind to read, so it groups exactly as every 3.0 and
// 3.1 tag does and stays top-level.
func TestGrouping_UndeclaredTagIsNavigational(t *testing.T) {
	t.Parallel()
	_, svc, diags := compileService(t, openapitest.PathsSpec(`  /x:
    get:
      operationId: x
      tags: [adHoc]
      responses: {"200": {description: ok}}
`))
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, svc.Groups, 1)
	assert.Equal(t, "adHoc", svc.Groups[0].Name.Source)
}

// TestGrouping_SameNameDeclaredTwiceIsDeterministic pins that two declarations
// of one tag name resolve the same way whichever order the operations that name
// them are lowered in: the first declaration wins, so a second one declaring a
// different parent cannot move the group midway through the walk. A tag name
// MUST be unique, so the document draws the table's error as well.
func TestGrouping_SameNameDeclaredTwiceIsDeterministic(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: books, kind: nav}
  - {name: books, parent: catalog, kind: nav}
  - {name: catalog, kind: nav}
paths:
  /books:
    get:
      operationId: listBooks
      tags: [books]
      responses: {"200": {description: ok}}
`
	_, svc, diags := compileService(t, spec)
	assert.Equal(t, []string{"openapi/validation/validation-duplicate-key at /tags/1/name"}, errorFindings(diags))
	require.Len(t, svc.Groups, 1, "the first declaration wins for both the kind and the parent")
	assert.Equal(t, "books", svc.Groups[0].Name.Source)
}

// TestTagDefs_RecordParentAndKind pins GitHub #613's second half: the declared
// tag registry keeps the 3.2 parent and kind verbatim, and neither is invented
// for a tag that declares none.
func TestTagDefs_RecordParentAndKind(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: books, parent: catalog, kind: nav}
  - {name: beta, kind: badge}
  - {name: catalog}
paths: {}
`
	doc, _, diags := compileService(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	require.Len(t, doc.TagDefs, 3)
	assert.Equal(t, ir.TagDef{Name: "books", Docs: ir.Docs{}, Parent: "catalog", Kind: "nav"}, doc.TagDefs[0])
	assert.Equal(t, "badge", doc.TagDefs[1].Kind)
	assert.Empty(t, doc.TagDefs[1].Parent)
	assert.Empty(t, doc.TagDefs[2].Parent, "a tag declaring neither keeps both empty")
	assert.Empty(t, doc.TagDefs[2].Kind)
}

// TestGrouping_TreeIsIndependentOfDeclarationOrder is the two-order diff for
// GitHub #613. The group tree is neither the type registry nor a diagnostic, so
// the in-package order-invariance oracle does not see it. Both documents declare
// the same tags and the same operations; only the order of the `tags` list and
// of the badge tag within one operation's `tags` differs, and the tree — nesting,
// sibling order and operations — must be identical.
func TestGrouping_TreeIsIndependentOfDeclarationOrder(t *testing.T) {
	t.Parallel()
	const paths = `paths:
  /books:
    get:
      operationId: listBooks
      tags: [beta, books]
      responses: {"200": {description: ok}}
  /catalog:
    get:
      operationId: showCatalog
      tags: [catalog]
      responses: {"200": {description: ok}}
`
	childFirst := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: books, parent: catalog, kind: nav}
  - {name: catalog, kind: nav}
  - {name: beta, kind: badge}
` + paths
	parentFirst := `openapi: 3.2.0
info: {title: T, version: "1"}
tags:
  - {name: beta, kind: badge}
  - {name: catalog, kind: nav}
  - {name: books, parent: catalog, kind: nav}
` + paths
	tree := func(spec string) []ir.OperationGroup {
		t.Helper()
		_, svc, diags := compileService(t, spec)
		openapitest.RequireNoErrorDiags(t, diags)
		return svc.Groups
	}
	got := tree(childFirst)
	assert.Empty(t, cmp.Diff(tree(parentFirst), got), "the group tree must not depend on declaration order")
	require.Len(t, got, 1, "catalog is the only top-level group")
	require.Len(t, got[0].Groups, 1)
	assert.Equal(t, "books", got[0].Groups[0].Name.Source)
	require.Len(t, got[0].Groups[0].Operations, 1, "the badge tag first in the operation's tags changed nothing")
}

// groupPlacement is where a group sits in a service's tree: the group it nests
// in, the name its source spells, and its operations' IDs in order.
type groupPlacement struct {
	parent ir.GroupID
	name   string
	ops    []ir.OpID
}

// placements flattens a group tree into each group's placement, by ID, walking
// it with a stack of its own so a deep tree costs no recursion.
func placements(t *testing.T, groups []ir.OperationGroup) map[ir.GroupID]groupPlacement {
	t.Helper()
	type pending struct {
		parent ir.GroupID
		group  ir.OperationGroup
	}
	out := map[ir.GroupID]groupPlacement{}
	stack := make([]pending, 0, len(groups))
	for _, g := range slices.Backward(groups) {
		stack = append(stack, pending{group: g})
	}
	for len(stack) > 0 {
		next := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		require.NotContains(t, out, next.group.ID, "two groups share the ID %q", next.group.ID)
		p := groupPlacement{parent: next.parent, name: next.group.Name.Source}
		for _, op := range next.group.Operations {
			p.ops = append(p.ops, op.ID)
		}
		out[next.group.ID] = p
		for _, child := range slices.Backward(next.group.Groups) {
			stack = append(stack, pending{parent: next.group.ID, group: child})
		}
	}
	return out
}

// groupedByExtension is the extension GroupByExtension reads in these tests,
// its lists read as one path and an operation stating none grouped by tags.
var groupedByExtension = lowering.GroupingExtension{
	Key: "x-group", Lists: lowering.ListsAreSegments, Fallback: lowering.GroupByTags,
}

// extensionGroupingSpec states a group path each way a document can, and
// leaves it out, so every filing GroupByExtension makes is beside the others.
// The nested path is declared before the one it nests in, the order in which
// the inner group is met first, and one path item writes the key itself, which
// files none of its operations.
const extensionGroupingSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /accounts/{id}/members:
    parameters: [{name: id, in: path, required: true, schema: {type: string}}]
    get: {operationId: listMembers, x-group: accounts.members, responses: {"200": {description: ok}}}
  /accounts:
    get: {operationId: listAccounts, x-group: accounts, responses: {"200": {description: ok}}}
  /zones:
    get: {operationId: listZones, x-group: [zones, settings], responses: {"200": {description: ok}}}
  /misc:
    x-group: item.level
    get: {operationId: misc, tags: [misc], responses: {"200": {description: ok}}}
  /blank:
    get: {operationId: blank, tags: [misc], x-group: "", responses: {"200": {description: ok}}}
  /bad:
    get: {operationId: bad, tags: [misc], x-group: a..b, responses: {"200": {description: ok}}}
  /nan:
    get: {operationId: nan, x-group: .nan, responses: {"200": {description: ok}}}
  /orders:
    get:
      operationId: listOrders
      x-group: orders
      callbacks:
        done:
          '{$request.query.cb}':
            post: {operationId: ordersDone, x-group: elsewhere, responses: {"200": {description: ok}}}
      responses: {"200": {description: ok}}
webhooks:
  onEvent: {post: {operationId: onEvent, x-group: events, responses: {"200": {description: ok}}}}
`

// TestGrouping_ByExtensionNestsAGroupPerSegment pins GroupByExtension: a
// group for each segment of the path an operation's extension states, nested
// in its prefix's, whether a string or a list spells it. An operation stating
// none is grouped by tags, its path item's key unread, a callback's goes with
// its parent whatever it states, and a webhook's stays in the webhook group, as
// under tags.
func TestGrouping_ByExtensionNestsAGroupPerSegment(t *testing.T) {
	t.Parallel()
	svc, diags := serviceByExtension(t, extensionGroupingSpec, groupedByExtension)
	openapitest.RequireNoErrorDiags(t, diags)

	op := func(path, method string) ir.OpID {
		return ir.OpID("op/openapi/paths/" + strings.ReplaceAll(path, "/", "~1") + "/" + method)
	}
	callback := ir.OpID(string(op("/orders", "get")) + "/callbacks/done/{$request.query.cb}/post")
	want := map[ir.GroupID]groupPlacement{
		"g/extension/accounts": {name: "accounts", ops: []ir.OpID{op("/accounts", "get")}},
		"g/extension/accounts/members": {
			parent: "g/extension/accounts", name: "members", ops: []ir.OpID{op("/accounts/{id}/members", "get")},
		},
		"g/extension/zones":          {name: "zones"},
		"g/extension/zones/settings": {parent: "g/extension/zones", name: "settings", ops: []ir.OpID{op("/zones", "get")}},
		"g/tags/misc":                {name: "misc", ops: []ir.OpID{op("/misc", "get"), op("/blank", "get"), op("/bad", "get")}},
		"g/default":                  {ops: []ir.OpID{op("/nan", "get")}},
		"g/extension/orders":         {name: "orders", ops: []ir.OpID{op("/orders", "get"), callback}},
		"g/webhooks":                 {ops: []ir.OpID{"op/openapi/webhooks/onEvent/post"}},
	}
	got := placements(t, svc.Groups)
	assert.Empty(t, cmp.Diff(want, got, cmp.AllowUnexported(groupPlacement{})), "the group tree (-want +got)")

	ext := ir.Inferred("group-extension")
	marked := map[ir.OpID]ir.Inferred{}
	for _, g := range svc.Groups {
		for _, o := range g.Operations {
			marked[o.ID] = o.Provenance.Inferred
		}
		for _, sub := range g.Groups {
			for _, o := range sub.Operations {
				marked[o.ID] = o.Provenance.Inferred
			}
		}
	}
	assert.Equal(t, map[ir.OpID]ir.Inferred{
		op("/accounts", "get"): ext, op("/accounts/{id}/members", "get"): ext, op("/zones", "get"): ext,
		op("/orders", "get"): ext, callback: ext,
		op("/misc", "get"): "", op("/blank", "get"): "", op("/bad", "get"): "", op("/nan", "get"): "",
		"op/openapi/webhooks/onEvent/post": "",
	}, marked, "the extension's reading is marked where it filed an operation, and only there")
}

// TestGrouping_ByExtensionReportsAValueStatingNoPath pins the one report
// GroupByExtension makes: a value naming no group path, at the key, once. A
// value that does not serialize is reported where every extension is, and not
// a second time here, and empty text states nothing, so neither draws one.
func TestGrouping_ByExtensionReportsAValueStatingNoPath(t *testing.T) {
	t.Parallel()
	svc, diags := serviceByExtension(t, extensionGroupingSpec, groupedByExtension)
	require.NotEmpty(t, svc.Groups)

	var found []string
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct {
			found = append(found, string(d.Severity)+" at "+string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{
		"info at /paths/~1bad/get/x-group",
		"warning at /paths/~1nan/get",
	}, found)
}

// TestGrouping_ByExtensionFallsBackToPathPrefix pins the other fallback: an
// operation stating no path, or a value naming none, is grouped by its first
// path segment and marked as that strategy marks it, beside one the extension
// filed, and the value naming none is reported as under tags.
func TestGrouping_ByExtensionFallsBackToPathPrefix(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /users:
    get: {operationId: listUsers, tags: [people], responses: {"200": {description: ok}}}
  /zones:
    get: {operationId: listZones, x-group: dns.zones, responses: {"200": {description: ok}}}
  /legacy:
    get: {operationId: getLegacy, tags: [people], x-group: a..b, responses: {"200": {description: ok}}}
`)
	ext := groupedByExtension
	ext.Fallback = lowering.GroupByPathPrefix
	svc, diags := serviceByExtension(t, spec, ext)
	openapitest.RequireNoErrorDiags(t, diags)

	require.Len(t, svc.Groups, 3)
	users, dns, legacy := svc.Groups[0], svc.Groups[1], svc.Groups[2]
	assert.Equal(t, ir.GroupID("g/path-prefix/users"), users.ID)
	require.Len(t, users.Operations, 1)
	assert.Equal(t, ir.Inferred("group-path-prefix"), users.Operations[0].Provenance.Inferred)
	assert.Equal(t, ir.GroupID("g/extension/dns"), dns.ID)
	require.Len(t, dns.Groups, 1)
	require.Len(t, dns.Groups[0].Operations, 1)
	assert.Equal(t, ir.Inferred("group-extension"), dns.Groups[0].Operations[0].Provenance.Inferred)
	assert.Equal(t, ir.GroupID("g/path-prefix/legacy"), legacy.ID)
	require.Len(t, legacy.Operations, 1)
	assert.Equal(t, ir.Inferred("group-path-prefix"), legacy.Operations[0].Provenance.Inferred)

	var reported []string
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct {
			reported = append(reported, string(d.Severity)+" at "+string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"info at /paths/~1legacy/get/x-group"}, reported)
}

// TestGrouping_ByExtensionListOfGroupsMintsAGroupForEach pins ListsAreGroups:
// a list names several groups, and the operation is filed under the first
// alone, while each other path gets a group holding no operation, nested in its
// prefix's, for a remount to name. A path another operation is filed under is
// one group, and the extension stays kept whole.
func TestGrouping_ByExtensionListOfGroupsMintsAGroupForEach(t *testing.T) {
	t.Parallel()
	spec := openapitest.PathsSpec(`  /zones:
    get: {operationId: listZones, x-group: [zones.settings, dns.records, users], responses: {"200": {description: ok}}}
  /users:
    get: {operationId: listUsers, x-group: users, responses: {"200": {description: ok}}}
`)
	ext := groupedByExtension
	ext.Lists = lowering.ListsAreGroups
	svc, diags := serviceByExtension(t, spec, ext)
	openapitest.RequireNoErrorDiags(t, diags)

	want := map[ir.GroupID]groupPlacement{
		"g/extension/zones": {name: "zones"},
		"g/extension/zones/settings": {
			parent: "g/extension/zones", name: "settings", ops: []ir.OpID{"op/openapi/paths/~1zones/get"},
		},
		"g/extension/dns":         {name: "dns"},
		"g/extension/dns/records": {parent: "g/extension/dns", name: "records"},
		"g/extension/users":       {name: "users", ops: []ir.OpID{"op/openapi/paths/~1users/get"}},
	}
	got := placements(t, svc.Groups)
	assert.Empty(t, cmp.Diff(want, got, cmp.AllowUnexported(groupPlacement{})), "the group tree (-want +got)")
	entry := svc.Groups[0].Groups[0].Operations[0].Unmodeled["openapi:x-group"]
	assert.JSONEq(t, `["zones.settings","dns.records","users"]`, string(entry.Value), "the extension is kept whole")
}

// aliasedSpec lists aliases each way a document can: beside a path of the
// operation's own, on an operation stating none, as an object stating no path,
// on a webhook, a callback and a path item, none of which the strategy reads,
// and once naming no path, which is reported.
const aliasedSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /issues:
    get:
      operationId: listIssues
      x-group: workers.issues
      x-aliases: [{x-group: o11y.issues, x-name: list}, {x-name: only}, {x-group: workers}]
      callbacks:
        done:
          '{$request.query.cb}':
            post: {operationId: issuesDone, x-aliases: [{x-group: hooks}], responses: {"200": {description: ok}}}
      responses: {"200": {description: ok}}
  /queries:
    x-aliases: [{x-group: item.level}]
    get:
      operationId: listQueries
      tags: [misc]
      x-aliases: [{x-group: o11y.queries}]
      responses: {"200": {description: ok}}
  /bad:
    get: {operationId: bad, tags: [misc], x-aliases: [{x-group: a..b}, {x-group: never}], responses: {"200": {description: ok}}}
webhooks:
  onEvent: {post: {operationId: onEvent, x-aliases: [{x-group: events}], responses: {"200": {description: ok}}}}
`

// aliasedExtension is groupedByExtension reading the aliases aliasedSpec lists.
var aliasedExtension = lowering.GroupingExtension{
	Key: "x-group", Lists: lowering.ListsAreSegments, Aliases: "x-aliases", Fallback: lowering.GroupByTags,
}

// TestGrouping_ByExtensionMintsAGroupForEachAlias pins Aliases: each path an
// operation's aliases state gets a group holding no operation, nested in its
// prefix's, whether the operation is filed by its own path or by the fallback,
// while a webhook's, a callback's and a path item's aliases are not read. A
// value naming no path is reported at its key and mints none of its paths.
func TestGrouping_ByExtensionMintsAGroupForEachAlias(t *testing.T) {
	t.Parallel()
	svc, diags := serviceByExtension(t, aliasedSpec, aliasedExtension)
	openapitest.RequireNoErrorDiags(t, diags)

	issues := ir.OpID("op/openapi/paths/~1issues/get")
	want := map[ir.GroupID]groupPlacement{
		"g/extension/workers": {name: "workers"},
		"g/extension/workers/issues": {parent: "g/extension/workers", name: "issues", ops: []ir.OpID{
			issues, issues + "/callbacks/done/{$request.query.cb}/post",
		}},
		"g/extension/o11y":         {name: "o11y"},
		"g/extension/o11y/issues":  {parent: "g/extension/o11y", name: "issues"},
		"g/tags/misc":              {name: "misc", ops: []ir.OpID{"op/openapi/paths/~1queries/get", "op/openapi/paths/~1bad/get"}},
		"g/extension/o11y/queries": {parent: "g/extension/o11y", name: "queries"},
		"g/webhooks":               {ops: []ir.OpID{"op/openapi/webhooks/onEvent/post"}},
	}
	got := placements(t, svc.Groups)
	assert.Empty(t, cmp.Diff(want, got, cmp.AllowUnexported(groupPlacement{})), "the group tree (-want +got)")

	var found []string
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct {
			found = append(found, string(d.Severity)+" at "+string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"info at /paths/~1bad/get/x-aliases"}, found)
	listQueries := svc.Groups[2].Operations[0]
	require.Equal(t, ir.OpID("op/openapi/paths/~1queries/get"), listQueries.ID)
	assert.Empty(t, listQueries.Provenance.Inferred, "an alias moves no filing, so the fallback's marker stands")
	assert.JSONEq(t, `[{"x-group":"o11y.queries"}]`, string(listQueries.Unmodeled["openapi:x-aliases"].Value),
		"the aliases stay kept whole")
}

// TestGrouping_ByExtensionRemountsAreIndependentOfDeclarationOrder is the
// two-order diff for the groups a remount names: a group minted for an alias
// before an operation is filed in it, or after, and one an alias names inside a
// filed group, nest and hold what they would in the other order.
func TestGrouping_ByExtensionRemountsAreIndependentOfDeclarationOrder(t *testing.T) {
	t.Parallel()
	const aliasing = `  /aliasing:
    get: {operationId: aliasing, x-group: a, x-aliases: [{x-group: o11y.issues}, {x-group: b.c}], responses: {"200": {description: ok}}}
`
	const filed = `  /filed:
    get: {operationId: filed, x-group: o11y.issues, responses: {"200": {description: ok}}}
  /outer:
    get: {operationId: outer, x-group: b, responses: {"200": {description: ok}}}
`
	tree := func(paths string) map[ir.GroupID]groupPlacement {
		t.Helper()
		svc, diags := serviceByExtension(t, openapitest.PathsSpec(paths), aliasedExtension)
		openapitest.RequireNoErrorDiags(t, diags)
		return placements(t, svc.Groups)
	}
	got := tree(aliasing + filed)
	assert.Empty(t, cmp.Diff(tree(filed+aliasing), got, cmp.AllowUnexported(groupPlacement{})),
		"the groups must not depend on declaration order")
	assert.Equal(t, groupPlacement{parent: "g/extension/o11y", name: "issues", ops: []ir.OpID{"op/openapi/paths/~1filed/get"}},
		got["g/extension/o11y/issues"])
	assert.Equal(t, groupPlacement{parent: "g/extension/b", name: "c"}, got["g/extension/b/c"])
}

// TestGrouping_ByExtensionReadsAReferencedPathItemWhereItIsDeclared pins the
// filing of an operation a path item $ref mounts: the extension is the
// declaration's, so a path item mounted twice files both mounts in one group,
// and a value stating no path is reported where it is written, which two
// mounts make one finding rather than one at each.
func TestGrouping_ByExtensionReadsAReferencedPathItemWhereItIsDeclared(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Item'}
  /b: {$ref: '#/components/pathItems/Item'}
  /c: {$ref: '#/components/pathItems/Bad'}
  /d: {$ref: '#/components/pathItems/Bad'}
components:
  pathItems:
    Item:
      get: {x-group: shared.items, responses: {"200": {description: ok}}}
    Bad:
      get: {x-group: [shared, 3], responses: {"200": {description: ok}}}
`
	svc, diags := serviceByExtension(t, spec, groupedByExtension)

	got := placements(t, svc.Groups)
	assert.Equal(t, []ir.OpID{"op/openapi/paths/~1a/get", "op/openapi/paths/~1b/get"},
		got["g/extension/shared/items"].ops, "both mounts in the group their declaration names")
	assert.Equal(t, []ir.OpID{"op/openapi/paths/~1c/get", "op/openapi/paths/~1d/get"}, got["g/default"].ops)

	var findings []string
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct {
			findings = append(findings, string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"/components/pathItems/Bad/get/x-group"}, findings)
}

// TestGrouping_ByExtensionTreeIsIndependentOfDeclarationOrder is the two-order
// diff for GroupByExtension. A group nests in the group its path's prefix
// names, so the operation stating the inner path may come first or last and
// the tree, nesting, order and operations, is the same.
func TestGrouping_ByExtensionTreeIsIndependentOfDeclarationOrder(t *testing.T) {
	t.Parallel()
	const inner = `  /accounts/{id}/members:
    parameters: [{name: id, in: path, required: true, schema: {type: string}}]
    get: {operationId: listMembers, x-group: accounts.members, responses: {"200": {description: ok}}}
`
	const outer = `  /accounts:
    get: {operationId: listAccounts, x-group: accounts, responses: {"200": {description: ok}}}
`
	tree := func(paths string) []ir.OperationGroup {
		t.Helper()
		svc, diags := serviceByExtension(t, openapitest.PathsSpec(paths), groupedByExtension)
		openapitest.RequireNoErrorDiags(t, diags)
		return svc.Groups
	}
	got := tree(inner + outer)
	assert.Empty(t, cmp.Diff(tree(outer+inner), got), "the group tree must not depend on declaration order")
	require.Len(t, got, 1, "accounts is the only top-level group")
	require.Len(t, got[0].Operations, 1)
	require.Len(t, got[0].Groups, 1)
	assert.Equal(t, "members", got[0].Groups[0].Name.Source)
}

// TestGrouping_ACallbackGoesWithItsParentWhateverItsTags pins where every
// strategy files a callback's operation: in its parent's group, as a webhook's
// stays in the webhook group, its own tags kept on it and grouping nothing, so
// no group forms for a tag only a callback names.
func TestGrouping_ACallbackGoesWithItsParentWhateverItsTags(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: F3, version: "1"}
tags: [{name: pets}, {name: hooks}]
paths:
  /a:
    post:
      operationId: parent
      tags: [pets]
      responses: {"200": {description: ok}}
      callbacks:
        onEvent:
          "{$request.body#/url}":
            post:
              operationId: child
              tags: [hooks]
              x-group: hooks
              responses: {"200": {description: ok}}
`
	const parent, child = "op/openapi/paths/~1a/post", "op/openapi/paths/~1a/post/callbacks/onEvent/{$request.body#~1url}/post"
	for strategy, group := range map[lowering.GroupingStrategy]ir.GroupID{
		lowering.GroupByTags: "g/tags/pets", lowering.GroupByPathPrefix: "g/path-prefix/a",
		lowering.GroupByExtension: "g/tags/pets",
	} {
		svc, diags := serviceUnderStrategy(t, spec, strategy)
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Equal(t, map[ir.GroupID][]ir.OpID{group: {parent, child}}, opsByGroup(t, svc.Groups), "%s", strategy)
		assert.Equal(t, []string{"hooks"}, svc.Groups[0].Operations[1].Tags, "%s keeps the callback's tags", strategy)
	}
}

// serviceUnderStrategy is serviceWithGrouping under strategy, reading
// groupedByExtension where strategy is GroupByExtension.
func serviceUnderStrategy(t *testing.T, src string, strategy lowering.GroupingStrategy) (ir.Service, []ir.Diagnostic) {
	t.Helper()
	if strategy == lowering.GroupByExtension {
		return serviceByExtension(t, src, groupedByExtension)
	}
	return serviceWithGrouping(t, src, strategy)
}
