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
// X-Up, a parameter named q.x with a dot in it, and a host label.
func paramsOp() *ir.Operation {
	return &ir.Operation{
		Params: []ir.Parameter{
			{ID: "path-id", Name: ir.Naming{Source: "id"}}, {ID: "query-id", Name: ir.Naming{Source: "id"}},
			{ID: "up", Name: ir.Naming{Source: "X-Up"}}, {ID: "dotted", Name: ir.Naming{Source: "q.x"}},
			{ID: "region", Name: ir.Naming{Source: "region"}},
		},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{
			{Param: "path-id", Location: ir.HTTPLocationPath},
			{Param: "query-id", Location: ir.HTTPLocationQuery, WireName: "id"},
			{Param: "up", Location: ir.HTTPLocationHeader},
			{Param: "up", Location: ir.HTTPLocationHeader},
			{Param: "dotted", Location: ir.HTTPLocationQuery},
			{Param: "region", Location: ir.HTTPLocationHost},
		}}}},
	}
}

// TestParamNamed_ReadsALocationQualifiedName pins how a name picks a
// parameter: `<in>.<name>` names the one at that location, a name alone the one
// parameter carrying it, a header's in any case, and a dotted name whose prefix
// is no parameter location, or names no match there, is read whole.
func TestParamNamed_ReadsALocationQualifiedName(t *testing.T) {
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
		{"body.id", "", 0},
		{"cookie.id", "", 0},
		{"region", "", 0},
		{"nope", "", 0},
	} {
		id, matched := lowering.ParamNamed(paramsOp(), tc.key)
		assert.Equal(t, tc.want, id, tc.key)
		assert.Equal(t, tc.matched, matched, tc.key)
	}
}
