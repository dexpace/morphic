// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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
	doc := typesDoc(namedModel("type/x/A"))
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
			model := namedModel("type/x/A")
			model.Properties = []ir.Property{{ID: "property/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: "type/x/A"},
				Default: &ir.Value{Kind: ir.ValueNumber, Number: ir.BigVal(illFormed)}}}
			return typesDoc(model)
		}, "ir/bigval-not-numeric",
			fmt.Sprintf("numeric value %q is not a decimal literal, so it does not read back as a JSON number", illFormed)},
		{"inferred not canonical", func() *ir.Document {
			model := namedModel("type/x/A")
			model.Provenance.Inferred = illFormed + "," + illFormed
			return typesDoc(model)
		}, "ir/inferred-not-canonical", fmt.Sprintf(
			"inferred marker %q is not the list %q ir.JoinInferred writes for it", illFormed+","+illFormed, illFormed)},
		{"inferred on a reference admitting no null", func() *ir.Document {
			model := namedModel("type/x/A")
			model.Properties = []ir.Property{{ID: "property/x/A/p", Name: named("p"),
				Type: ir.TypeRef{Target: "type/x/A", Inferred: illFormed}}}
			return typesDoc(model)
		}, "ir/type-ref-inferred-not-nullable", fmt.Sprintf(
			"type reference is marked inferred by %q but admits no null", illFormed)},
		{"inferred on a part carrying no filename", func() *ir.Document {
			return opDoc(ir.Operation{ID: "operation/x/S/op", Name: named("op"), Request: &ir.Payload{Contents: []ir.Content{{
				ItemEncoding: &ir.PartEncoding{Inferred: illFormed},
			}}}})
		}, "ir/part-encoding-inferred-not-filename", fmt.Sprintf(
			"part encoding is marked inferred by %q but carries no filename", illFormed)},
		{"bigval out of range", func() *ir.Document {
			model := namedModel("type/x/A")
			model.Properties = []ir.Property{{ID: "property/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: "type/x/A"},
				Default: &ir.Value{Kind: ir.ValueNumber, Number: "1e1000000000000000000"}}}
			return typesDoc(model)
		}, "ir/bigval-out-of-range",
			`numeric value "1e1000000000000000000" is a decimal literal whose leading digit carries a power of ten ` +
				`past the ±999999999999999999 a BigVal holds`},
		{"bigval not canonical", func() *ir.Document {
			model := namedModel("type/x/A")
			model.Properties = []ir.Property{{ID: "property/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: "type/x/A"},
				Default: &ir.Value{Kind: ir.ValueNumber, Number: "01"}}}
			return typesDoc(model)
		}, "ir/bigval-not-canonical", `numeric value "01" is not the JSON form "1" ir.NewBigVal produces for it`},
		{"duplicate id", func() *ir.Document {
			op := ir.Operation{ID: ir.OperationID("operation/x/S/" + illFormed), Name: named("op")}
			doc := opDoc(op)
			doc.Services[0].Groups[0].Operations = append(doc.Services[0].Groups[0].Operations, op)
			return doc
		}, "ir/duplicate-operation-id", fmt.Sprintf("id %q is declared here and at %q",
			"operation/x/S/"+illFormed, "document.Services[0].Groups[0].Operations[0]")},
		{"idempotency", func() *ir.Document {
			return opDoc(ir.Operation{ID: "operation/x/S/op", Name: named("op"),
				Idempotency: ir.Idempotency{Kind: ir.IdempotencyKind(illFormed), TokenParam: ir.ParameterID(illFormed)}})
		}, "ir/idempotency-token", fmt.Sprintf(
			"idempotency kind %q with token parameter %q: a token is named exactly when the kind is %q",
			illFormed, illFormed, ir.IdempotencyToken)},
		{"webhook template", func() *ir.Document {
			return opDoc(ir.Operation{ID: "operation/x/S/op", Name: named("op"), Bindings: ir.OpBindings{
				HTTP: []ir.HTTPBinding{{Inbound: ir.HTTPInboundWebhook, URITemplate: illFormed}}}})
		}, "ir/webhook-route", fmt.Sprintf(
			"webhook binding carries URI template %q; a webhook names its event in WebhookName and has no template",
			illFormed)},
		{"webhook name", func() *ir.Document {
			return opDoc(ir.Operation{ID: "operation/x/S/op", Name: named("op"), Bindings: ir.OpBindings{
				HTTP: []ir.HTTPBinding{{WebhookName: illFormed}}}})
		}, "ir/webhook-route", fmt.Sprintf(
			"binding that is not a webhook carries webhook name %q; only a webhook binding names an event", illFormed)},
		{"prefix header of no map", func() *ir.Document {
			doc := opDoc(ir.Operation{ID: "operation/x/S/op", Name: named("op"), Responses: []ir.Response{{Headers: []ir.Header{{
				ID: "property/x/h", Name: named("h"), Type: ir.TypeRef{Target: illFormed}, Prefix: new(""),
			}}}}})
			doc.Types = ir.TypeRegistry{illFormed: namedModel(illFormed)}
			return doc
		}, "ir/prefix-header-not-map", fmt.Sprintf("header under a prefix has type %q, which is no map", illFormed)},
		{"prefix header wire name", func() *ir.Document {
			return opDoc(ir.Operation{ID: "operation/x/S/op", Name: named("op"), Responses: []ir.Response{{Headers: []ir.Header{{
				ID: "property/x/h", Name: named("h"), WireName: illFormed, Type: ir.TypeRef{Target: "type/x/M"}, Prefix: new(""),
			}}}}})
		}, "ir/prefix-header-wire-name", fmt.Sprintf("header under a prefix also names the field %q", illFormed)},
		{"prefix binding location", func() *ir.Document {
			return opDoc(ir.Operation{ID: "operation/x/S/op", Name: named("op"), Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
				ParamBindings: []ir.HTTPParamBinding{{Location: ir.HTTPLocation(illFormed), Prefix: new("")}},
			}}}})
		}, "ir/prefix-binding-location", fmt.Sprintf(
			"binding under a prefix binds location %q, which is neither header nor query", illFormed)},
		{"prefix binding wire name", func() *ir.Document {
			return opDoc(ir.Operation{ID: "operation/x/S/op", Name: named("op"), Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{
				ParamBindings: []ir.HTTPParamBinding{{Location: ir.HTTPLocationQuery, WireName: illFormed, Prefix: new("")}},
			}}}})
		}, "ir/prefix-binding-wire-name", fmt.Sprintf("binding under a prefix also names the field %q", illFormed)},
		{"prefix binding of no map", func() *ir.Document {
			doc := opDoc(ir.Operation{
				ID: "operation/x/S/op", Name: named("op"),
				Params: []ir.Parameter{{ID: "parameter/x/p", Name: named("p"), Type: ir.TypeRef{Target: illFormed}}},
				Bindings: ir.OpBindings{HTTP: []ir.HTTPBinding{{ParamBindings: []ir.HTTPParamBinding{{
					Param: "parameter/x/p", Location: ir.HTTPLocationHeader, Prefix: new(""),
				}}}}},
			})
			doc.Types = ir.TypeRegistry{illFormed: namedModel(illFormed)}
			return doc
		}, "ir/prefix-binding-not-map", fmt.Sprintf(
			"binding under a prefix binds a parameter of type %q, which is no map", illFormed)},
		{"inbound kind", func() *ir.Document {
			return opDoc(ir.Operation{ID: "operation/x/S/op", Name: named("op"), Bindings: ir.OpBindings{
				HTTP: []ir.HTTPBinding{{Inbound: ir.HTTPInbound(illFormed)}}}})
		}, "ir/unknown-http-inbound", fmt.Sprintf("binding carries undeclared inbound kind %q", illFormed)},
		{"callback unlisted", func() *ir.Document {
			return opDoc(ir.Operation{ID: ir.OperationID("operation/x/S/" + illFormed), Name: named("op"), Bindings: ir.OpBindings{
				HTTP: []ir.HTTPBinding{{Inbound: ir.HTTPInboundCallback}}}})
		}, "ir/callback-unlisted", fmt.Sprintf(
			"operation %q is marked a callback, but no binding lists it among its Callbacks", "operation/x/S/"+illFormed)},
		{"callback unmarked", func() *ir.Document {
			cb := ir.OperationID("operation/x/S/" + illFormed)
			doc := opDoc(ir.Operation{ID: "operation/x/S/op", Name: named("op"), Bindings: ir.OpBindings{
				HTTP: []ir.HTTPBinding{{Callbacks: []ir.Callback{{Operations: []ir.OperationID{cb}}}}}}})
			doc.Services[0].Groups[0].Operations = append(doc.Services[0].Groups[0].Operations,
				ir.Operation{ID: cb, Name: named("cb")})
			return doc
		}, "ir/callback-unmarked", fmt.Sprintf(
			"a binding lists operation %q among its Callbacks, but it is not marked a callback", "operation/x/S/"+illFormed)},
		{"prim not derived", func() *ir.Document {
			return typesDoc(&ir.Primitive{ID: ir.TypeID("type/x/" + illFormed), PrimitiveKind: ir.PrimitiveString})
		}, "ir/prim-id-not-derived", fmt.Sprintf(
			"primitive of kind \"string\" is interned at %q rather than the shared \"type/primitive/string\"", "type/x/"+illFormed)},
		{"prim with no kind", func() *ir.Document {
			return typesDoc(&ir.Primitive{ID: ir.TypeID("type/x/" + illFormed)})
		}, "ir/prim-id-not-derived", fmt.Sprintf(
			"primitive at %q carries no kind, so no shared ID derives from it", "type/x/"+illFormed)},
		{"prim space reserved", func() *ir.Document {
			return typesDoc(namedModel(ir.TypeID("type/primitive/" + illFormed)))
		}, "ir/prim-space-reserved", fmt.Sprintf(
			"id %q addresses the reserved primitive space but names a model", "type/primitive/"+illFormed)},
		{"id malformed", func() *ir.Document {
			return typesDoc(namedModel(ir.TypeID("type//" + illFormed)))
		}, "ir/id-malformed", fmt.Sprintf(
			"id %q is not type/<space>[/<path>]; every segment must be non-empty", "type//"+illFormed)},
		{"id disagrees with its pointer", func() *ir.Document {
			model := namedModel(ir.TypeID("type/x/" + illFormed))
			model.Provenance = ir.Provenance{Pointer: "/other"}
			return typesDoc(model)
		}, "ir/id-provenance-disagreement", fmt.Sprintf(
			"id %q carries path %q, which is not the source pointer \"/other\" it records", "type/x/"+illFormed, illFormed)},
		{"namespace undeclared", func() *ir.Document {
			doc := typesDoc(namedModel(ir.TypeID("type/" + illFormed + "/A")))
			doc.IDSpaces = map[string][]string{ir.IDKindType: {"x"}}
			return doc
		}, "ir/id-space-undeclared", fmt.Sprintf(
			"id %q lives in namespace %q, which the document does not declare for kind \"type\"", "type/"+illFormed+"/A", illFormed)},
		{"kind unknown", func() *ir.Document {
			return spacedDoc(map[string][]string{ir.IDKindType: {"x"}, illFormed: {"x"}})
		}, "ir/id-spaces-unknown-kind", fmt.Sprintf(
			"id namespaces are declared for kind %q, which no id opens with", illFormed)},
		{"kind with no namespace", func() *ir.Document {
			return spacedDoc(map[string][]string{ir.IDKindType: {"x"}, ir.IDKindOperation: {}})
		}, "ir/id-spaces-not-canonical", `kind "operation" is declared with no namespace, and a kind with none is left out`},
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
			doc.Types[ir.TypeID("type/x/"+illFormed)] = namedModel("type/x/A")
			return doc
		}, "ir/type-id-mismatch", fmt.Sprintf("registry key %q disagrees with node ID \"type/x/A\"", "type/x/"+illFormed)},
		{"prim kind", func() *ir.Document {
			return typesDoc(&ir.Primitive{ID: ir.PrimitiveTypeID(ir.PrimitiveKind(illFormed)), PrimitiveKind: ir.PrimitiveKind(illFormed)})
		}, "ir/unknown-prim-kind", fmt.Sprintf("primitive carries undeclared kind %q", illFormed)},
		{"auth kind", func() *ir.Document {
			doc := typesDoc()
			doc.Auth = map[ir.AuthID]ir.AuthScheme{"auth/x/a": {ID: "auth/x/a", Name: named("a"), Kind: ir.AuthKind(illFormed)}}
			return doc
		}, "ir/unknown-auth-kind", fmt.Sprintf("auth scheme names undeclared mechanism %q", illFormed)},
		{"streaming mode", func() *ir.Document {
			doc := typesDoc()
			doc.Services = []ir.Service{{ID: "service/x/s", Name: named("s"), Groups: []ir.OperationGroup{{
				ID: "operation-group/x/g", Name: named("g"),
				Operations: []ir.Operation{{ID: "operation/x/a", Name: named("a"), Streaming: ir.StreamingMode(illFormed)}},
			}}}}
			return doc
		}, "ir/unknown-streaming-mode", fmt.Sprintf("operation carries undeclared streaming mode %q", illFormed)},
		{"oauth scope repeated", func() *ir.Document {
			doc := typesDoc()
			doc.Auth = map[ir.AuthID]ir.AuthScheme{"auth/x/a": {ID: "auth/x/a", Name: named("a"), Kind: ir.AuthKindOAuth2,
				Flows: []ir.OAuthFlow{{Kind: "implicit", Scopes: []ir.OAuthScope{{Name: illFormed}, {Name: illFormed}}}}}}
			return doc
		}, "ir/oauth-scope-duplicate", fmt.Sprintf("oauth flow offers scope %q more than once", illFormed)},
		{"auth kind in the unresolved space", func() *ir.Document {
			const id ir.AuthID = "auth/unresolved/a"
			doc := typesDoc()
			doc.Auth = map[ir.AuthID]ir.AuthScheme{id: {ID: id, Name: named("a"), Kind: ir.AuthKind(illFormed)}}
			return doc
		}, "ir/unresolved-auth-space", fmt.Sprintf("auth scheme of kind %q is addressed in space \"unresolved\", "+
			"and the kind unresolved and the space unresolved go together", illFormed)},
		{"auth space of the unresolved kind", func() *ir.Document {
			id := ir.AuthID("auth/" + illFormed + "/a")
			doc := typesDoc()
			doc.Auth = map[ir.AuthID]ir.AuthScheme{id: {ID: id, Name: named("a"), Kind: ir.AuthKindUnresolved}}
			return doc
		}, "ir/unresolved-auth-space", fmt.Sprintf("auth scheme of kind \"unresolved\" is addressed in space %q, "+
			"and the kind unresolved and the space unresolved go together", illFormed)},
		{"member scope", func() *ir.Document {
			return typesDoc(&ir.Enum{ID: "type/x/E", Name: named("e"), Members: []ir.EnumMember{{
				ID: ir.EnumMemberID("enum-member/x/" + illFormed + "/k"), Name: named("k"), Value: ir.Value{Kind: ir.ValueString, String: "k"},
			}}})
		}, "ir/member-id-scope", fmt.Sprintf(
			"member id %q is not in the namespace \"x\" and beneath the path \"E\" of its enum \"type/x/E\"", "enum-member/x/"+illFormed+"/k")},
		{"unsegmented", func() *ir.Document {
			return typesDoc(&ir.Model{ID: "type/x/A", Name: ir.Naming{Hint: "a1" + illFormed}})
		}, "ir/naming-unsegmented", fmt.Sprintf(
			"name hint %q runs a letter and a digit together in one word; the grammar splits that boundary", "a1"+illFormed)},
		{"not derived", func() *ir.Document {
			return typesDoc(&ir.Model{ID: "type/x/A", Name: ir.Naming{Source: illFormed + "X", Canonical: "x"}})
		}, "ir/naming-not-derived", fmt.Sprintf(
			"canonical name \"x\" is not what the grammar derives from source %q (%q); "+
				"emitters cannot tell which grammar produced it", illFormed+"X", ir.CanonicalWords(illFormed+"X"))},
		{"unmodeled reason", func() *ir.Document {
			model := namedModel("type/x/A")
			model.Unmodeled = ir.Unmodeled{"bytes:k": {Reason: ir.UnmodeledReason(illFormed), Value: ir.RawValue(`1`)}}
			return typesDoc(model)
		}, "ir/unknown-unmodeled-reason", fmt.Sprintf("unmodeled entry carries undeclared reason %q", illFormed)},
		{"dangling reference", func() *ir.Document {
			model := namedModel("type/x/A")
			model.Properties = []ir.Property{{ID: "property/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: ir.TypeID("type/x/" + illFormed)}}}
			return typesDoc(model)
		}, "ir/dangling-type-ref", fmt.Sprintf("reference %q does not resolve in types", "type/x/"+illFormed)},
		{"stray payload", func() *ir.Document {
			model := namedModel("type/x/A")
			model.Properties = []ir.Property{{ID: "property/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: "type/x/A"},
				Default: &ir.Value{Kind: ir.ValueString, String: "s", Bool: true}}}
			return typesDoc(model)
		}, "ir/value-stray-payload", `value of kind "string" populates Bool, a payload its kind does not select`},
		{"missing payload", func() *ir.Document {
			model := namedModel("type/x/A")
			model.Properties = []ir.Property{{ID: "property/x/A/p", Name: named("p"), Type: ir.TypeRef{Target: "type/x/A"},
				Default: &ir.Value{Kind: ir.ValueNumber}}}
			return typesDoc(model)
		}, "ir/value-missing-payload", `value of kind "number" leaves Number empty, which is no value of that kind`},
		{"cyclic structure", func() *ir.Document {
			constructorValue := &ir.ConstructorValue{Scalar: "type/x/S", Name: "from"}
			constructorValue.Args = []ir.Value{ctorValue(constructorValue)}
			model := namedModel(ir.TypeID("type/x/" + illFormed))
			model.Examples = []ir.Example{{Value: new(ctorValue(constructorValue))}}
			return typesDoc(model,
				&ir.Scalar{ID: "type/x/S", Name: named("s"), Base: &ir.TypeRef{Target: "type/primitive/string"}},
				&ir.Primitive{ID: "type/primitive/string", PrimitiveKind: ir.PrimitiveString})
		}, "ir/cyclic-structure", fmt.Sprintf(
			"document reaches the *ir.ConstructorValue it is already inside (entered at %q), so it cannot be encoded",
			"document.Types[type/x/"+illFormed+"].Examples[0].Value.Constructor")},
		{"irVersion", func() *ir.Document {
			doc := typesDoc(namedModel("type/x/A"))
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
			for _, violation := range got {
				assert.True(t, utf8.ValidString(violation.Message), "%s: %q", violation.Code, violation.Message)
			}
			var messages []string
			for _, violation := range got {
				if violation.Code == tc.code {
					messages = append(messages, violation.Message)
				}
			}
			require.NotEmpty(t, messages, "the case must reach %s; got %v", tc.code, codesOf(got))
			assert.Contains(t, messages, tc.want)
		})
	}
}
