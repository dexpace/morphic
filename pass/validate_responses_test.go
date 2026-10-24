package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/pass"
)

func TestValidate_OneWayWithResponses(t *testing.T) {
	t.Parallel()
	op := ir.Operation{ID: "op", OneWay: true, Responses: []ir.Response{{}}}
	diags := pass.Validate(docWithOperation(op))
	require.NotEmpty(t, diags)
	assert.Contains(t, irtest.DiagCodes(diags), "pass/oneway-with-responses")
}

// catchAll is the one condition an operation's Default may hold.
func catchAll() ir.ResponseConditions {
	return ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 0, To: 0}}}
}

// defaultResponse is a Default answering the catch-all, with no body.
func defaultResponse() *ir.Response {
	return &ir.Response{Name: ir.Naming{Source: "default"}, Conditions: catchAll()}
}

// TestValidate_AOneWayOperationWithADefaultIsReported pins that a Default is
// counted among the responses a one-way operation may not declare: it is a
// response whichever class the plan gives it (ir-design §7.2). Counting only
// Responses would let a one-way operation carry a Default unreported.
func TestValidate_AOneWayOperationWithADefaultIsReported(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		op   ir.Operation
		want string
	}{
		{"a Default alone", ir.Operation{ID: "op", OneWay: true, Default: defaultResponse()},
			"operation op is one-way but declares 1 response(s)"},
		{"a Response and a Default", ir.Operation{
			ID: "op", OneWay: true, Responses: []ir.Response{{}}, Default: defaultResponse(),
		}, "operation op is one-way but declares 2 response(s)"},
		{"neither", ir.Operation{ID: "op", OneWay: true}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			found := withCode(pass.Validate(docWithOperation(tc.op)), "pass/oneway-with-responses")
			if tc.want == "" {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1)
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Equal(t, "op", found[0].Provenance.Node)
			assert.Equal(t, tc.want, found[0].Message)
		})
	}
}

// TestValidate_ADefaultAnswersTheCatchAllAlone pins the one condition a Default
// may hold: the zero range, alone. A Default that names a status is a Response
// or an ErrorCase by the protocol's own classes, and one with no condition or an
// extra range answers something other than every unnamed outcome.
func TestValidate_ADefaultAnswersTheCatchAllAlone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		conditions ir.ResponseConditions
		want       string
	}{
		{"the catch-all alone", catchAll(), ""},
		{"a named status", ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 200, To: 200}}},
			"default response at op/default answers 200, where a default answers the catch-all alone"},
		{"a range", ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 500, To: 599}}},
			"default response at op/default answers 500-599, where a default answers the catch-all alone"},
		{"no condition", ir.ResponseConditions{},
			"default response at op/default answers no status, where a default answers the catch-all alone"},
		{"the catch-all beside a status", ir.ResponseConditions{
			StatusCodes: []ir.StatusRange{{From: 0, To: 0}, {From: 404, To: 404}},
		}, "default response at op/default answers the catch-all and 404, where a default answers the catch-all alone"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op := ir.Operation{ID: "op", Default: &ir.Response{Conditions: tc.conditions}}
			found := withCode(pass.Validate(docWithOperation(op)), "ir/default-response-conditions")
			if tc.want == "" {
				assert.Empty(t, found)
				return
			}
			require.Len(t, found, 1, "the Default is reported once")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Equal(t, "op/default", found[0].Provenance.Node)
			assert.Equal(t, tc.want, found[0].Message)
		})
	}
}

// TestValidate_AnOperationWithoutADefaultHasNoDefaultConditionsToJudge pins that
// the check is silent on an operation declaring no Default, whose absent
// response has no conditions to hold to the catch-all.
func TestValidate_AnOperationWithoutADefaultHasNoDefaultConditionsToJudge(t *testing.T) {
	t.Parallel()
	op := ir.Operation{ID: "op", Responses: []ir.Response{{
		Conditions: ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: 200, To: 200}}},
	}}}
	assert.Empty(t, withCode(pass.Validate(docWithOperation(op)), "ir/default-response-conditions"))
}
