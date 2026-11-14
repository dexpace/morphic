// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/convention"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// keyed is an operation bound by method, declaring a parameter for each of
// bindings, its location and wire name, under an ID of its own.
func keyed(method string, bindings ...ir.HTTPParamBinding) ir.Operation {
	op := ir.Operation{ID: "op/a", Provenance: ir.Provenance{Inferred: "extension-promotion"}}
	hb := ir.HTTPBinding{Method: method, URITemplate: "/a"}
	for i, b := range bindings {
		b.Param = ir.ParamID("param/" + string(rune('a'+i)))
		op.Params = append(op.Params, ir.Parameter{ID: b.Param, Name: ir.NamingFor(b.WireName)})
		hb.ParamBindings = append(hb.ParamBindings, b)
	}
	op.Bindings = ir.OpBindings{HTTP: []ir.HTTPBinding{hb}}
	return op
}

// header is a binding of a header parameter under name.
func header(name string) ir.HTTPParamBinding {
	return ir.HTTPParamBinding{Location: ir.HTTPLocationHeader, WireName: name}
}

// TestIdempotency_TheKeyHeaderIsTheToken pins which operations take their
// Idempotency-Key header as their token, marked beside what they carry: one
// sent by a method RFC 9110 does not define as safe, binding exactly one header
// of that name in any case, and stating no idempotency of its own, a webhook's
// and a callback's among them. A safe method, a key outside a header and a
// stated idempotency leave the header unread, unreported.
func TestIdempotency_TheKeyHeaderIsTheToken(t *testing.T) {
	t.Parallel()
	declared := keyed(http.MethodPost, header("Idempotency-Key"))
	declared.Idempotency = ir.Idempotency{Kind: ir.IdempotencySafe}
	twice := keyed(http.MethodPost, header("Idempotency-Key"))
	twice.Bindings.HTTP = append(twice.Bindings.HTTP, twice.Bindings.HTTP[0])
	hook := keyed(http.MethodPost, header("Idempotency-Key"))
	hook.Bindings.HTTP[0].URITemplate, hook.Bindings.HTTP[0].WebhookName = "", "paid"
	callback := keyed(http.MethodPost, header("Idempotency-Key"))
	callback.Bindings.HTTP[0].URITemplate = "{$request.body#/url}"
	for _, tc := range []struct {
		name  string
		op    ir.Operation
		token ir.ParamID
	}{
		{"a POST", keyed(http.MethodPost, header("Idempotency-Key")), "param/a"},
		{"a PATCH", keyed(http.MethodPatch, header("Idempotency-Key")), "param/a"},
		{"a PUT", keyed(http.MethodPut, header("Idempotency-Key")), "param/a"},
		{"a DELETE", keyed(http.MethodDelete, header("Idempotency-Key")), "param/a"},
		{"a webhook's POST", hook, "param/a"},
		{"a callback's POST", callback, "param/a"},
		{"the name in lower case, after another header", keyed(http.MethodPost, header("X-Trace"), header("idempotency-key")),
			"param/b"},
		{"a GET", keyed(http.MethodGet, header("Idempotency-Key")), ""},
		{"a HEAD", keyed(http.MethodHead, header("Idempotency-Key")), ""},
		{"an OPTIONS", keyed(http.MethodOptions, header("Idempotency-Key")), ""},
		{"a TRACE", keyed(http.MethodTrace, header("Idempotency-Key")), ""},
		{"a QUERY", keyed("QUERY", header("Idempotency-Key")), ""},
		{"a query parameter of the name", keyed(http.MethodPost,
			ir.HTTPParamBinding{Location: ir.HTTPLocationQuery, WireName: "Idempotency-Key"}), ""},
		{"another header", keyed(http.MethodPost, header("X-Idempotency-Key")), ""},
		{"a declared idempotency", declared, ""},
		{"two bindings", twice, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := oneOperation(tc.op, registry(nil))
			assert.Empty(t, convention.Apply(under(lowering.Policy{}), lowering.NewWhole(doc)))
			op := operationOf(doc)
			if tc.token == "" {
				assert.Equal(t, tc.op.Idempotency, op.Idempotency, "the idempotency stays as it was")
				assert.Equal(t, ir.Inferred("extension-promotion"), op.Provenance.Inferred)
				return
			}
			assert.Equal(t, ir.Idempotency{Kind: ir.IdempotencyToken, TokenParam: tc.token}, op.Idempotency)
			assert.Equal(t, ir.Inferred("extension-promotion,idempotency-inference"), op.Provenance.Inferred)
		})
	}
}

