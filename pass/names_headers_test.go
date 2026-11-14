// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// headerOf is a header spelled name.
func headerOf(id, name string) ir.Header {
	return ir.Header{ID: ir.PropID(id), Name: named(name)}
}

// headers are the two spellings of one header the wire reads as one.
func headers(a, b string) []ir.Header { return []ir.Header{headerOf("h/"+a, a), headerOf("h/"+b, b)} }

// inOperation is a document whose one operation, op/x, is op.
func inOperation(op ir.Operation) *ir.Document {
	op.ID = "op/x"
	return inGroup(ir.OperationGroup{ID: "g", Operations: []ir.Operation{op}})
}

// TestNames_ReadsTheHeadersOfAResponseAndAnErrorCaseAsAScope holds each place
// headers sit in a response or an error to a scope of its own, reported at the
// operation or the service that holds it and naming the response or error
// the pair sits in, by the first of its source, canonical and hint spellings,
// and "" where there is none. The same headers in two responses are no
// collision.
func TestNames_ReadsTheHeadersOfAResponseAndAnErrorCaseAsAScope(t *testing.T) {
	t.Parallel()
	const pair = `headers 0 ("X-Rate-Limit") and 1 ("X_Rate_Limit") share the neutral name "x_rate_limit"`
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []ir.Diagnostic
	}{
		{"a response's and an error's headers", inOperation(ir.Operation{
			Responses: []ir.Response{{Name: hinted("200"), Headers: headers("X-Rate-Limit", "X_Rate_Limit")}},
			Errors:    []ir.ErrorCase{{Name: hinted("429"), Headers: headers("Retry-After", "retry_after")}},
		}), []ir.Diagnostic{
			collision("op/x", `in operation op/x, response "200", `+pair),
			collision("op/x", `in operation op/x, error "429", headers 0 ("Retry-After") and 1 ("retry_after") share the neutral name "retry_after"`),
		}},
		{"the default response's headers", inOperation(ir.Operation{
			Default: &ir.Response{Headers: headers("X-Rate-Limit", "X_Rate_Limit")},
		}), []ir.Diagnostic{collision("op/x", `in operation op/x, the default response, `+pair)}},
		{"a service's common error's headers", &ir.Document{Services: []ir.Service{{
			ID: "s/x", CommonErrors: []ir.ErrorCase{{Name: named("Throttled"), Headers: headers("X-Rate-Limit", "X_Rate_Limit")}},
		}}}, []ir.Diagnostic{collision("s/x", `in service s/x, common error "Throttled", `+pair)}},
		{"a response named by nothing", inOperation(ir.Operation{
			Responses: []ir.Response{{Headers: headers("X-Rate-Limit", "X_Rate_Limit")}},
		}), []ir.Diagnostic{collision("op/x", `in operation op/x, response "", `+pair)}},
		{"a response named by its canonical spelling", inOperation(ir.Operation{
			Responses: []ir.Response{{Name: ir.Naming{Canonical: "ok"}, Headers: headers("X-Rate-Limit", "X_Rate_Limit")}},
		}), []ir.Diagnostic{collision("op/x", `in operation op/x, response "ok", `+pair)}},
		// The valid twins. Headers are compared as properties are, by their words as
		// they are, so two whose words break apart differ.
		{"headers whose words break apart", inOperation(ir.Operation{
			Responses: []ir.Response{{Name: hinted("200"), Headers: headers("Custom-Header", "customheader")}},
		}), nil},
		{"the same headers in two responses", inOperation(ir.Operation{Responses: []ir.Response{
			{Name: hinted("200"), Headers: []ir.Header{headerOf("h/a", "X-Rate-Limit")}},
			{Name: hinted("201"), Headers: []ir.Header{headerOf("h/b", "X_Rate_Limit")}},
		}}), nil},
		{"two headers of different names", inOperation(ir.Operation{
			Responses: []ir.Response{{Name: hinted("200"), Headers: headers("X-Rate", "X-Rates")}},
		}), nil},
		{"the same headers in a response and an error", inOperation(ir.Operation{
			Responses: []ir.Response{{Name: hinted("200"), Headers: []ir.Header{headerOf("h/a", "X-Rate-Limit")}}},
			Errors:    []ir.ErrorCase{{Name: hinted("429"), Headers: []ir.Header{headerOf("h/b", "X_Rate_Limit")}}},
		}), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for range 16 {
				if !assert.Empty(t, cmp.Diff(tc.want, namesOf(t, tc.doc))) {
					break
				}
			}
		})
	}
}

// TestNames_ReadsTheHeadersOfTwoResponsesAtTheOperationInEitherOrder holds the
// finding of a pair of headers to the operation and the code whatever order the
// responses are declared in, and to the label of the response that holds the
// pair: it is placed at the operation and no index of it.
func TestNames_ReadsTheHeadersOfTwoResponsesAtTheOperationInEitherOrder(t *testing.T) {
	t.Parallel()
	plain := func(label string) ir.Response {
		return ir.Response{Name: hinted(label), Headers: []ir.Header{headerOf("h/quota", "X-Quota")}}
	}
	holding := func(label string) ir.Response {
		return ir.Response{Name: hinted(label), Headers: headers("X-Rate-Limit", "X_Rate_Limit")}
	}
	for _, tc := range []struct {
		name      string
		responses []ir.Response
		label     string
	}{
		{"the pair's response declared second", []ir.Response{plain("200"), holding("201")}, "201"},
		{"the pair's response declared first", []ir.Response{holding("201"), plain("200")}, "201"},
		{"the labels swapped", []ir.Response{plain("201"), holding("200")}, "200"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := namesOf(t, inOperation(ir.Operation{Responses: tc.responses}))

			require.Len(t, got, 1)
			assert.Equal(t, "op/x", got[0].Provenance.Node)
			assert.Equal(t, "pass/name-collision", got[0].Code)
			assert.Contains(t, got[0].Message, `response "`+tc.label+`", headers 0`)
		})
	}
}
