// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"go/types"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/internal/jsonshape"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/pass"
)

// multipartContent is a body content typed by validDoc's model, carrying enc.
func multipartContent(enc map[ir.PropertyID]ir.PartEncoding) ir.Content {
	return ir.Content{MediaType: "multipart/form-data", Type: ir.TypeRef{Target: "type/m"}, Encoding: enc}
}

// multipartPayload is a payload of one multipartContent carrying enc.
func multipartPayload(enc map[ir.PropertyID]ir.PartEncoding) ir.Payload {
	return ir.Payload{Contents: []ir.Content{multipartContent(enc)}}
}

// encodingCarrier is one field that carries a Payload, named as the ir field it
// is, paired with the location a diagnostic about the payload must point at,
// and saying whether the position is a request — the one place
// Payload.Required is defined.
type encodingCarrier struct {
	field   string
	at      string
	request bool
	plant   func(doc *ir.Document, payload ir.Payload)
}

// encodingCarriers enumerates the Payload-bearing fields forEachPayload walks.
// TestEncodingCarriers_NameEveryPayloadFieldInTheIR holds this list against the
// IR and the cases below hold the checks built on the walk against this list,
// so a carrier added to the IR has to reach both.
func encodingCarriers() []encodingCarrier {
	return []encodingCarrier{
		{"Operation.Request", "op/request", true, func(document *ir.Document, payload ir.Payload) {
			firstOp(document).Request = &payload
		}},
		{"Response.Payload", "op/responses/0", false, func(document *ir.Document, payload ir.Payload) {
			firstOp(document).Responses = []ir.Response{{Name: ir.Naming{Source: "ok"}, Payload: &payload}}
		}},
		{"ErrorCase.Payload", "op/errors/0", false, func(document *ir.Document, payload ir.Payload) {
			firstOp(document).Errors = []ir.ErrorCase{{Name: ir.Naming{Source: "bad_request"}, Payload: &payload}}
		}},
		{"Message.Payload", "message/a", false, func(document *ir.Document, payload ir.Payload) {
			putMessage(document, func(message *ir.Message) { message.Payload = payload })
		}},
	}
}

// TestValidate_EncodingKeyAddressesNoProperty plants an encoding key naming a
// property that exists nowhere, in each field that carries a Payload.
//
// A key is a PropertyID and a property is a position inside its model, so no registry
// resolves one and the type-driven reference walk cannot reach it: before
// checkEncodingKeys every case here validated clean.
func TestValidate_EncodingKeyAddressesNoProperty(t *testing.T) {
	t.Parallel()
	for _, tc := range encodingCarriers() {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			tc.plant(doc, multipartPayload(map[ir.PropertyID]ir.PartEncoding{"property/m/ghost": {RepeatsPerArrayItem: true}}))

			found := withCode(validated(t, doc), "ir/encoding-key-unknown-property")
			require.Len(t, found, 1, "exactly the planted key must address nothing")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			at := tc.at + "/contents/0/encoding/property/m/ghost"
			assert.Equal(t, at, found[0].Provenance.Node)
			assert.Equal(t, `encoding key "property/m/ghost" at `+at+
				` addresses no property of the content's type "type/m"`, found[0].Message,
				"the message names the key, where it sits, and what it failed to address")
		})
	}
}

// TestValidate_PayloadRequiredOutsideARequest sets Payload.Required in each
// field that carries a Payload. Only the request may state it: ir.Payload
// defines the field for that one position, and an emitter rendering "required"
// off the boolean would print it on a response (GitHub #421). No compiler
// produces the shape, so the fixture is built by hand.
func TestValidate_PayloadRequiredOutsideARequest(t *testing.T) {
	t.Parallel()
	for _, tc := range encodingCarriers() {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			payload := multipartPayload(nil)
			payload.Required = new(false)
			tc.plant(doc, payload)

			found := withCode(validated(t, doc), "ir/payload-required-outside-request")
			if tc.request {
				assert.Empty(t, found, "a request body is the position the field is defined for")
				return
			}
			require.Len(t, found, 1, "the one planted field must be reported once")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Equal(t, tc.at, found[0].Provenance.Node)
			assert.Equal(t, "payload at "+tc.at+" sets required, which only a request body can state",
				found[0].Message)
		})
	}
}

