// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// paramsOp is an operation declaring id in its path and its query, a header
// X-Up bound twice, a parameter named q.x with a dot in it, a parameter b bound
// in its query and its headers, and a host label.
func paramsOp() *ir.Operation {
	return &ir.Operation{
		Params: []ir.Parameter{
			{ID: "path-id", Name: ir.Naming{Source: "id"}}, {ID: "query-id", Name: ir.Naming{Source: "id"}},
			{ID: "up", Name: ir.Naming{Source: "X-Up"}}, {ID: "dotted", Name: ir.Naming{Source: "q.x"}},
			{ID: "region", Name: ir.Naming{Source: "region"}}, {ID: "both", Name: ir.Naming{Source: "b"}},
		},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{
			{Param: "path-id", Location: ir.HTTPLocationPath},
			{Param: "query-id", Location: ir.HTTPLocationQuery, WireName: "id"},
			{Param: "up", Location: ir.HTTPLocationHeader},
			{Param: "up", Location: ir.HTTPLocationHeader},
			{Param: "dotted", Location: ir.HTTPLocationQuery},
			{Param: "region", Location: ir.HTTPLocationHost},
			{Param: "both", Location: ir.HTTPLocationQuery},
			{Param: "both", Location: ir.HTTPLocationHeader},
		}}}},
	}
}

// TestParamIndex_NamedReadsALocationQualifiedName pins how a name picks a
// parameter: `<in>.<name>` names the one at that location, a name alone the one
// parameter carrying it, a header's in any case, and a dotted name whose prefix
// is no parameter location, or names no match there, is read whole. A
// parameter bound at several places is one match.
func TestParamIndex_NamedReadsALocationQualifiedName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		key     string
		want    ir.ParamID
		matched int
	}{
		{"path.id", "path-id", 1},
		{"query.id", "query-id", 1},
		{"id", "", 2},
		{"x-up", "up", 1},
		{"header.X-UP", "up", 1},
		{"q.x", "dotted", 1},
		{"b", "both", 1},
		{"header.B", "both", 1},
		{"body.id", "", 0},
		{"cookie.id", "", 0},
		{"region", "", 0},
		{"nope", "", 0},
	} {
		id, matched := lowering.IndexParams(paramsOp()).Named(tc.key)
		assert.Equal(t, tc.want, id, tc.key)
		assert.Equal(t, tc.matched, matched, tc.key)
	}
}

// TestParamIndex_LeavesTheHostOut pins that a host binding, which no link
// parameter or replacement names, matches no name, so a name a host label
// shares with a parameter names that parameter alone. No OpenAPI operation
// binds the host, so only a direct call reaches the case.
func TestParamIndex_LeavesTheHostOut(t *testing.T) {
	t.Parallel()
	op := &ir.Operation{Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{
		{Param: "param/x/host", Location: ir.HTTPLocationHost, WireName: "id"},
		{Param: "param/x/id", Location: ir.HTTPLocationQuery, WireName: "id"},
	}}}}}
	ix := lowering.IndexParams(op)
	assert.Equal(t, []ir.ParamID{"param/x/id"}, ix.Bound("", "id"))
	assert.Empty(t, ix.Bound(ir.HTTPLocationHost, "id"))
}
