// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package runtimeexpr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/runtimeexpr"
	"github.com/dexpace/morphic/compilers/openapi/internal/whole"
	"github.com/dexpace/morphic/ir"
)

// requestOp is an operation whose request body is the page and whose
// parameters are a query parameter q of the info model, and a parameter d
// bound in both the query and a header.
func requestOp() *ir.Operation {
	d1, d2 := ir.ParamID("param/d/query"), ir.ParamID("param/d/header")
	return &ir.Operation{
		ID:      "op/r",
		Request: body(content("application/json", "t/page")),
		Params: []ir.Parameter{
			{ID: "param/q", Name: ir.Naming{Source: "q"}, Type: ir.TypeRef{Target: "t/info"}},
			{ID: d1, Name: ir.Naming{Source: "d"}, Type: ir.TypeRef{Target: "t/text"}},
			{ID: d2, Name: ir.Naming{Source: "d"}, Type: ir.TypeRef{Target: "t/text"}},
		},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{
			{Param: "param/q", Location: ir.HTTPLocationQuery},
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
	q := ir.ParamID("param/q")
	for _, tc := range []struct {
		text  string
		want  *ir.InputPath
		typ   ir.TypeID
		error string
	}{
		{"$request.body#/info/next", &ir.InputPath{Segments: []ir.PropID{"p/info", "p/next"}}, "t/text", ""},
		{"$request.body", &ir.InputPath{}, "", ""},
		{"$request.query.q", &ir.InputPath{Param: &q}, "t/info", ""},
		{"$request.q.next", &ir.InputPath{Param: &q, Segments: []ir.PropID{"p/next"}}, "t/text", ""},
		{"$request.items", &ir.InputPath{Segments: []ir.PropID{"p/items"}}, "t/text", ""},
		{"$response.body#/items", nil, "", "names no value of the request"},
		{"$request.query.nope", nil, "", "which the operation does not declare once"},
		{"$request.d", nil, "", "which the operation declares in more than one location"},
		{"$request.", nil, "", "is no expression naming a value of the request"},
		{"$request.nope", nil, "", "holds as no chain of members"},
		{"$request.q.nope", nil, "", "which holds no such chain of members"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			in, typ, why := runtimeexpr.RequestValue(whole.New(&ir.Document{Types: registry()}), requestOp(), tc.text)
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