// TestValidate_EncodingKeysNamingRealPropertiesAreClean is the silent half of the
// proof: with every carrier keyed by a property the content's type really
// declares, Validate reports nothing at all. A check that cannot stay silent is
// no better than one that cannot fire.
func TestValidate_EncodingKeysNamingRealPropertiesAreClean(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	for _, tc := range encodingCarriers() {
		tc.plant(doc, multipartPayload(map[ir.PropertyID]ir.PartEncoding{"property/m/a": {RepeatsPerArrayItem: true}}))
	}
	assert.Empty(t, validated(t, doc))
}

// TestValidate_EncodingKeyIsPlacedWhereItSits pins that an encoding key no
// property answers to is reported at its own position past the first: the
// second content of a request, the second response and the second error case,
// each after one carrying no such key.
func TestValidate_EncodingKeyIsPlacedWhereItSits(t *testing.T) {
	t.Parallel()
	ghost := multipartPayload(map[ir.PropertyID]ir.PartEncoding{"property/m/ghost": {RepeatsPerArrayItem: true}})
	twoContents := ir.Payload{Contents: append(
		[]ir.Content{{MediaType: "application/json", Type: ir.TypeRef{Target: "type/m"}}}, ghost.Contents...)}
	for _, tc := range []struct {
		name string
		op   ir.Operation
		at   string
	}{
		{"the second content", ir.Operation{ID: "op", Request: &twoContents}, "op/request/contents/1"},
		{"the second response", ir.Operation{ID: "op", Responses: []ir.Response{{}, {Payload: &ghost}}},
			"op/responses/1/contents/0"},
		{"the second error case", ir.Operation{ID: "op", Errors: []ir.ErrorCase{{}, {Payload: &ghost}}},
			"op/errors/1/contents/0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{tc.at + "/encoding/property/m/ghost"},
				pointers(withCode(validated(t, docWithOperation(tc.op)), "ir/encoding-key-unknown-property")))
		})
	}
}

// TestValidate_EncodingKeyThroughServiceCommonErrors covers the second position
// an ir.ErrorCase hangs from. The carrier table above plants into an operation's
// own Errors, which is one of two lists of the same node: a check reaching only
// that one would resolve a service-level error's encoding keys against nothing
// while every case above stayed green.
func TestValidate_EncodingKeyThroughServiceCommonErrors(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	payload := multipartPayload(map[ir.PropertyID]ir.PartEncoding{"property/m/ghost": {RepeatsPerArrayItem: true}})
	service(doc).CommonErrors = []ir.ErrorCase{{Name: ir.Naming{Source: "throttled"}, Payload: &payload}}

	found := withCode(validated(t, doc), "ir/encoding-key-unknown-property")
	require.Len(t, found, 1, "exactly the planted key must address nothing")
	assert.Equal(t, "s/commonErrors/0/contents/0/encoding/property/m/ghost",
		found[0].Provenance.Node,
		"the pointer names the service by ID, as checkResponseIndices names an operation, "+
			"so one node does not have two spellings from one package")
}

