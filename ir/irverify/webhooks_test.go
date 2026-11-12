// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_WebhookRouteIsInTheFieldItsFlagSelects drives checkWebhookRoutes
// over every pairing of Inbound with the two route fields. A webhook naming an
// event or none, and another binding carrying a template or none, are clean;
// the field Inbound does not select is a violation at that field.
func TestVerify_WebhookRouteIsInTheFieldItsFlagSelects(t *testing.T) {
	t.Parallel()
	const at = "doc.Services[0].Groups[0].Operations[0].Bindings.HTTP[0]"
	for _, tc := range []struct {
		name    string
		binding ir.HTTPBinding
		want    []string
	}{
		{"a webhook naming its event", ir.HTTPBinding{Inbound: ir.HTTPInboundWebhook, WebhookName: "{tenant}.created"}, nil},
		// An empty webhooks-map key is legal, and the binding it lowers to names
		// nothing.
		{"a webhook with an empty key", ir.HTTPBinding{Inbound: ir.HTTPInboundWebhook}, nil},
		{"a path binding with its template", ir.HTTPBinding{URITemplate: "/pets"}, nil},
		{"a binding with neither", ir.HTTPBinding{}, nil},
		{"a webhook with a template", ir.HTTPBinding{Inbound: ir.HTTPInboundWebhook, URITemplate: "{tenant}.created"},
			[]string{at + ".URITemplate"}},
		{"a webhook with a template beside its name",
			ir.HTTPBinding{Inbound: ir.HTTPInboundWebhook, URITemplate: "/pets", WebhookName: "newPet"},
			[]string{at + ".URITemplate"}},
		{"a path binding with a webhook name", ir.HTTPBinding{URITemplate: "/pets", WebhookName: "newPet"},
			[]string{at + ".WebhookName"}},
		{"a webhook name on an unmarked binding", ir.HTTPBinding{WebhookName: "newPet"},
			[]string{at + ".WebhookName"}},
		{"a callback naming its expression", ir.HTTPBinding{Inbound: ir.HTTPInboundCallback, URITemplate: "{$url}"}, nil},
		{"a webhook name on a callback", ir.HTTPBinding{Inbound: ir.HTTPInboundCallback, WebhookName: "newPet"},
			[]string{at + ".WebhookName"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op := ir.Operation{ID: "op/x/S/op", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{tc.binding}}}
			var got []string
			for _, v := range irverify.Verify(opDoc(op)) {
				if v.Code == "ir/webhook-route" {
					got = append(got, v.Path)
				}
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestVerify_UndeclaredInboundKindIsAViolation holds Inbound to the kinds ir
// declares, at the field, and reports nothing else about such a binding.
func TestVerify_UndeclaredInboundKindIsAViolation(t *testing.T) {
	t.Parallel()
	const at = "doc.Services[0].Groups[0].Operations[0].Bindings.HTTP[0]"
	for _, tc := range []struct {
		name    string
		inbound ir.HTTPInbound
		want    []string
	}{
		{"an invented kind", "push", []string{at + ".Inbound"}},
		{"a re-cased kind", "Webhook", []string{at + ".Inbound"}},
		{"a webhook", ir.HTTPInboundWebhook, nil},
		{"a callback", ir.HTTPInboundCallback, nil},
		{"none", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op := ir.Operation{ID: "op/x/S/op", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{Inbound: tc.inbound}}}}
			assert.Equal(t, tc.want, violationPaths(irverify.Verify(opDoc(op)), "ir/unknown-http-inbound"))
		})
	}
}

// TestVerify_ACallbacksTwoHalvesAgree pins ir/callback-unlisted and
// ir/callback-unmarked: an operation marked a callback that no binding lists,
// and one listed that is not marked, are reported at the marked operation's
// bindings and at the entry listing it, alone or side by side, while a listed
// and marked callback, a webhook and a listed ID naming no operation are not.
func TestVerify_ACallbacksTwoHalvesAgree(t *testing.T) {
	t.Parallel()
	parent := func(listed ...ir.OpID) ir.Operation {
		return ir.Operation{ID: "op/x/S/parent", Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
			URITemplate: "/sub", Callbacks: []ir.Callback{{Name: "on", Expression: "{$url}", Operations: listed}},
		}}}}
	}
	callback := func(id ir.OpID, inbound ir.HTTPInbound) ir.Operation {
		return ir.Operation{ID: id, Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{Inbound: inbound, URITemplate: "{$url}"}}}}
	}
	const second = "doc.Services[0].Groups[0].Operations[1].Bindings.HTTP"
	const third = "doc.Services[0].Groups[0].Operations[2].Bindings.HTTP"
	const entry = "doc.Services[0].Groups[0].Operations[0].Bindings.HTTP[0].Callbacks[0].Operations[0]"
	for _, tc := range []struct {
		name               string
		ops                []ir.Operation
		unlisted, unmarked []string
	}{
		{"listed and marked", []ir.Operation{parent("op/x/S/cb"), callback("op/x/S/cb", ir.HTTPInboundCallback)}, nil, nil},
		{"marked and listed by none", []ir.Operation{parent(), callback("op/x/S/cb", ir.HTTPInboundCallback)},
			[]string{second}, nil},
		{"listed and not marked", []ir.Operation{parent("op/x/S/cb"), callback("op/x/S/cb", "")}, nil, []string{entry}},
		{"listed and marked a webhook", []ir.Operation{parent("op/x/S/cb"), callback("op/x/S/cb", ir.HTTPInboundWebhook)},
			nil, []string{entry}},
		{"a webhook nobody lists", []ir.Operation{parent(), callback("op/x/S/hook", ir.HTTPInboundWebhook)}, nil, nil},
		{"listed and reached by no walk", []ir.Operation{parent("op/x/S/ghost")}, nil, nil},
		// As many marked as listed, though neither is the other.
		{"one marked and unlisted beside one listed and unmarked",
			[]ir.Operation{parent("op/x/S/cb"), callback("op/x/S/cb", ""), callback("op/x/S/other", ir.HTTPInboundCallback)},
			[]string{third}, []string{entry}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := opDoc(tc.ops[0])
			doc.Services[0].Groups[0].Operations = tc.ops
			vs := irverify.Verify(doc)
			assert.Equal(t, tc.unlisted, violationPaths(vs, "ir/callback-unlisted"))
			assert.Equal(t, tc.unmarked, violationPaths(vs, "ir/callback-unmarked"))
		})
	}
}
