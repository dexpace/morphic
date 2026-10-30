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
// over every pairing of IsWebhook with the two route fields. A webhook naming
// an event or none, and another binding carrying a template or none, are
// clean; the field IsWebhook does not select is a violation at that field.
func TestVerify_WebhookRouteIsInTheFieldItsFlagSelects(t *testing.T) {
	t.Parallel()
	const at = "doc.Services[0].Groups[0].Operations[0].Bindings.HTTP[0]"
	for _, tc := range []struct {
		name    string
		binding ir.HTTPBinding
		want    []string
	}{
		{"a webhook naming its event", ir.HTTPBinding{IsWebhook: true, WebhookName: "{tenant}.created"}, nil},
		// An empty webhooks-map key is legal, and the binding it lowers to names
		// nothing.
		{"a webhook with an empty key", ir.HTTPBinding{IsWebhook: true}, nil},
		{"a path binding with its template", ir.HTTPBinding{URITemplate: "/pets"}, nil},
		{"a binding with neither", ir.HTTPBinding{}, nil},
		{"a webhook with a template", ir.HTTPBinding{IsWebhook: true, URITemplate: "{tenant}.created"},
			[]string{at + ".URITemplate"}},
		{"a webhook with a template beside its name",
			ir.HTTPBinding{IsWebhook: true, URITemplate: "/pets", WebhookName: "newPet"},
			[]string{at + ".URITemplate"}},
		{"a path binding with a webhook name", ir.HTTPBinding{URITemplate: "/pets", WebhookName: "newPet"},
			[]string{at + ".WebhookName"}},
		{"a webhook name on an unmarked binding", ir.HTTPBinding{WebhookName: "newPet"},
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
