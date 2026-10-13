package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var httpBindingType = reflect.TypeFor[ir.HTTPBinding]()

// checkWebhookRoutes asserts every ir.HTTPBinding names its route in the field
// IsWebhook selects: a webhook its event in WebhookName, any other binding its
// template in URITemplate. A binding filling the other field gives a consumer
// two answers to whether it is a webhook, as a stray Value payload does to what
// the value is.
//
// Neither field is required. An empty webhooks-map key is legal and lowers to
// no name, so requiring one would report a document the compiler produces.
//
// The walk reaches bindings rather than the fields that carry them, so a new
// carrier is held at once.
func checkWebhookRoutes(doc *ir.Document, _ declarations) ([]Violation, bool) {
	var vs []Violation
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Type() != httpBindingType {
			return true
		}
		if b, ok := reflect.TypeAssert[ir.HTTPBinding](v); ok {
			vs = appendStrayRoute(vs, b, path)
		}
		return true
	})
	return vs, truncated
}

// appendStrayRoute reports the route field b fills that its IsWebhook does not
// select. The two cases exclude each other, so a binding draws one at most.
func appendStrayRoute(vs []Violation, b ir.HTTPBinding, path string) []Violation {
	switch {
	case b.IsWebhook && b.URITemplate != "":
		return append(vs, Violation{
			Code: "ir/webhook-route",
			Message: "webhook binding carries URI template " + strconv.Quote(b.URITemplate) +
				"; a webhook names its event in WebhookName and has no template",
			Path: path + ".URITemplate",
		})
	case !b.IsWebhook && b.WebhookName != "":
		return append(vs, Violation{
			Code: "ir/webhook-route",
			Message: "binding that is not a webhook carries webhook name " + strconv.Quote(b.WebhookName) +
				"; only a webhook binding names an event",
			Path: path + ".WebhookName",
		})
	}
	return vs
}