// TestIdempotency_ReportsWhatItCannotSettle pins the reports: an operation
// binding the Idempotency-Key header that takes no token, for want of a method
// whose safety is known or of one such header, is reported once at the
// operation, saying why, and keeps its idempotency and marks. One binding no
// such header, sent by a safe method, or stating its idempotency is not.
func TestIdempotency_ReportsWhatItCannotSettle(t *testing.T) {
	t.Parallel()
	const unknown = " is no method a path item's fixed fields declare, so whether it is safe is unknown"
	declared := keyed("LINK", header("Idempotency-Key"))
	declared.Idempotency = ir.Idempotency{Kind: ir.IdempotencyIdempotent}
	for _, tc := range []struct {
		name string
		op   ir.Operation
		why  string
	}{
		{"a method only additionalOperations declares", keyed("LINK", header("Idempotency-Key")), "LINK" + unknown},
		{"a lower-case method", keyed("post", header("Idempotency-Key")), "post" + unknown},
		{"two headers whose names differ in case", keyed(http.MethodPost, header("Idempotency-Key"),
			header("idempotency-key")), "2 header parameters are named Idempotency-Key, compared without case"},
		{"an unknown method binding the header twice", keyed("LINK", header("Idempotency-Key"),
			header("IDEMPOTENCY-KEY")), "LINK" + unknown},
		{"an unknown method binding no such header", keyed("LINK", header("X-Idempotency-Key")), ""},
		{"a safe method binding the header twice", keyed(http.MethodGet, header("Idempotency-Key"),
			header("idempotency-key")), ""},
		{"an unknown method stating its idempotency", declared, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.op.Provenance.Pointer = "/paths/~1a/link"
			doc := oneOperation(tc.op, registry(nil))
			diags := convention.Apply(under(lowering.Policy{}), lowering.NewWhole(doc))
			assert.Equal(t, tc.op.Idempotency, operationOf(doc).Idempotency, "the idempotency stays as it was")
			assert.Equal(t, ir.Inferred("extension-promotion"), operationOf(doc).Provenance.Inferred, "nothing is marked")
			if tc.why == "" {
				assert.Empty(t, diags)
				return
			}
			assert.Equal(t, []ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "openapi/undecided-idempotency",
				Message: "no idempotency token is inferred: " + tc.why, Provenance: ir.Provenance{Pointer: "/paths/~1a/link"}}},
				diags)
		})
	}
}

// TestIdempotency_DisabledReadsNoToken pins the switch: with the policy off,
// an operation the policy would name a token for keeps the idempotency the
// document states, and no mark, and one it would report is not reported.
func TestIdempotency_DisabledReadsNoToken(t *testing.T) {
	t.Parallel()
	off := under(lowering.Policy{IdempotencyInference: lowering.IdempotencyInference{Disabled: true}})
	for _, op := range []ir.Operation{keyed(http.MethodPost, header("Idempotency-Key")),
		keyed("LINK", header("Idempotency-Key"))} {
		doc := oneOperation(op, registry(nil))
		assert.Empty(t, convention.Apply(off, lowering.NewWhole(doc)), op.Bindings.HTTP[0].Method)
		assert.Equal(t, ir.Idempotency{}, operationOf(doc).Idempotency)
		assert.Equal(t, ir.Inferred("extension-promotion"), operationOf(doc).Provenance.Inferred)
	}
}
