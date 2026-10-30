// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestValidate_ParamBinding_UndeclaredIDIsReportedOnceAsDangling pins that a
// binding naming an ID no operation declares draws the dangling walk's report
// alone. The declared parameter is bound too, so no unbound warning shares the
// mismatch code.
func TestValidate_ParamBinding_UndeclaredIDIsReportedOnceAsDangling(t *testing.T) {
	t.Parallel()
	op := paramBindingOp("op", ir.Parameter{ID: "param/x/op/id/query", Name: ir.Naming{Source: "id"}})
	op.Bindings.HTTP[0].ParamBindings = append(op.Bindings.HTTP[0].ParamBindings,
		ir.HTTPParamBinding{Param: "param/x/op/ghost/query", Location: ir.HTTPLocationQuery})
	diags := validated(t, docWithOperation(op))
	assert.Equal(t, 1, countCode(t, diags, "ir/dangling-param-ref"))
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"), "the dangling walk owns an ID nothing declares")
}

// TestValidate_ParamBinding_EmptyParamIsRejected pins that a binding naming no
// parameter at all is reported here: the dangling walk skips an empty ID.
func TestValidate_ParamBinding_EmptyParamIsRejected(t *testing.T) {
	t.Parallel()
	op := paramBindingOp("op", ir.Parameter{ID: "param/x/op/id/query", Name: ir.Naming{Source: "id"}})
	op.Bindings.HTTP[0].ParamBindings = append(op.Bindings.HTTP[0].ParamBindings,
		ir.HTTPParamBinding{Location: ir.HTTPLocationQuery})
	diags := validated(t, docWithOperation(op))
	assert.Equal(t, 1, countCode(t, diags, "pass/param-binding-mismatch"))
	assert.Zero(t, countCode(t, diags, "ir/dangling-param-ref"))
}

func TestValidate_ParamBinding_DoubleBound(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID:     "op",
		Params: []ir.Parameter{{ID: "param/x/op/id/query", Name: ir.Naming{Source: "id"}, Type: ir.TypeRef{Target: "t/prim/string"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x",
			ParamBindings: []ir.HTTPParamBinding{
				{Param: "param/x/op/id/query", Location: ir.HTTPLocationQuery},
				{Param: "param/x/op/id/query", Location: ir.HTTPLocationQuery},
			},
		}}},
	}
	var messages []string
	for _, d := range validated(t, docWithOperation(op)) {
		if d.Code == "pass/param-binding-mismatch" {
			messages = append(messages, d.Message)
		}
	}
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0], `parameter "id" is bound more than once in location "query"`,
		"the report names the parameter, not its ID")
}

// paramBindingOp returns an operation declaring the given parameters and
// binding each one once, in the query.
func paramBindingOp(id ir.OpID, params ...ir.Parameter) ir.Operation {
	op := ir.Operation{ID: id, Params: params}
	bindings := make([]ir.HTTPParamBinding, 0, len(params))
	for _, p := range params {
		bindings = append(bindings, ir.HTTPParamBinding{Param: p.ID, Location: ir.HTTPLocationQuery})
	}
	op.Bindings = ir.OpBindings{HTTP: []ir.HTTPBinding{{Method: "GET", URITemplate: "/x", ParamBindings: bindings}}}
	return op
}

// TestValidate_ParamBinding_SameNameDifferentLocation pins that a parameter is
// identified by its ID: id in the query and id in the path are two parameters,
// each bound once, so neither binding is a duplicate of the other.
func TestValidate_ParamBinding_SameNameDifferentLocation(t *testing.T) {
	t.Parallel()
	op := paramBindingOp("op/x/get",
		ir.Parameter{ID: "param/x/get/id/query", Name: ir.Naming{Source: "id"}},
		ir.Parameter{ID: "param/x/get/id/path", Name: ir.Naming{Source: "id"}},
	)
	op.Bindings.HTTP[0].ParamBindings[1].Location = ir.HTTPLocationPath
	assert.NotContains(t, irtest.DiagCodes(validated(t, docWithOperation(op))), "pass/param-binding-mismatch")
}

