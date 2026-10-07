package irverify

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

var httpBindingType = reflect.TypeFor[ir.HTTPBinding]()

// checkWebhookBindings asserts an ir.HTTPBinding marked IsWebhook carries no
// URITemplate: a webhook's event name belongs in WebhookName, and populating the
// field a webhook does not select leaves a consumer two answers, the discipline
// checkValues holds for ir.Value.
//
// Only that negative half is a contract. An empty webhooks-map key is legal, so
// requiring a non-empty WebhookName would fire on a document the compiler itself
// produces; the converse is left to the corpus goldens.
//
// The walk reaches bindings rather than the fields that carry them, so a new
// carrier is held at once.
func checkWebhookBindings(doc *ir.Document, _ declarations) ([]Violation, bool) {
	var vs []Violation
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Type() != httpBindingType {
			return true
		}
		if v.FieldByName("IsWebhook").Bool() && v.FieldByName("URITemplate").String() != "" {
			vs = append(vs, Violation{
				Code: "ir/webhook-uri-template",
				Message: "webhook binding carries a URI template, a field a webhook does not " +
					"select; a webhook's event name belongs in WebhookName",
				Path: path + ".URITemplate",
			})
		}
		return true
	})
	return vs, truncated
}
