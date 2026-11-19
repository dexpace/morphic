// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compileddoc_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/ir"
)

// paramsOperation is an operation declaring id in its path and its query, a header
// X-Up bound twice, a parameter named q.x with a dot in it, a parameter b bound
// in its query and its headers, and a host label.
func paramsOperation() *ir.Operation {
	return &ir.Operation{
		Params: []ir.Parameter{
			{ID: "path-id", Name: ir.Naming{Source: "id"}}, {ID: "query-id", Name: ir.Naming{Source: "id"}},
			{ID: "up", Name: ir.Naming{Source: "X-Up"}}, {ID: "dotted", Name: ir.Naming{Source: "q.x"}},
			{ID: "region", Name: ir.Naming{Source: "region"}}, {ID: "both", Name: ir.Naming{Source: "b"}},
		},
		Bindings: ir.OperationBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{
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
		want    ir.ParameterID
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
		compiledDocumentView, operation := wholeOf(*paramsOperation())
		id, matched := compiledDocumentView.Params(operation).Named(tc.key)
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
	operation := &ir.Operation{Bindings: ir.OperationBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{
		{Param: "parameter/x/host", Location: ir.HTTPLocationHost, WireName: "id"},
		{Param: "parameter/x/id", Location: ir.HTTPLocationQuery, WireName: "id"},
	}}}}}
	compiledDocumentView, held := wholeOf(*operation)
	ix := compiledDocumentView.Params(held)
	assert.Equal(t, []ir.ParameterID{"parameter/x/id"}, ix.Bound("", "id"))
	assert.Empty(t, ix.Bound(ir.HTTPLocationHost, "id"))
}

// TestParamIndex_PlacementsReadsWhereEachParameterIsBound pins where an index
// says each parameter is bound: every place in binding order, each once, under
// its wire name or else its own name as written, so a header keeps its case;
// a host binding and an unbound parameter place nothing.
func TestParamIndex_PlacementsReadsWhereEachParameterIsBound(t *testing.T) {
	t.Parallel()
	compiledDocumentView, operation := wholeOf(*paramsOperation())
	ix := compiledDocumentView.Params(operation)
	for _, tc := range []struct {
		id   ir.ParameterID
		want []compileddoc.Placement
	}{
		{"query-id", []compileddoc.Placement{{Location: ir.HTTPLocationQuery, Name: "id"}}},
		{"up", []compileddoc.Placement{{Location: ir.HTTPLocationHeader, Name: "X-Up"}}},
		{"both", []compileddoc.Placement{{Location: ir.HTTPLocationQuery, Name: "b"}, {Location: ir.HTTPLocationHeader, Name: "b"}}},
		{"region", nil},
		{"nope", nil},
	} {
		assert.Equal(t, tc.want, ix.Placements(tc.id), tc.id)
	}
}
