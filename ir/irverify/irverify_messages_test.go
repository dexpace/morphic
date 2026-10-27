package irverify_test

import (
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// illFormed holds a byte that does not decode, so a message that interpolates
// it unquoted is ill-formed.
const illFormed = "caf\xe9"

// typesDoc returns a document holding types, keyed by their own IDs.
func typesDoc(types ...ir.TypeDef) *ir.Document {
	doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{}}
	for _, td := range types {
		doc.Types[td.Common().ID] = td
	}
	return doc
}

// namedModel returns a model at id that draws no naming violation.
func namedModel(id ir.TypeID) *ir.Model {
	return &ir.Model{ID: id, Name: named("m")}
}

// spacedDoc returns a clean one-type document declaring spaces as its own.
func spacedDoc(spaces map[string][]string) *ir.Document {
	doc := typesDoc(namedModel("t/x/A"))
	doc.IDSpaces = spaces
	return doc
}

// messageCase is one message site: a document that reaches it, the code it
// reports, and the message it must spell.
type messageCase struct {
	name string
	doc  func() *ir.Document
	code string
	want string
}

// messageCases reach every Violation message that interpolates text from the
// document, each with that text ill-formed where the check still reaches it.
func messageCases() []messageCase {
	return []messageCase{
		{"bigval not numeric", func() *ir.Document {
			m := namedModel("t/x/A")
			m.Properties = []ir.Property{{ID: "p/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: "t/x/A"},
				Default: &ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal(illFormed)}}}
			return typesDoc(m)
		}, "ir/bigval-not-numeric",
			fmt.Sprintf("numeric value %q is not a decimal literal, so it does not read back as a JSON number", illFormed)},
		{"inferred not canonical", func() *ir.Document {
			m := namedModel("t/x/A")
			m.Provenance.Inferred = illFormed + "," + illFormed
			return typesDoc(m)
		}, "ir/inferred-not-canonical", fmt.Sprintf(
			"inferred marker %q is not the list %q ir.JoinInferred writes for it", illFormed+","+illFormed, illFormed)},
		{"bigval out of range", func() *ir.Document {
			m := namedModel("t/x/A")
			m.Properties = []ir.Property{{ID: "p/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: "t/x/A"},
				Default: &ir.Value{Kind: ir.ValueNumber, Num: "1e1000000000000000000"}}}
			return typesDoc(m)
		}, "ir/bigval-out-of-range",
			`numeric value "1e1000000000000000000" is a decimal literal whose leading digit carries a power of ten ` +
				`past the ±999999999999999999 a BigVal holds`},
		{"bigval not canonical", func() *ir.Document {
			m := namedModel("t/x/A")
			m.Properties = []ir.Property{{ID: "p/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: "t/x/A"},
				Default: &ir.Value{Kind: ir.ValueNumber, Num: "01"}}}
			return typesDoc(m)
		}, "ir/bigval-not-canonical", `numeric value "01" is not the JSON form "1" ir.NewBigVal produces for it`},
		{"duplicate id", func() *ir.Document {
			op := ir.Operation{ID: ir.OpID("op/x/S/" + illFormed), Name: named("op")}
			doc := opDoc(op)
			doc.Services[0].Groups[0].Operations = append(doc.Services[0].Groups[0].Operations, op)
			return doc
		}, "ir/duplicate-op-id", fmt.Sprintf("id %q is declared here and at %q",
			"op/x/S/"+illFormed, "doc.Services[0].Groups[0].Operations[0]")},
		{"idempotency", func() *ir.Document {
			return opDoc(ir.Operation{ID: "op/x/S/op", Name: named("op"),
				Idempotency: ir.Idempotency{Kind: ir.IdempotencyKind(illFormed), TokenParam: ir.ParamID(illFormed)}})
		}, "ir/idempotency-token", fmt.Sprintf(
			"idempotency kind %q with token parameter %q: a token is named exactly when the kind is %q",
			illFormed, illFormed, ir.IdempotencyToken)},
		{"webhook template", func() *ir.Document {
			return opDoc(ir.Operation{ID: "op/x/S/op", Name: named("op"), Bindings: ir.OpBindings{
				HTTP: []ir.HTTPBinding{{IsWebhook: true, URITemplate: illFormed}}}})
		}, "ir/webhook-route", fmt.Sprintf(
			"webhook binding carries URI template %q; a webhook names its event in WebhookName and has no template",
			illFormed)},
		{"webhook name", func() *ir.Document {
			return opDoc(ir.Operation{ID: "op/x/S/op", Name: named("op"), Bindings: ir.OpBindings{
				HTTP: []ir.HTTPBinding{{WebhookName: illFormed}}}})
		}, "ir/webhook-route", fmt.Sprintf(
			"binding that is not a webhook carries webhook name %q; only a webhook binding names an event", illFormed)},
		{"prim not derived", func() *ir.Document {
			return typesDoc(&ir.Primitive{ID: ir.TypeID("t/x/" + illFormed), Prim: ir.PrimString})
		}, "ir/prim-id-not-derived", fmt.Sprintf(
			"primitive of kind \"string\" is interned at %q rather than the shared \"t/prim/string\"", "t/x/"+illFormed)},
		{"prim with no kind", func() *ir.Document {
			return typesDoc(&ir.Primitive{ID: ir.TypeID("t/x/" + illFormed)})
		}, "ir/prim-id-not-derived", fmt.Sprintf(
			"primitive at %q carries no kind, so no shared ID derives from it", "t/x/"+illFormed)},
		{"prim space reserved", func() *ir.Document {
			return typesDoc(namedModel(ir.TypeID("t/prim/" + illFormed)))
		}, "ir/prim-space-reserved", fmt.Sprintf(
			"id %q addresses the reserved primitive space but names a model", "t/prim/"+illFormed)},
		{"id malformed", func() *ir.Document {
			return typesDoc(namedModel(ir.TypeID("t//" + illFormed)))
		}, "ir/id-malformed", fmt.Sprintf(
			"id %q is not t/<space>[/<path>]; every segment must be non-empty", "t//"+illFormed)},
		{"id disagrees with its pointer", func() *ir.Document {
			m := namedModel(ir.TypeID("t/x/" + illFormed))
			m.Provenance = ir.Provenance{Pointer: "/other"}
			return typesDoc(m)
		}, "ir/id-provenance-disagreement", fmt.Sprintf(
			"id %q carries path %q, which is not the source pointer \"/other\" it records", "t/x/"+illFormed, illFormed)},
		{"namespace undeclared", func() *ir.Document {
			doc := typesDoc(namedModel(ir.TypeID("t/" + illFormed + "/A")))
			doc.IDSpaces = map[string][]string{ir.IDKindType: {"x"}}
			return doc
		}, "ir/id-space-undeclared", fmt.Sprintf(
			"id %q lives in namespace %q, which the document does not declare for kind \"t\"", "t/"+illFormed+"/A", illFormed)},
		{"kind unknown", func() *ir.Document {
			return spacedDoc(map[string][]string{ir.IDKindType: {"x"}, illFormed: {"x"}})
		}, "ir/id-spaces-unknown-kind", fmt.Sprintf(
			"id namespaces are declared for kind %q, which no id opens with", illFormed)},
		{"kind with no namespace", func() *ir.Document {
			return spacedDoc(map[string][]string{ir.IDKindType: {"x"}, ir.IDKindOp: {}})
		}, "ir/id-spaces-not-canonical", `kind "op" is declared with no namespace, and a kind with none is left out`},
		{"namespace invalid", func() *ir.Document {
			return spacedDoc(map[string][]string{ir.IDKindType: {illFormed + "/", "x"}})
		}, "ir/id-space-invalid", fmt.Sprintf(
			"namespace %q is empty or carries the id separator, so no id can be in it", illFormed+"/")},
		{"namespaces unsorted", func() *ir.Document {
			return spacedDoc(map[string][]string{ir.IDKindType: {"x", illFormed}})
		}, "ir/id-spaces-not-canonical", fmt.Sprintf(
			"namespace %q does not follow \"x\" in sorted order without repeats", illFormed)},
		{"registry key", func() *ir.Document {
			doc := typesDoc()
			doc.Types[ir.TypeID("t/x/"+illFormed)] = namedModel("t/x/A")
			return doc
		}, "ir/type-id-mismatch", fmt.Sprintf("registry key %q disagrees with node ID \"t/x/A\"", "t/x/"+illFormed)},
		{"prim kind", func() *ir.Document {
			return typesDoc(&ir.Primitive{ID: ir.PrimTypeID(ir.PrimKind(illFormed)), Prim: ir.PrimKind(illFormed)})
		}, "ir/unknown-prim-kind", fmt.Sprintf("primitive carries undeclared kind %q", illFormed)},
		{"auth kind", func() *ir.Document {
			doc := typesDoc()
			doc.Auth = map[ir.AuthID]ir.AuthScheme{"auth/x/a": {ID: "auth/x/a", Name: named("a"), Kind: ir.AuthKind(illFormed)}}
			return doc
		}, "ir/unknown-auth-kind", fmt.Sprintf("auth scheme names undeclared mechanism %q", illFormed)},
		{"oauth scope repeated", func() *ir.Document {
			doc := typesDoc()
			doc.Auth = map[ir.AuthID]ir.AuthScheme{"auth/x/a": {ID: "auth/x/a", Name: named("a"), Kind: ir.AuthKindOAuth2,
				Flows: []ir.OAuthFlow{{Kind: "implicit", Scopes: []ir.OAuthScope{{Name: illFormed}, {Name: illFormed}}}}}}
			return doc
		}, "ir/oauth-scope-duplicate", fmt.Sprintf("oauth flow offers scope %q more than once", illFormed)},
		{"member scope", func() *ir.Document {
			return typesDoc(&ir.Enum{ID: "t/x/E", Name: named("e"), Members: []ir.EnumMember{{
				ID: ir.EnumMemberID("e/x/" + illFormed + "/k"), Name: named("k"), Value: ir.Value{Kind: ir.ValueString, Str: "k"},
			}}})
		}, "ir/member-id-scope", fmt.Sprintf(
			"member id %q is not in the namespace \"x\" and beneath the path \"E\" of its enum \"t/x/E\"", "e/x/"+illFormed+"/k")},
		{"unsegmented", func() *ir.Document {
			return typesDoc(&ir.Model{ID: "t/x/A", Name: ir.Naming{Hint: "a1" + illFormed}})
		}, "ir/naming-unsegmented", fmt.Sprintf(
			"name hint %q runs a letter and a digit together in one word; the grammar splits that boundary", "a1"+illFormed)},
		{"not derived", func() *ir.Document {
			return typesDoc(&ir.Model{ID: "t/x/A", Name: ir.Naming{Source: illFormed + "X", Canonical: "x"}})
		}, "ir/naming-not-derived", fmt.Sprintf(
			"canonical name \"x\" is not what the grammar derives from source %q (%q); "+
				"emitters cannot tell which grammar produced it", illFormed+"X", ir.CanonicalWords(illFormed+"X"))},
		{"unmodeled reason", func() *ir.Document {
			m := namedModel("t/x/A")
			m.Unmodeled = ir.Unmodeled{"x:k": {Reason: ir.UnmodeledReason(illFormed), Value: ir.RawValue(`1`)}}
			return typesDoc(m)
		}, "ir/unknown-unmodeled-reason", fmt.Sprintf("unmodeled entry carries undeclared reason %q", illFormed)},
		{"dangling reference", func() *ir.Document {
			m := namedModel("t/x/A")
			m.Properties = []ir.Property{{ID: "p/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: ir.TypeID("t/x/" + illFormed)}}}
			return typesDoc(m)
		}, "ir/dangling-type-ref", fmt.Sprintf("reference %q does not resolve in types", "t/x/"+illFormed)},
		{"stray payload", func() *ir.Document {
			m := namedModel("t/x/A")
			m.Properties = []ir.Property{{ID: "p/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: "t/x/A"},
				Default: &ir.Value{Kind: ir.ValueString, Str: "s", Bool: true}}}
			return typesDoc(m)
		}, "ir/value-stray-payload", `value of kind "string" populates Bool, a payload its kind does not select`},
		{"missing payload", func() *ir.Document {
			m := namedModel("t/x/A")
			m.Properties = []ir.Property{{ID: "p/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: "t/x/A"},
				Default: &ir.Value{Kind: ir.ValueNumber}}}
			return typesDoc(m)
		}, "ir/value-missing-payload", `value of kind "number" leaves Num empty, which is no value of that kind`},
		{"cyclic structure", func() *ir.Document {
			c := &ir.CtorValue{Scalar: "t/x/S", Name: "from"}
			c.Args = []ir.Value{ctorValue(c)}
			m := namedModel(ir.TypeID("t/x/" + illFormed))
			m.Examples = []ir.Example{{Value: new(ctorValue(c))}}
			return typesDoc(m,
				&ir.Scalar{ID: "t/x/S", Name: named("s"), Base: &ir.TypeRef{Target: "t/prim/string"}},
				&ir.Primitive{ID: "t/prim/string", Prim: ir.PrimString})
		}, "ir/cyclic-structure", fmt.Sprintf(
			"document reaches the *ir.CtorValue it is already inside (entered at %q), so it cannot be encoded",
			"doc.Types[t/x/"+illFormed+"].Examples[0].Value.Ctor")},
		{"irVersion", func() *ir.Document {
			doc := typesDoc(namedModel("t/x/A"))
			doc.IRVersion = illFormed
			return doc
		}, "ir/ir-version-incompatible", fmt.Sprintf(
			"document declares irVersion %q; this build reads only %s", illFormed, ir.IRVersion)},
	}
}

// TestVerify_MessagesQuoteWhatTheDocumentSpells holds every message that
// interpolates document text to the contract Violation.Message states: the
// text is strconv.Quote'd, so the message is valid UTF-8 whatever the document
// holds (GitHub #400). Each case pins one site's exact spelling, so dropping a
// quote at any of them reddens its own case.
func TestVerify_MessagesQuoteWhatTheDocumentSpells(t *testing.T) {
	t.Parallel()
	for _, tc := range messageCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := verifyDeclared(tc.doc())
			for _, v := range got {
				assert.True(t, utf8.ValidString(v.Message), "%s: %q", v.Code, v.Message)
			}
			var messages []string
			for _, v := range got {
				if v.Code == tc.code {
					messages = append(messages, v.Message)
				}
			}
			require.NotEmpty(t, messages, "the case must reach %s; got %v", tc.code, codesOf(got))
			assert.Contains(t, messages, tc.want)
		})
	}
}
