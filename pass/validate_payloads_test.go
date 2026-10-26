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
)

// multipartContent is a body content typed by validDoc's model, carrying enc.
func multipartContent(enc map[ir.PropID]ir.PartEncoding) ir.Content {
	return ir.Content{MediaType: "multipart/form-data", Type: ir.TypeRef{Target: "t/m"}, Encoding: enc}
}

// multipartPayload is a payload of one multipartContent carrying enc.
func multipartPayload(enc map[ir.PropID]ir.PartEncoding) ir.Payload {
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
		{"Operation.Request", "op/request", true, func(d *ir.Document, p ir.Payload) {
			firstOp(d).Request = &p
		}},
		{"Response.Payload", "op/responses/0", false, func(d *ir.Document, p ir.Payload) {
			firstOp(d).Responses = []ir.Response{{Name: ir.Naming{Source: "ok"}, Payload: &p}}
		}},
		{"ErrorCase.Payload", "op/errors/0", false, func(d *ir.Document, p ir.Payload) {
			firstOp(d).Errors = []ir.ErrorCase{{Name: ir.Naming{Source: "bad_request"}, Payload: &p}}
		}},
		{"Message.Payload", "msg/a", false, func(d *ir.Document, p ir.Payload) {
			putMessage(d, func(m *ir.Message) { m.Payload = p })
		}},
	}
}

