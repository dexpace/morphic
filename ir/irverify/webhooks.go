package irverify

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

var httpBindingType = reflect.TypeFor[ir.HTTPBinding]()

// checkWebhookBindings asserts an ir.HTTPBinding marked IsWebhook carries no
// URITemplate: a webhook's event name belongs in WebhookName, and the field a
// webhook does not select is empty on one the compiler produced.
//
// It is the discipline checkValues holds for ir.Value, at the one position
// HTTPBinding makes a field selection on: a discriminator (there Kind, here
// IsWebhook) selects which field carries meaning, and a document populating
// another leaves a consumer two answers and no way to tell which to trust.
//
// Only the negative half is a contract. An empty webhooks-map key is legal — an
// OpenAPI document may declare `webhooks: {"": …}` — so "a webhook binding names
// its event" is not universal and a non-empty WebhookName requirement would fire
// on a document this compiler itself produces. The converse, that a binding
// which is not a webhook carries no WebhookName, is left to the corpus goldens.
//
// Nothing else holds the rule. A document decoded from JSON, written by a
// foreign compiler, or rewritten by a pass is beyond what the compiler's own
// tests reach, and every producer has to agree on the contract regardless of
// which one wrote the file — which is what makes this irverify's business.
//
// Bindings are reached through the walk rather than through the fields that
// carry them — Operation.Bindings, from any node holding one — so a new carrier
// is held the moment it exists. The walk continues below each binding, since it
// holds callbacks and unmodeled entries of its own.
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