// TestValidate_ParamBinding_OtherOperationsParamIsRejected pins that a binding
// names a parameter of its own operation: another operation's parameter is a
// well-formed reference, so only this check can say it is the wrong one.
func TestValidate_ParamBinding_OtherOperationsParamIsRejected(t *testing.T) {
	t.Parallel()
	get := paramBindingOp("op/x/get", ir.Parameter{ID: "param/x/get/id/query", Name: ir.Naming{Source: "id"}})
	post := paramBindingOp("op/x/post", ir.Parameter{ID: "param/x/post/id/query", Name: ir.Naming{Source: "id"}})
	post.Bindings.HTTP[0].ParamBindings[0].Param = "param/x/get/id/query"

	doc := validDoc()
	doc.Services = []ir.Service{{ID: "s", Groups: []ir.OperationGroup{{Operations: []ir.Operation{get, post}}}}}
	diags := validated(t, doc)

	var mismatches []string
	for _, d := range diags {
		if d.Code == "pass/param-binding-mismatch" {
			mismatches = append(mismatches, d.Message)
		}
	}
	assert.Len(t, mismatches, 2, "the foreign binding, and the parameter post then leaves unbound: %v", mismatches)
	assert.Contains(t, mismatches[0], "does not declare")
}

// TestValidate_ParamBinding_EachBindingIsJudgedAlone pins that an operation's
// HTTP bindings are judged one at a time, each report placed at its binding.
// The first binding places the parameter once: a second placing nothing
// leaves it unbound there, one placing it once binds it once there too, and
// the second's repeat and foreign parameter are the second's.
func TestValidate_ParamBinding_EachBindingIsJudgedAlone(t *testing.T) {
	t.Parallel()
	own := ir.HTTPParamBinding{Param: "param/x/op/id/query", Location: ir.HTTPLocationQuery}
	foreign := ir.HTTPParamBinding{Param: "param/x/other/id/query", Location: ir.HTTPLocationQuery}
	type report struct {
		severity ir.Severity
		message  string
	}
	for _, tc := range []struct {
		name   string
		second []ir.HTTPParamBinding
		want   []report
	}{
		{"placing nothing", nil, []report{{ir.SeverityWarning,
			`parameter "id" on op/bindings/http/1 is not bound to any HTTP location`}}},
		{"placing it once", []ir.HTTPParamBinding{own}, []report{}},
		{"placing it twice", []ir.HTTPParamBinding{own, own}, []report{{ir.SeverityError,
			`parameter "id" is bound more than once in location "query" on op/bindings/http/1`}}},
		{"placing another operation's parameter", []ir.HTTPParamBinding{own, foreign}, []report{{ir.SeverityError,
			`binding on op/bindings/http/1 names parameter "param/x/other/id/query", which this operation does not declare`}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op := paramBindingOp("op", ir.Parameter{ID: own.Param, Name: ir.Naming{Source: "id"}})
			op.Bindings.HTTP = append(op.Bindings.HTTP,
				ir.HTTPBinding{Method: "POST", URITemplate: "/x", ParamBindings: tc.second})
			other := paramBindingOp("other", ir.Parameter{ID: foreign.Param, Name: ir.Naming{Source: "id"}})

			found := withCode(validated(t, docWithOperations(op, other)), "pass/param-binding-mismatch")
			got := make([]report, 0, len(found))
			for _, d := range found {
				assert.Equal(t, "op/bindings/http/1", d.Provenance.Node, d.Message)
				got = append(got, report{d.Severity, d.Message})
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestValidate_HostParamBoundMultipleTimesIsLegal covers the host-location
// continue: a host label may be filled by a param several times without a
// double-bind error.
func TestValidate_HostParamBoundMultipleTimesIsLegal(t *testing.T) {
	t.Parallel()
	op := ir.Operation{
		ID:     "op",
		Params: []ir.Parameter{{ID: "param/x/op/region/host", Name: ir.Naming{Source: "region"}, Type: ir.TypeRef{Target: "t/prim/string"}}},
		Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			Method: "GET", URITemplate: "/x", HostPrefix: "{region}.{region}",
			ParamBindings: []ir.HTTPParamBinding{
				{Param: "param/x/op/region/host", Location: ir.HTTPLocationHost},
				{Param: "param/x/op/region/host", Location: ir.HTTPLocationHost},
			},
		}}},
	}
	assert.NotContains(t, irtest.DiagCodes(validated(t, docWithOperation(op))), "pass/param-binding-mismatch")
}

// paramRefOp returns an operation declaring one parameter and pointing its
// pagination cursor, limit and idempotency token at the given IDs.
func paramRefOp(id ir.OpID, own ir.ParamID, cursor, limit, token ir.ParamID) ir.Operation {
	return ir.Operation{
		ID:     id,
		Params: []ir.Parameter{{ID: own, Name: ir.Naming{Source: "p"}}},
		Pagination: &ir.Pagination{
			InputCursor: &ir.ParamPath{Param: cursor},
			InputLimit:  &ir.ParamPath{Param: limit},
		},
		Idempotency: ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: token},
	}
}

func TestValidate_ParamReferences_OwnOperationIsClean(t *testing.T) {
	t.Parallel()
	op := paramRefOp("op/x/put", "param/x/put/p", "param/x/put/p", "param/x/put/p", "param/x/put/p")
	diags := validated(t, docWithOperations(op))
	assert.NotContains(t, irtest.DiagCodes(diags), "pass/param-binding-mismatch")
	assert.NotContains(t, irtest.DiagCodes(diags), "ir/dangling-param-ref")
}

func TestValidate_ParamReferences_OtherOperationsParamIsRejected(t *testing.T) {
	t.Parallel()
	get := paramRefOp("op/x/get", "param/x/get/p", "param/x/get/p", "param/x/get/p", "param/x/get/p")
	post := paramRefOp("op/x/post", "param/x/post/p", "param/x/get/p", "param/x/get/p", "param/x/get/p")

	diags := validated(t, docWithOperations(get, post))

	var at []string
	for _, d := range diags {
		if d.Code == "pass/param-binding-mismatch" {
			at = append(at, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{
		"op/x/post/pagination/inputCursor", "op/x/post/pagination/inputLimit", "op/x/post/idempotency/tokenParam",
	}, at)
}

func TestValidate_ParamReferences_UndeclaredIDIsReportedOnceAsDangling(t *testing.T) {
	t.Parallel()
	op := paramRefOp("op/x/get", "param/x/get/p", "param/x/ghost", "param/x/get/p", "param/x/get/p")
	diags := validated(t, docWithOperations(op))
	assert.Equal(t, 1, countCode(t, diags, "ir/dangling-param-ref"))
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"), "the dangling walk owns an ID nothing declares")
}

// TestValidate_ParamReferences_EmptyTokenParamWithoutTokenKindIsClean pins that
// an empty token is no reference: it is how an operation that is not
// token-keyed spells having none.
func TestValidate_ParamReferences_EmptyTokenParamWithoutTokenKindIsClean(t *testing.T) {
	t.Parallel()
	op := paramRefOp("op/x/get", "param/x/get/p", "param/x/get/p", "param/x/get/p", "")
	op.Idempotency = ir.Idempotency{Kind: ir.IdempotencySafe}
	diags := validated(t, docWithOperations(op))
	assert.Zero(t, countCode(t, diags, "pass/param-binding-mismatch"))
	assert.Zero(t, countCode(t, diags, "ir/dangling-param-ref"))
}

// TestValidate_ParamReferences_EmptyPaginationInputIsRejected pins that a
// pagination input naming no parameter is reported here, as an empty binding
// is: the dangling walk skips an empty ID.
func TestValidate_ParamReferences_EmptyPaginationInputIsRejected(t *testing.T) {
	t.Parallel()
	op := paramRefOp("op/x/get", "param/x/get/p", "", "param/x/get/p", "param/x/get/p")
	var at []string
	for _, d := range validated(t, docWithOperations(op)) {
		if d.Code == "pass/param-binding-mismatch" {
			at = append(at, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"op/x/get/pagination/inputCursor"}, at)
}

// TestValidate_ParamReferences_FieldArgumentIsRejected pins that a binding or a
// pagination input naming a field argument is reported. The argument is a
// declared parameter, so the dangling walk resolves the reference and only this
// check can say it is no parameter of the operation.
func TestValidate_ParamReferences_FieldArgumentIsRejected(t *testing.T) {
	t.Parallel()
	const arg = ir.ParamID("param/x/m/a/limit")
	op := paramBindingOp("op/x/get", ir.Parameter{ID: "param/x/get/id/query", Name: ir.Naming{Source: "id"}})
	op.Bindings.HTTP[0].ParamBindings = append(op.Bindings.HTTP[0].ParamBindings,
		ir.HTTPParamBinding{Param: arg, Location: ir.HTTPLocationQuery})
	op.Pagination = &ir.Pagination{InputLimit: &ir.ParamPath{Param: arg}}
	doc := docWithOperations(op)
	model, ok := doc.Types["t/m"].(*ir.Model)
	require.True(t, ok)
	model.Properties[0].Args = []ir.Parameter{{ID: arg, Name: ir.Naming{Source: "limit"}, Type: ir.TypeRef{Target: "t/prim/string"}}}

	diags := validated(t, doc)
	var at []string
	for _, d := range diags {
		if d.Code == "pass/param-binding-mismatch" {
			at = append(at, d.Provenance.Node)
		}
	}
	assert.Equal(t, []string{"op/x/get/bindings/http/0", "op/x/get/pagination/inputLimit"}, at)
	assert.Zero(t, countCode(t, diags, "ir/dangling-param-ref"), "the argument is declared")
}
