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
			assert.Equal(t, "group-path-prefix", op.Provenance.Inferred, "op %s must be marked inferred", op.ID)
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
