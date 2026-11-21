// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/compileddoc"
	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/ir"
)

// requestOperation is an operation whose request body is the page and whose
// parameters are a query parameter q of the info model, and a parameter d
// bound in both the query and a header.
func requestOperation() *ir.Operation {
	d1, d2 := ir.ParameterID("parameter/d/query"), ir.ParameterID("parameter/d/header")
	return &ir.Operation{
		ID:      "operation/r",
		Request: body(content("application/json", "type/page")),
		Params: []ir.Parameter{
			{ID: "parameter/q", Name: ir.Naming{Source: "q"}, Type: ir.TypeRef{Target: "type/info"}},
			{ID: d1, Name: ir.Naming{Source: "d"}, Type: ir.TypeRef{Target: "type/text"}},
			{ID: d2, Name: ir.Naming{Source: "d"}, Type: ir.TypeRef{Target: "type/text"}},
		},
		Bindings: ir.OperationBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{
			{Param: "parameter/q", Location: ir.HTTPLocationQuery},
			{Param: d1, Location: ir.HTTPLocationQuery},
			{Param: d2, Location: ir.HTTPLocationHeader},
		}}}},
	}
}

// TestRequestValue_ResolvesEachSpelling pins what an expression of the request
// names, and the type of what it names: a member of the body or of a
// parameter, a parameter whole, the body whole, which has no one type, and
// why each spelling naming nothing resolves nothing.
func TestRequestValue_ResolvesEachSpelling(t *testing.T) {
	t.Parallel()
	parameterID := ir.ParameterID("parameter/q")
	for _, tc := range []struct {
		text  string
		want  *ir.InputPath
		typ   ir.TypeID
		error string
	}{
		{"$request.body#/info/next", &ir.InputPath{Segments: []ir.PropertyID{"property/info", "property/next"}}, "type/text", ""},
		{"$request.body", &ir.InputPath{}, "", ""},
		{"$request.query.q", &ir.InputPath{Param: &parameterID}, "type/info", ""},
		{"$request.q.next", &ir.InputPath{Param: &parameterID, Segments: []ir.PropertyID{"property/next"}}, "type/text", ""},
		{"$request.items", &ir.InputPath{Segments: []ir.PropertyID{"property/items"}}, "type/text", ""},
		{"$response.body#/items", nil, "", "names no value of the request"},
		{"$request.query.nope", nil, "", "which the operation does not declare once"},
		{"$request.d", nil, "", "which the operation declares in more than one location"},
		{"$request.", nil, "", "is no expression naming a value of the request"},
		{"$request.nope", nil, "", "holds as no chain of members"},
		{"$request.q.nope", nil, "", "which holds no such chain of members"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			in, typ, why := runtimeexpr.RequestValue(compileddoc.New(&ir.Document{Types: registry()}), requestOperation(), tc.text)
			if tc.error != "" {
				assert.Nil(t, in)
				assert.Contains(t, why, tc.error)
				return
			}
			assert.Empty(t, why)
			assert.Equal(t, tc.want, in)
			assert.Equal(t, tc.typ, typ)
		})
	}
}