// TestValidate_EncodingKeyThroughComposition covers the three ways a body model
// composes a part in. An emitter renders the flat property set
// (docs/ir/models.md#the-flattening), so a key naming an inherited part is
// legal IR and the check must stay silent on it — resolving against the model's
// own properties alone would fail every one of these.
func TestValidate_EncodingKeyThroughComposition(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		compose func(*ir.Model)
	}{
		{"base", func(model *ir.Model) { model.Base = &ir.TypeRef{Target: "type/parent"} }},
		{"implements", func(model *ir.Model) { model.Implements = []ir.TypeRef{{Target: "type/parent"}} }},
		{"mixins", func(model *ir.Model) { model.Mixins = []ir.TypeRef{{Target: "type/parent"}} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			doc.Types["type/parent"] = &ir.Model{
				ID:         "type/parent",
				Properties: []ir.Property{{ID: "property/parent/x", WireName: "x", Type: ir.TypeRef{Target: "type/primitive/string"}}},
			}
			tc.compose(model(doc))
			requestContent(doc).Encoding = map[ir.PropertyID]ir.PartEncoding{"property/parent/x": {RepeatsPerArrayItem: true}}

			assert.Empty(t, withCode(validated(t, doc), "ir/encoding-key-unknown-property"))
		})
	}
}

// TestValidate_EncodingKeyThroughCyclicComposition pins that the composition walk
// terminates on a cycle — normal input for a schema language, not an edge case —
// and still reaches the far model: the key names its property, so a walk that cut
// out early would report it.
func TestValidate_EncodingKeyThroughCyclicComposition(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	model(doc).Base = &ir.TypeRef{Target: "type/cycle"}
	doc.Types["type/cycle"] = &ir.Model{
		ID:         "type/cycle",
		Properties: []ir.Property{{ID: "property/cycle/x", WireName: "x", Type: ir.TypeRef{Target: "type/primitive/string"}}},
		Base:       &ir.TypeRef{Target: "type/m"},
	}
	requestContent(doc).Encoding = map[ir.PropertyID]ir.PartEncoding{"property/cycle/x": {RepeatsPerArrayItem: true}}

	assert.Empty(t, withCode(validated(t, doc), "ir/encoding-key-unknown-property"))
}

// TestValidate_EncodingKeyThroughAlias pins that a body typed by an alias scalar
// resolves its parts through the alias. A $ref carrying siblings hoists exactly
// that shape, so this is the ordinary spelling of a referenced multipart body,
// not a corner: resolving against models alone reported every key on one.
func TestValidate_EncodingKeyThroughAlias(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		base ir.TypeID // what the content's alias stands for
	}{
		{"an alias over the body model", "type/m"},
		{"an alias over an alias over it", "type/alias/inner"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			doc.Types["type/alias/inner"] = &ir.Scalar{
				ID:   "type/alias/inner",
				Base: &ir.TypeRef{Target: "type/m"},
			}
			doc.Types["type/alias/outer"] = &ir.Scalar{
				ID:   "type/alias/outer",
				Base: &ir.TypeRef{Target: tc.base},
			}
			content := requestContent(doc)
			content.Type = ir.TypeRef{Target: "type/alias/outer"}
			content.Encoding = map[ir.PropertyID]ir.PartEncoding{"property/m/a": {RepeatsPerArrayItem: true}}

			assert.Empty(t, withCode(validated(t, doc), "ir/encoding-key-unknown-property"),
				"the aliased model declares the part this key names")
		})
	}
}

// TestValidate_EncodingKeyThroughCyclicAlias pins that the alias walk terminates
// on a cycle the same way the composition walk does, and still reaches the model
// past it: the key names that model's property, so a walk that cut out early
// would report it, and one without the visited set would never return at all.
func TestValidate_EncodingKeyThroughCyclicAlias(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Types["type/alias/a"] = &ir.Scalar{
		ID:   "type/alias/a",
		Base: &ir.TypeRef{Target: "type/alias/b"},
	}
	doc.Types["type/alias/b"] = &ir.Scalar{
		ID:   "type/alias/b",
		Base: &ir.TypeRef{Target: "type/cycle"},
	}
	doc.Types["type/cycle"] = &ir.Model{
		ID:         "type/cycle",
		Properties: []ir.Property{{ID: "property/cycle/x", WireName: "x", Type: ir.TypeRef{Target: "type/primitive/string"}}},
		Base:       &ir.TypeRef{Target: "type/alias/a"}, // closes the cycle back through the aliases.
	}
	content := requestContent(doc)
	content.Type = ir.TypeRef{Target: "type/alias/a"}
	content.Encoding = map[ir.PropertyID]ir.PartEncoding{"property/cycle/x": {RepeatsPerArrayItem: true}}

	assert.Empty(t, withCode(validated(t, doc), "ir/encoding-key-unknown-property"))
}

