// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package operation_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/ir"
)

// groupIDs lists the ID of each group, in order.
func groupIDs(groups []ir.OperationGroup) []ir.GroupID {
	ids := make([]ir.GroupID, 0, len(groups))
	for _, operationGroup := range groups {
		ids = append(ids, operationGroup.ID)
	}
	return ids
}

// TestGroups_PathPrefixStopsAtTheQuery pins that a path key's query is no part
// of the path that names its group: a key with a query files beside the key
// without one, in either declaration order, one whose path is the root files
// under the root's group, and the URI template keeps the query. A key's query
// is reported as an invalid path key, which the grouping does not repeat.
func TestGroups_PathPrefixStopsAtTheQuery(t *testing.T) {
	t.Parallel()
	const users = `  /users:
    get: {operationId: listUsers, responses: {"200": {description: ok}}}
`
	const admins = `  /users?role=admin:
    get: {operationId: listAdmins, responses: {"200": {description: ok}}}
`
	const probe = `  /?probe=1:
    get: {operationId: probe, responses: {"200": {description: ok}}}
`
	const head = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n"
	want := map[ir.GroupID][]ir.OperationID{
		"operation-group/path-prefix/users": {"operation/openapi/paths/~1users/get", "operation/openapi/paths/~1users?role=admin/get"},
		"operation-group/path-prefix":       {"operation/openapi/paths/~1?probe=1/get"},
	}
	for _, tc := range []struct {
		name  string
		paths string
		order []ir.GroupID
	}{
		{"the key without a query first", users + admins + probe, []ir.GroupID{"operation-group/path-prefix/users", "operation-group/path-prefix"}},
		{"the key with a query first", admins + users + probe, []ir.GroupID{"operation-group/path-prefix/users", "operation-group/path-prefix"}},
		{"the root first", probe + admins + users, []ir.GroupID{"operation-group/path-prefix", "operation-group/path-prefix/users"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, diags := serviceWithGrouping(t, head+tc.paths, service.GroupByPathPrefix)
			openapitest.RequireNoErrorDiags(t, diags)

			assert.Equal(t, tc.order, groupIDs(svc.Groups))
			assert.Empty(t, cmp.Diff(want, opsByGroup(t, svc.Groups)), "group ID to operations (-want +got)")
			assert.Equal(t, 2, openapitest.CountDiagsAt(diags, diag.InvalidPathKey, ir.SeverityWarning),
				"one warning per key carrying a query, and none from the grouping")
			for _, operationGroup := range svc.Groups {
				switch operationGroup.ID {
				case "operation-group/path-prefix":
					assert.Equal(t, ir.Naming{Hint: "empty"}, operationGroup.Name)
				case "operation-group/path-prefix/users":
					assert.Equal(t, "users", operationGroup.Name.Source)
				}
			}
		})
	}
}

// TestGroups_PathPrefixKeepsTheQueryInTheTemplate pins that grouping by the path
// alone leaves the key as the operation's URI template.
func TestGroups_PathPrefixKeepsTheQueryInTheTemplate(t *testing.T) {
	t.Parallel()
	svc, _ := serviceWithGrouping(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /users?role=admin:
    get: {operationId: listAdmins, responses: {"200": {description: ok}}}
`, service.GroupByPathPrefix)

	require.Len(t, svc.Groups, 1)
	require.Len(t, svc.Groups[0].Operations, 1)
	assert.Equal(t, "operation-group/path-prefix/users", string(svc.Groups[0].ID))
	require.Len(t, svc.Groups[0].Operations[0].Bindings.HTTP, 1)
	assert.Equal(t, "/users?role=admin", svc.Groups[0].Operations[0].Bindings.HTTP[0].URITemplate)
}

// TestGroups_PathPrefixReadsAQuestionMarkInsideATemplateExpression pins that a
// "?" within braces is no query: the first segment is the whole expression.
func TestGroups_PathPrefixReadsAQuestionMarkInsideATemplateExpression(t *testing.T) {
	t.Parallel()
	svc, _ := serviceWithGrouping(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /{a?b}/users:
    get:
      operationId: odd
      parameters: [{name: "a?b", in: path, required: true, schema: {type: string}}]
      responses: {"200": {description: ok}}
`, service.GroupByPathPrefix)

	assert.Equal(t, []ir.GroupID{"operation-group/path-prefix/{a?b}"}, groupIDs(svc.Groups))
}
