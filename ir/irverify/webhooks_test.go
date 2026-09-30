package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// bindingDoc wraps b as one webhook operation's HTTP binding in an otherwise
// sound document — one service, one named group, one named operation — so
// nothing but b is open to question.
func bindingDoc(b ir.HTTPBinding) *ir.Document {
	return &ir.Document{
		IRVersion: ir.IRVersion,
		Services: []ir.Service{{
			ID:   "s/x/S",
			Name: named("s"),
			Groups: []ir.OperationGroup{{
				Name: named("webhooks"),
				Operations: []ir.Operation{{
					ID:       "op/x/S/hook",
					Name:     named("hook"),
					Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{b}},
				}},
			}},
		}},
	}
}

// bindingPath is where bindingDoc's one binding sits, before the field segment
// a violation names.
const bindingPath = "doc.Services[0].Groups[0].Operations[0].Bindings.HTTP[0]"

// TestVerify_WebhookBindingIsClean pins the negative half of
// ir/webhook-uri-template: a webhook binding carries its event name in
// WebhookName and leaves URITemplate empty, and a non-webhook binding carries a
// template and no name. Both must verify clean, or the check would fire on
// every document this compiler produces.
func TestVerify_WebhookBindingIsClean(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		binding ir.HTTPBinding
	}{
		{"webhook names its event", ir.HTTPBinding{Method: "POST", IsWebhook: true, WebhookName: "newPet"}},
		// An empty webhooks-map key is a legal OpenAPI document, so the binding
		// it lowers to carries no name at all and must still be clean.
		{"webhook with an empty key", ir.HTTPBinding{Method: "POST", IsWebhook: true}},
		{"path operation carries its template", ir.HTTPBinding{Method: "POST", URITemplate: "/pets"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, irverify.Verify(bindingDoc(tc.binding)))
		})
	}
}

// TestVerify_WebhookURITemplateIsAViolation plants the defect the check exists
// for: an IsWebhook binding populating URITemplate, the field a webhook does not
// select. It is the planted case the union fixture in ir's own bindings_test.go
// is not — that one is a deliberate union of every field, and this is what says
// the pair is wrong in a document.
func TestVerify_WebhookURITemplateIsAViolation(t *testing.T) {
	t.Parallel()
	got := irverify.Verify(bindingDoc(ir.HTTPBinding{
		Method:      "POST",
		URITemplate: "{tenant}.created",
		IsWebhook:   true,
		WebhookName: "newPet",
	}))
	require.Equal(t, []string{"ir/webhook-uri-template"}, codesOf(got))
	assert.Equal(t, bindingPath+".URITemplate", got[0].Path)
}