// TestValidate_EncodingKeyThroughAUnionBody pins a body that is a union, as one
// a composition is distributed across is: a key naming a property a variant
// exposes, the body's own on every variant or a branch's on one, names a part,
// read through an alias over the union and through a union nested in a variant
// too, and one no variant exposes names none, nor one on an alias chain
// closing on itself before any union or in a union nesting itself
// (docs/ir/payloads.md).
func TestValidate_EncodingKeyThroughAUnionBody(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		target ir.TypeID
		key    ir.PropertyID
		parts  bool
	}{
		"the body's own, on each variant":  {"type/u", "property/m/a", true},
		"a branch's, on one variant":       {"type/u", "property/other/b", true},
		"through an alias over the union":  {"type/alias", "property/other/b", true},
		"one no variant exposes":           {"type/u", "property/nowhere", false},
		"an alias cycle reaching no union": {"type/loop/a", "property/nowhere", false},
		"a nested union's variant's":       {"type/outer", "property/other/b", true},
		"none, in a union nesting itself":  {"type/self", "property/nowhere", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			doc.Types["type/other"] = &ir.Model{ID: "type/other", Base: &ir.TypeRef{Target: "type/m"},
				Properties: []ir.Property{{ID: "property/other/b", WireName: "b", Type: ir.TypeRef{Target: "type/primitive/string"}}}}
			doc.Types["type/u"] = &ir.Union{ID: "type/u", Closed: true,
				Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/m"}}, {Type: ir.TypeRef{Target: "type/other"}}}}
			doc.Types["type/alias"] = &ir.Scalar{ID: "type/alias", Base: &ir.TypeRef{Target: "type/u"}}
			doc.Types["type/outer"] = &ir.Union{ID: "type/outer", Closed: true,
				Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/primitive/string"}}, {Type: ir.TypeRef{Target: "type/alias"}}}}
			doc.Types["type/self"] = &ir.Union{ID: "type/self", Closed: true,
				Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/self"}}, {Type: ir.TypeRef{Target: "type/m"}}}}
			doc.Types["type/loop/a"] = &ir.Scalar{ID: "type/loop/a", Base: &ir.TypeRef{Target: "type/loop/b"}}
			doc.Types["type/loop/b"] = &ir.Scalar{ID: "type/loop/b", Base: &ir.TypeRef{Target: "type/loop/a"}}
			content := requestContent(doc)
			content.Type = ir.TypeRef{Target: tc.target}
			content.Encoding = map[ir.PropertyID]ir.PartEncoding{tc.key: {RepeatsPerArrayItem: true}}

			found := withCode(validated(t, doc), "ir/encoding-key-unknown-property")
			if tc.parts {
				assert.Empty(t, found)
				return
			}
			assert.Len(t, found, 1)
		})
	}
}

// TestValidate_EncodingKeyThroughANarrowingModel holds a key on a body that is
// a model narrowing a union, or on a union's variant that is one, to the
// members of the shapes it takes: a member only one of the union's variants
// declares is a part, and one no shape holds is none.
func TestValidate_EncodingKeyThroughANarrowingModel(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		target ir.TypeID
		key    ir.PropertyID
		parts  bool
	}{
		"a variant's, on the model":              {"type/n", "property/other/b", true},
		"the model's own":                        {"type/n", "property/n/note", true},
		"a variant's, on a union's variant":      {"type/wrap", "property/other/b", true},
		"one no shape holds":                     {"type/n", "property/nowhere", false},
		"a variant's, on a model narrowing none": {"type/m", "property/other/b", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := narrowingDoc()
			content := requestContent(doc)
			content.Type = ir.TypeRef{Target: tc.target}
			content.Encoding = map[ir.PropertyID]ir.PartEncoding{tc.key: {RepeatsPerArrayItem: true}}
			found := withCode(validated(t, doc), "ir/encoding-key-unknown-property")
			if tc.parts {
				assert.Empty(t, found)
				return
			}
			assert.Len(t, found, 1)
		})
	}
	doc := narrowingDoc()
	assert.True(t, pass.NarrowedHolds(doc.Types, "type/n", "property/other/b", 1_000))
	assert.False(t, pass.NarrowedHolds(doc.Types, "type/n", "property/other/b", 1), "a walk past its limit holds nothing")
}