// TestValidate_EncodingKeyAddressesNoProperty plants an encoding key naming a
// property that exists nowhere, in each field that carries a Payload.
//
// A key is a PropID and a property is a position inside its model, so no registry
// resolves one and the type-driven reference walk cannot reach it: before
// checkEncodingKeys every case here validated clean.
func TestValidate_EncodingKeyAddressesNoProperty(t *testing.T) {
	t.Parallel()
	for _, tc := range encodingCarriers() {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			tc.plant(doc, multipartPayload(map[ir.PropID]ir.PartEncoding{"p/m/ghost": {Multi: true}}))

			found := withCode(validated(t, doc), "ir/encoding-key-unknown-property")
			require.Len(t, found, 1, "exactly the planted key must address nothing")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			at := tc.at + "/contents/0/encoding/p/m/ghost"
			assert.Equal(t, at, found[0].Provenance.Node)
			assert.Equal(t, `encoding key "p/m/ghost" at `+at+
				` addresses no property of the content's type "t/m"`, found[0].Message,
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
		tc.plant(doc, multipartPayload(map[ir.PropID]ir.PartEncoding{"p/m/a": {Multi: true}}))
	}
	assert.Empty(t, validated(t, doc))
}

// TestValidate_EncodingKeyThroughServiceCommonErrors covers the second position
// an ir.ErrorCase hangs from. The carrier table above plants into an operation's
// own Errors, which is one of two lists of the same node: a check reaching only
// that one would resolve a service-level error's encoding keys against nothing
// while every case above stayed green.
func TestValidate_EncodingKeyThroughServiceCommonErrors(t *testing.T) {
	t.Parallel()
	doc := validDoc()
	payload := multipartPayload(map[ir.PropID]ir.PartEncoding{"p/m/ghost": {Multi: true}})
	service(doc).CommonErrors = []ir.ErrorCase{{Name: ir.Naming{Source: "throttled"}, Payload: &payload}}

	found := withCode(validated(t, doc), "ir/encoding-key-unknown-property")
	require.Len(t, found, 1, "exactly the planted key must address nothing")
	assert.Equal(t, "s/commonErrors/0/contents/0/encoding/p/m/ghost",
		found[0].Provenance.Node,
		"the pointer names the service by ID, as checkServerIndices does, so one node "+
			"does not have two spellings from one package")
}

// TestValidate_EncodingKeyThroughComposition covers the three ways a body model
// composes a part in. An emitter renders the flat property set (§4.3), so a key
// naming an inherited part is legal IR and the check must stay silent on it —
// resolving against the model's own properties alone would fail every one of
// these.
func TestValidate_EncodingKeyThroughComposition(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		compose func(*ir.Model)
	}{
		{"base", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "t/parent"} }},
		{"implements", func(m *ir.Model) { m.Implements = []ir.TypeRef{{Target: "t/parent"}} }},
		{"mixins", func(m *ir.Model) { m.Mixins = []ir.TypeRef{{Target: "t/parent"}} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			doc.Types["t/parent"] = &ir.Model{
				ID:         "t/parent",
				Properties: []ir.Property{{ID: "p/parent/x", WireName: "x", Type: ir.TypeRef{Target: "t/prim/string"}}},
			}
			tc.compose(model(doc))
			requestContent(doc).Encoding = map[ir.PropID]ir.PartEncoding{"p/parent/x": {Multi: true}}

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
	model(doc).Base = &ir.TypeRef{Target: "t/cycle"}
	doc.Types["t/cycle"] = &ir.Model{
		ID:         "t/cycle",
		Properties: []ir.Property{{ID: "p/cycle/x", WireName: "x", Type: ir.TypeRef{Target: "t/prim/string"}}},
		Base:       &ir.TypeRef{Target: "t/m"},
	}
	requestContent(doc).Encoding = map[ir.PropID]ir.PartEncoding{"p/cycle/x": {Multi: true}}

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
		{"an alias over the body model", "t/m"},
		{"an alias over an alias over it", "t/alias/inner"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			doc.Types["t/alias/inner"] = &ir.Scalar{
				ID:   "t/alias/inner",
				Base: &ir.TypeRef{Target: "t/m"},
			}
			doc.Types["t/alias/outer"] = &ir.Scalar{
				ID:   "t/alias/outer",
				Base: &ir.TypeRef{Target: tc.base},
			}
			c := requestContent(doc)
			c.Type = ir.TypeRef{Target: "t/alias/outer"}
			c.Encoding = map[ir.PropID]ir.PartEncoding{"p/m/a": {Multi: true}}

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
	doc.Types["t/alias/a"] = &ir.Scalar{
		ID:   "t/alias/a",
		Base: &ir.TypeRef{Target: "t/alias/b"},
	}
	doc.Types["t/alias/b"] = &ir.Scalar{
		ID:   "t/alias/b",
		Base: &ir.TypeRef{Target: "t/cycle"},
	}
	doc.Types["t/cycle"] = &ir.Model{
		ID:         "t/cycle",
		Properties: []ir.Property{{ID: "p/cycle/x", WireName: "x", Type: ir.TypeRef{Target: "t/prim/string"}}},
		Base:       &ir.TypeRef{Target: "t/alias/a"}, // closes the cycle back through the aliases.
	}
	c := requestContent(doc)
	c.Type = ir.TypeRef{Target: "t/alias/a"}
	c.Encoding = map[ir.PropID]ir.PartEncoding{"p/cycle/x": {Multi: true}}

	assert.Empty(t, withCode(validated(t, doc), "ir/encoding-key-unknown-property"))
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
		{"a type that is no model", "t/prim/string"},
		{"an opaque alias over nothing", "t/opaque"},
		{"a typed nil registry entry", "t/nil"},
		{"no type at all", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := validDoc()
			doc.Types["t/nil"] = (*ir.Model)(nil) // seeded for every case; only one reaches it.
			doc.Types["t/opaque"] = &ir.Scalar{ID: "t/opaque"}
			c := requestContent(doc)
			c.Type = ir.TypeRef{Target: tc.target}
			c.Encoding = map[ir.PropID]ir.PartEncoding{"p/m/a": {Multi: true}}

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
	c := requestContent(doc)
	c.Type = ir.TypeRef{Target: "t/missing"}
	c.Encoding = map[ir.PropID]ir.PartEncoding{"p/m/a": {Multi: true}}

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
func encodingKeys(t *testing.T, doc *ir.Document) []ir.PropID {
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
	requestContent(doc).Encoding = map[ir.PropID]ir.PartEncoding{
		"p/m/z": {}, "p/m/b": {}, "p/m/q": {}, "p/m/c": {}, "p/m/m": {},
	}
	want := pointers(withCode(validated(t, doc), "ir/encoding-key-unknown-property"))
	require.Equal(t, []string{
		"op/request/contents/0/encoding/p/m/b",
		"op/request/contents/0/encoding/p/m/c",
		"op/request/contents/0/encoding/p/m/m",
		"op/request/contents/0/encoding/p/m/q",
		"op/request/contents/0/encoding/p/m/z",
	}, want)
	for range 8 {
		assert.Equal(t, want, pointers(withCode(validated(t, doc), "ir/encoding-key-unknown-property")))
	}
}

// payloadType is the node whose carriers payloadFields enumerates.
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
	for _, c := range carriers {
		listed = append(listed, c.field)
	}
	slices.Sort(listed)

	found := payloadFields(t)
	require.NotEmpty(t, found, "the shape holds no ir.Payload field at all, so the read reached "+
		"nothing and proves nothing about the ones encodingCarriers names")
	assert.Empty(t, cmp.Diff(found, listed),
		"encodingCarriers must name every ir field that carries an ir.Payload, once each "+
			"(-declared +listed); a new one also has to be walked by forEachPayload")
}

// payloadFields returns "Owner.Field", sorted, for every struct field the IR
// declares whose type is or contains an ir.Payload. It reads the fields off the
// JSON shape ir.Document reaches, which lists a sealed sum's variants from the
// ir sources, so a carrier is covered the day it is declared.
func payloadFields(t *testing.T) []string {
	t.Helper()
	p, err := decls.Load(payloadType.PkgPath())
	require.NoError(t, err)
	shape, err := jsonshape.Read(p, "Document")
	require.NoError(t, err)

	var found []string
	for _, typ := range shape.Types {
		for _, m := range typ.Members {
			if carriesPayload(t, m.Field.Type(), 0) {
				found = append(found, typ.Named.Obj().Name()+"."+m.Field.Name())
			}
		}
	}
	slices.Sort(found)
	return found
}

// carriesPayload reports whether a field's type is an ir.Payload, or a pointer,
// slice, array or map of one.
//
// It does not descend into structs. A field whose type merely reaches a Payload
// further down is the spine leading to a carrier — Document.Services and
// Document.Messages between them reach every one of today's — and naming the
// spine would name most of the IR.
func carriesPayload(t *testing.T, typ types.Type, depth int) bool {
	t.Helper()
	require.Less(t, depth, maxFieldTypeDepth, "resolving a field type exceeded %d steps", maxFieldTypeDepth)
	if named, isNamed := types.Unalias(typ).(*types.Named); isNamed {
		obj := named.Obj()
		if obj.Pkg() != nil && obj.Pkg().Path() == payloadType.PkgPath() && obj.Name() == payloadType.Name() {
			return true
		}
	}
	switch u := typ.Underlying().(type) {
	case *types.Pointer:
		return carriesPayload(t, u.Elem(), depth+1)
	case *types.Slice:
		return carriesPayload(t, u.Elem(), depth+1)
	case *types.Array:
		return carriesPayload(t, u.Elem(), depth+1)
	case *types.Map:
		return carriesPayload(t, u.Key(), depth+1) || carriesPayload(t, u.Elem(), depth+1)
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
		payload := multipartPayload(map[ir.PropID]ir.PartEncoding{"p/m/ghost": {Multi: true}})
		def := defaultResponse()
		def.Payload = &payload
		firstOp(doc).Default = def

		found := withCode(validated(t, doc), "ir/encoding-key-unknown-property")
		require.Len(t, found, 1)
		assert.Equal(t, ir.SeverityError, found[0].Severity)
		at := "op/default/contents/0/encoding/p/m/ghost"
		assert.Equal(t, at, found[0].Provenance.Node)
		assert.Equal(t, `encoding key "p/m/ghost" at `+at+
			` addresses no property of the content's type "t/m"`, found[0].Message)
	})

	t.Run("a payload keyed by a real property is clean", func(t *testing.T) {
		t.Parallel()
		doc := validDoc()
		payload := multipartPayload(map[ir.PropID]ir.PartEncoding{"p/m/a": {Multi: true}})
		def := defaultResponse()
		def.Payload = &payload
		firstOp(doc).Default = def

		assert.Empty(t, validated(t, doc))
	})
}
