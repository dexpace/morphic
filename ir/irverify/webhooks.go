// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"maps"
	"reflect"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var (
	httpBindingType = reflect.TypeFor[ir.HTTPBinding]()
	opIDType        = reflect.TypeFor[ir.OpID]()
)

// checkWebhookRoutes asserts every ir.HTTPBinding names its route in the field
// Inbound selects, a webhook its event in WebhookName and any other binding its
// template in URITemplate, and that Inbound is a kind ir declares. A binding
// filling the other field gives two answers to whether it is a webhook.
//
// Neither field is required: an empty webhooks-map key is legal and lowers to
// no name. The walk reaches bindings rather than the fields carrying them, so a
// new carrier is held at once.
func checkWebhookRoutes(*ir.Document) walkCheck {
	var vs []Violation
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() != httpBindingType {
				return true
			}
			if b, ok := reflect.TypeAssert[ir.HTTPBinding](v); ok {
				vs = appendStrayRoute(vs, b, at)
			}
			return true
		},
		report: reporting(&vs),
	}
}

// appendStrayRoute reports the route field b fills that its Inbound does not
// select, or an Inbound ir does not declare. The cases exclude each other, so a
// binding draws one at most.
func appendStrayRoute(vs []Violation, b ir.HTTPBinding, at ir.WalkPath) []Violation {
	webhook := b.Inbound == ir.HTTPInboundWebhook
	switch {
	case b.Inbound != "" && !webhook && b.Inbound != ir.HTTPInboundCallback:
		return append(vs, Violation{
			Code:    "ir/unknown-http-inbound",
			Message: "binding carries undeclared inbound kind " + strconv.Quote(string(b.Inbound)),
			Path:    at.String() + ".Inbound",
		})
	case webhook && b.URITemplate != "":
		return append(vs, Violation{
			Code: "ir/webhook-route",
			Message: "webhook binding carries URI template " + strconv.Quote(b.URITemplate) +
				"; a webhook names its event in WebhookName and has no template",
			Path: at.String() + ".URITemplate",
		})
	case !webhook && b.WebhookName != "":
		return append(vs, Violation{
			Code: "ir/webhook-route",
			Message: "binding that is not a webhook carries webhook name " + strconv.Quote(b.WebhookName) +
				"; only a webhook binding names an event",
			Path: at.String() + ".WebhookName",
		})
	}
	return vs
}

// checkCallbacks asserts the two halves of a callback agree: every operation an
// HTTP binding lists among its Callbacks is marked a callback, and every one
// marked is listed (docs/ir/http-binding.md). Either alone is how a consumer
// finds a callback, so disagreeing halves give two answers to who sends the
// request. A marked operation is reported at its bindings, a listed one at the
// entry listing it, and a listed ID naming no operation is left to the
// reference checks. It spells a path only for those two, so a document with no
// callbacks costs it nothing.
func checkCallbacks(*ir.Document) walkCheck {
	marked := map[ir.OpID]string{}
	listed := map[ir.OpID]string{}
	return walkCheck{
		visit: func(v reflect.Value, path ir.WalkPath) bool {
			if v.Type() != operationType {
				return true
			}
			op, ok := reflect.TypeAssert[ir.Operation](v)
			if !ok {
				return true
			}
			for i, b := range op.Bindings.HTTP {
				if _, seen := marked[op.ID]; b.Inbound == ir.HTTPInboundCallback && !seen {
					marked[op.ID] = path.String() + ".Bindings.HTTP"
				}
				for j, cb := range b.Callbacks {
					for k, id := range cb.Operations {
						if _, seen := listed[id]; !seen {
							listed[id] = path.String() + ".Bindings.HTTP[" + strconv.Itoa(i) + "].Callbacks[" +
								strconv.Itoa(j) + "].Operations[" + strconv.Itoa(k) + "]"
						}
					}
				}
			}
			return true
		},
		report: func(d declarations) []Violation { return callbackDisagreements(d, marked, listed) },
	}
}

// callbackDisagreements reports each operation marked a callback that no
// binding lists, then each listed operation the walk declared that is not
// marked, each in byte order of its ID.
func callbackDisagreements(d declarations, marked, listed map[ir.OpID]string) []Violation {
	var vs []Violation
	for _, id := range slices.Sorted(maps.Keys(marked)) {
		if _, ok := listed[id]; !ok {
			vs = append(vs, Violation{
				Code:    "ir/callback-unlisted",
				Message: "operation " + strconv.Quote(string(id)) + " is marked a callback, but no binding lists it among its Callbacks",
				Path:    marked[id],
			})
		}
	}
	if len(listed) == len(marked) && len(vs) == 0 {
		return vs
	}
	held := map[ir.OpID]bool{}
	for _, decl := range d.list() {
		if decl.Class == opIDType {
			held[ir.OpID(decl.ID)] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(listed)) {
		if _, ok := marked[id]; !ok && held[id] {
			vs = append(vs, Violation{
				Code:    "ir/callback-unmarked",
				Message: "a binding lists operation " + strconv.Quote(string(id)) + " among its Callbacks, but it is not marked a callback",
				Path:    listed[id],
			})
		}
	}
	return vs
}