// narrowingDoc is validDoc with t/n, a model declaring note and composing t/u,
// a union of validDoc's t/m and t/other, which declares b, and t/wrap, a union
// of a string and t/n.
func narrowingDoc() *ir.Document {
	doc := validDoc()
	doc.Types["type/other"] = &ir.Model{ID: "type/other",
		Properties: []ir.Property{{ID: "property/other/b", WireName: "b", Type: ir.TypeRef{Target: "type/primitive/string"}}}}
	doc.Types["type/u"] = &ir.Union{ID: "type/u", Closed: true,
		Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/m"}}, {Type: ir.TypeRef{Target: "type/other"}}}}
	doc.Types["type/n"] = &ir.Model{ID: "type/n", Base: &ir.TypeRef{Target: "type/u"},
		Properties: []ir.Property{{ID: "property/n/note", WireName: "note", Type: ir.TypeRef{Target: "type/primitive/string"}}}}
	doc.Types["type/wrap"] = &ir.Union{ID: "type/wrap", Closed: true,
		Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/primitive/string"}}, {Type: ir.TypeRef{Target: "type/n"}}}}
	return doc
}

// TestValidate_EncodingKeyOnTypeWithoutParts covers the targets that expose no
// part at all: a type that is no model, an opaque alias standing for nothing, a
// registry entry holding a typed nil — which must be reported rather than
// dereferenced — and no target at all. Every key on such a content addresses
// nothing.
func TestValidate_EncodingKeyOnTypeWithoutParts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		target ir.TypeID
	}{
		{"a type that is no model", "type/primitive/string"},
		{"an opaque alias over nothing", "type/opaque"},
		{"a typed nil registry entry", "type/nil"},
		{"no type at all", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			doc.Types["type/nil"] = (*ir.Model)(nil) // seeded for every case; only one reaches it.
			doc.Types["type/opaque"] = &ir.Scalar{ID: "type/opaque"}
			content := requestContent(doc)
			content.Type = ir.TypeRef{Target: tc.target}
			content.Encoding = map[ir.PropertyID]ir.PartEncoding{"property/m/a": {RepeatsPerArrayItem: true}}

			found := withCode(validated(t, doc), "ir/encoding-key-unknown-property")
			require.Len(t, found, 1, "a type exposing no property resolves no key")
			assert.Contains(t, found[0].Message,
				`addresses no property of the content's type "`+string(tc.target)+`"`)
		})
	}
}

// TestValidate_EncodingKeyOnAnUndeclaredTypeIsReportedBesideTheDanglingRef
// holds the double report appendEncodingKeyDiags documents: the type is a
// broken reference, and the key, a different claim, routes no part.
func TestValidate_EncodingKeyOnAnUndeclaredTypeIsReportedBesideTheDanglingRef(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	content := requestContent(doc)
	content.Type = ir.TypeRef{Target: "type/missing"}
	content.Encoding = map[ir.PropertyID]ir.PartEncoding{"property/m/a": {RepeatsPerArrayItem: true}}

	got := irtest.DiagCodes(validated(t, doc))
	assert.Contains(t, got, "ir/dangling-type-ref")
	assert.Contains(t, got, "ir/encoding-key-unknown-property")
}

// aliasMultipartSpec is a multipart body written so the media schema lowers to an
// alias over the model that declares the parts: a $ref carrying siblings has
// nowhere but an alias to keep them, and a component that is a bare $ref is one.
func aliasMultipartSpec(mediaSchema string) string {
	return `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /upload:
    post:
      operationId: upload
      requestBody:
        content:
          multipart/form-data:
            schema: ` + mediaSchema + `
      responses: {"200": {description: ok}}
components:
  schemas:
    Form: {type: object, properties: {file: {type: string, format: binary}}}
    AliasForm: {$ref: "#/components/schemas/Form"}
`
}

// TestValidate_CompiledAliasMultipartBodyIsClean compiles the documents whose
// bodies sit behind an alias and requires validation to stay silent, the way
// `morphic compile` runs the two stages together.
//
// The unit cases above plant an alias by hand, and the conformance corpus has no
// multipart body behind one — so nothing else here meets what the compiler
// actually emits for these ordinary spellings, which is where a checker
// disagreeing with the compiler shows up as an error on a valid document. No
// explicit encoding block is needed: an array or binary part mints an entry on
// its own.
func TestValidate_CompiledAliasMultipartBodyIsClean(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		mediaSchema string
	}{
		{"a $ref carrying siblings", `{$ref: "#/components/schemas/Form", title: Upload}`},
		{"a $ref to an alias component", `{$ref: "#/components/schemas/AliasForm"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{{Path: "spec.yaml", Data: []byte(aliasMultipartSpec(tc.mediaSchema))}},
				compilers.Options{})
			require.NoError(t, err)
			require.NotNil(t, doc)

			require.NotEmpty(t, encodingKeys(t, doc), "the body's binary part mints an encoding key to check")
			assert.Empty(t, withCode(validated(t, doc), "ir/encoding-key-unknown-property"))
		})
	}
}

// encodingKeys returns the encoding keys of the document's single request
// content, so a case asserting nothing is reported first proves there was
// something to report on.
func encodingKeys(t *testing.T, doc *ir.Document) []ir.PropertyID {
	t.Helper()
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Groups, 1)
	require.Len(t, doc.Services[0].Groups[0].Operations, 1)
	req := doc.Services[0].Groups[0].Operations[0].Request
	require.NotNil(t, req)
	require.Len(t, req.Contents, 1)
	return slices.Collect(maps.Keys(req.Contents[0].Encoding))
}

// TestValidate_EncodingKeyDiagnosticsAreDeterministic pins invariant 7 for the
// encoding map, whose keys Go ranges in randomized order: they are sorted before
// any diagnostic is built.
func TestValidate_EncodingKeyDiagnosticsAreDeterministic(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	requestContent(doc).Encoding = map[ir.PropertyID]ir.PartEncoding{
		"property/m/z": {}, "property/m/b": {}, "property/m/q": {}, "property/m/c": {}, "property/m/m": {},
	}
	want := pointers(withCode(validated(t, doc), "ir/encoding-key-unknown-property"))
	require.Equal(t, []string{
		"op/request/contents/0/encoding/property/m/b",
		"op/request/contents/0/encoding/property/m/c",
		"op/request/contents/0/encoding/property/m/m",
		"op/request/contents/0/encoding/property/m/q",
		"op/request/contents/0/encoding/property/m/z",
	}, want)
	for range 8 {
		assert.Equal(t, want, pointers(withCode(validated(t, doc), "ir/encoding-key-unknown-property")))
	}
}

// TestValidate_MessagePayloadDiagnosticsAreDeterministic pins invariant 7 for
// the messages map, whose keys Go ranges in randomized order: forEachPayload
// sorts them before any payload site reaches a check. Each check that reads
// payloads through it has a row, so one that ranged the messages itself would
// redden its own.
func TestValidate_MessagePayloadDiagnosticsAreDeterministic(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	doc.Messages = map[ir.MessageID]ir.Message{}
	for _, id := range []ir.MessageID{"message/z", "message/b", "message/q", "message/c", "message/m"} {
		doc.Messages[id] = ir.Message{ID: id, Payload: ir.Payload{Required: new(true), Contents: []ir.Content{{
			MediaType: "multipart/form-data",
			Type:      ir.TypeRef{Target: "type/m"},
			Encoding:  map[ir.PropertyID]ir.PartEncoding{"property/m/none": {}},
		}}}}
	}
	cases := []struct{ name, code, at string }{
		{name: "checkPayloadRequired", code: "ir/payload-required-outside-request"},
		{name: "checkEncodingKeys", code: "ir/encoding-key-unknown-property", at: "/contents/0/encoding/property/m/none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := pointers(withCode(validated(t, doc), tc.code))
			require.Equal(t, []string{
				"message/b" + tc.at, "message/c" + tc.at, "message/m" + tc.at, "message/q" + tc.at, "message/z" + tc.at,
			}, want)
			for range 8 {
				assert.Equal(t, want, pointers(withCode(validated(t, doc), tc.code)))
			}
		})
	}
}

// payloadType is the node whose carriers TestEncodingCarriers_NameEveryPayloadFieldInTheIR
// enumerates.
var payloadType = reflect.TypeFor[ir.Payload]()

// maxFieldTypeDepth bounds the walk through one field's type constructors (the
// bounded-recursion rule). A field type is a finite tower of pointers, slices
// and maps, except that `type T *T` is legal Go, which is what the counter is
// for; reaching it means the walk stopped being a walk over the IR, so it
// fails rather than truncating.
const maxFieldTypeDepth = 32

// TestEncodingCarriers_NameEveryPayloadFieldInTheIR fails when the IR declares a
// field carrying an ir.Payload that encodingCarriers does not name.
//
// forEachPayload names the Payload-bearing fields by hand, a coupling the
// compiler cannot check: a carrier added to the IR would otherwise be walked by
// neither the checks built on the walk nor the cases built on encodingCarriers,
// with the whole suite green. The guard holds both lists in two steps. This
// test holds encodingCarriers against the IR;
// TestValidate_EncodingKeyAddressesNoProperty holds forEachPayload against
// encodingCarriers by requiring a diagnostic from every entry.
func TestEncodingCarriers_NameEveryPayloadFieldInTheIR(t *testing.T) {
	t.Parallel()
	carriers := encodingCarriers()
	listed := make([]string, 0, len(carriers))
	for _, encodingCarrier := range carriers {
		listed = append(listed, encodingCarrier.field)
	}
	slices.Sort(listed)

	found := fieldsCarrying(t, payloadType)
	require.NotEmpty(t, found, "the shape holds no ir.Payload field at all, so the read reached "+
		"nothing and proves nothing about the ones encodingCarriers names")
	assert.Empty(t, cmp.Diff(found, listed),
		"encodingCarriers must name every ir field that carries an ir.Payload, once each "+
			"(-declared +listed); a new one also has to be walked by forEachPayload")
}

// fieldsCarrying returns "Owner.Field", sorted, for every struct field the IR
// declares whose type is or contains node, an ir struct. It reads the fields
// off the JSON shape ir.Document reaches, which lists a sealed sum's variants
// from the ir sources, so a carrier is covered the day it is declared.
func fieldsCarrying(t *testing.T, node reflect.Type) []string {
	t.Helper()
	loadedPackage, err := decls.Load(node.PkgPath())
	require.NoError(t, err)
	shape, err := jsonshape.Read(loadedPackage, "Document")
	require.NoError(t, err)

	var found []string
	for _, typ := range shape.Types {
		for _, member := range typ.Members {
			if carries(t, member.Field.Type(), node, 0) {
				found = append(found, typ.Named.Obj().Name()+"."+member.Field.Name())
			}
		}
	}
	slices.Sort(found)
	return found
}

// carries reports whether a field's type is node, or a pointer, slice, array or
// map of one.
//
// It does not descend into structs. A field whose type merely reaches a Payload
// further down is the spine leading to a carrier — Document.Services and
// Document.Messages between them reach every one of today's — and naming the
// spine would name most of the IR.
func carries(t *testing.T, typ types.Type, node reflect.Type, depth int) bool {
	t.Helper()
	require.Less(t, depth, maxFieldTypeDepth, "resolving a field type exceeded %d steps", maxFieldTypeDepth)
	if named, isNamed := types.Unalias(typ).(*types.Named); isNamed {
		obj := named.Obj()
		if obj.Pkg() != nil && obj.Pkg().Path() == node.PkgPath() && obj.Name() == node.Name() {
			return true
		}
	}
	switch u := typ.Underlying().(type) {
	case *types.Pointer:
		return carries(t, u.Elem(), node, depth+1)
	case *types.Slice:
		return carries(t, u.Elem(), node, depth+1)
	case *types.Array:
		return carries(t, u.Elem(), node, depth+1)
	case *types.Map:
		return carries(t, u.Key(), node, depth+1) || carries(t, u.Elem(), node, depth+1)
	default:
		return false // a struct is a carrier's owner, not a carrier
	}
}

// TestValidate_ADefaultPayloadIsJudgedAsAResponsesIs pins that the payload walk
// reaches Operation.Default: a Default payload that sets Required is reported
// as a response's would be, and an encoding key naming no property of its body
// draws the same diagnostic a Response's does. Both sit at the operation's
// default position, so a walk that skipped it would leave the payload unjudged.
func TestValidate_ADefaultPayloadIsJudgedAsAResponsesIs(t *testing.T) {
	t.Parallel()

	t.Run("a Required that only a request can state", func(t *testing.T) {
		t.Parallel()
		doc := validDoc()
		payload := multipartPayload(nil)
		payload.Required = new(true)
		def := defaultResponse()
		def.Payload = &payload
		firstOp(doc).Default = def

		found := withCode(validated(t, doc), "ir/payload-required-outside-request")
		require.Len(t, found, 1)
		assert.Equal(t, ir.SeverityError, found[0].Severity)
		assert.Equal(t, "op/default", found[0].Provenance.Node)
		assert.Equal(t, "payload at op/default sets required, which only a request body can state",
			found[0].Message)
	})

	t.Run("an encoding key addressing no property", func(t *testing.T) {
		t.Parallel()
		doc := validDoc()
		payload := multipartPayload(map[ir.PropertyID]ir.PartEncoding{"property/m/ghost": {RepeatsPerArrayItem: true}})
		def := defaultResponse()
		def.Payload = &payload
		firstOp(doc).Default = def

		found := withCode(validated(t, doc), "ir/encoding-key-unknown-property")
		require.Len(t, found, 1)
		assert.Equal(t, ir.SeverityError, found[0].Severity)
		at := "op/default/contents/0/encoding/property/m/ghost"
		assert.Equal(t, at, found[0].Provenance.Node)
		assert.Equal(t, `encoding key "property/m/ghost" at `+at+
			` addresses no property of the content's type "type/m"`, found[0].Message)
	})

	t.Run("a payload keyed by a real property is clean", func(t *testing.T) {
		t.Parallel()
		doc := validDoc()
		payload := multipartPayload(map[ir.PropertyID]ir.PartEncoding{"property/m/a": {RepeatsPerArrayItem: true}})
		def := defaultResponse()
		def.Payload = &payload
		firstOp(doc).Default = def

		assert.Empty(t, validated(t, doc))
	})
}
