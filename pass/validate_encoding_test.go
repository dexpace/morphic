package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// multipartContent is a body content typed by validDoc's model, carrying enc.
func multipartContent(enc map[ir.PropID]ir.PartEncoding) ir.Content {
	return ir.Content{MediaType: "multipart/form-data", Type: ir.TypeRef{Target: "t/m"}, Encoding: enc}
}

// encodingCarrier is one field that carries a Payload — and so an Encoding map —
// named as the ir field it is, and paired with the location a diagnostic about
// its first content must point at.
type encodingCarrier struct {
	field string
	at    string
	plant func(doc *ir.Document, enc map[ir.PropID]ir.PartEncoding)
}

// encodingCarriers enumerates the Payload-bearing fields checkEncodingKeys walks.
// TestEncodingCarriers_NameEveryPayloadFieldInTheIR holds this list against the
// IR and the cases below hold checkEncodingKeys against this list, so a carrier
// added to the IR has to reach both.
func encodingCarriers() []encodingCarrier {
	return []encodingCarrier{
		{"Operation.Request", "op/request/contents/0", func(d *ir.Document, enc map[ir.PropID]ir.PartEncoding) {
			requestContent(d).Encoding = enc
		}},
		{"Response.Payload", "op/responses/0/contents/0", func(d *ir.Document, enc map[ir.PropID]ir.PartEncoding) {
			firstOp(d).Responses = []ir.Response{{
				Name:    ir.Naming{Source: "ok"},
				Payload: &ir.Payload{Contents: []ir.Content{multipartContent(enc)}},
			}}
		}},
		{"Message.Payload", "msg/a/contents/0", func(d *ir.Document, enc map[ir.PropID]ir.PartEncoding) {
			putMessage(d, func(m *ir.Message) {
				m.Payload = ir.Payload{Contents: []ir.Content{multipartContent(enc)}}
			})
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
			tc.plant(doc, map[ir.PropID]ir.PartEncoding{"p/m/ghost": {Multi: true}})

			found := withCode(pass.Validate(doc), "ir/encoding-key-unknown-property")
			require.Len(t, found, 1, "exactly the planted key must address nothing")
			assert.Equal(t, ir.SeverityError, found[0].Severity)
			assert.Equal(t, tc.at+"/encoding/p/m/ghost", found[0].Provenance.Pointer)
			assert.Equal(t, `encoding key "p/m/ghost" at `+tc.at+
				`/encoding/p/m/ghost addresses no property of the content's type "t/m"`, found[0].Message,
				"the message names the key, where it sits, and what it failed to address")
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
		tc.plant(doc, map[ir.PropID]ir.PartEncoding{"p/m/a": {Multi: true}})
	}
	assert.Empty(t, pass.Validate(doc))
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
				TypeCommon: ir.TypeCommon{ID: "t/parent"},
				Properties: []ir.Property{{ID: "p/parent/x", WireName: "x", Type: ir.TypeRef{Target: "t/prim/string"}}},
			}
			tc.compose(model(doc))
			requestContent(doc).Encoding = map[ir.PropID]ir.PartEncoding{"p/parent/x": {Multi: true}}

			assert.Empty(t, withCode(pass.Validate(doc), "ir/encoding-key-unknown-property"))
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
		TypeCommon: ir.TypeCommon{ID: "t/cycle"},
		Properties: []ir.Property{{ID: "p/cycle/x", WireName: "x", Type: ir.TypeRef{Target: "t/prim/string"}}},
		Base:       &ir.TypeRef{Target: "t/m"},
	}
	requestContent(doc).Encoding = map[ir.PropID]ir.PartEncoding{"p/cycle/x": {Multi: true}}

	assert.Empty(t, withCode(pass.Validate(doc), "ir/encoding-key-unknown-property"))
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
				TypeCommon: ir.TypeCommon{ID: "t/alias/inner"},
				Base:       &ir.TypeRef{Target: "t/m"},
			}
			doc.Types["t/alias/outer"] = &ir.Scalar{
				TypeCommon: ir.TypeCommon{ID: "t/alias/outer"},
				Base:       &ir.TypeRef{Target: tc.base},
			}
			c := requestContent(doc)
			c.Type = ir.TypeRef{Target: "t/alias/outer"}
			c.Encoding = map[ir.PropID]ir.PartEncoding{"p/m/a": {Multi: true}}

			assert.Empty(t, withCode(pass.Validate(doc), "ir/encoding-key-unknown-property"),
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
		TypeCommon: ir.TypeCommon{ID: "t/alias/a"},
		Base:       &ir.TypeRef{Target: "t/alias/b"},
	}
	doc.Types["t/alias/b"] = &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/alias/b"},
		Base:       &ir.TypeRef{Target: "t/cycle"},
	}
	doc.Types["t/cycle"] = &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/cycle"},
		Properties: []ir.Property{{ID: "p/cycle/x", WireName: "x", Type: ir.TypeRef{Target: "t/prim/string"}}},
		Base:       &ir.TypeRef{Target: "t/alias/a"}, // closes the cycle back through the aliases.
	}
	c := requestContent(doc)
	c.Type = ir.TypeRef{Target: "t/alias/a"}
	c.Encoding = map[ir.PropID]ir.PartEncoding{"p/cycle/x": {Multi: true}}

	assert.Empty(t, withCode(pass.Validate(doc), "ir/encoding-key-unknown-property"))
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
			doc.Types["t/opaque"] = &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}}
			c := requestContent(doc)
			c.Type = ir.TypeRef{Target: tc.target}
			c.Encoding = map[ir.PropID]ir.PartEncoding{"p/m/a": {Multi: true}}

			found := withCode(pass.Validate(doc), "ir/encoding-key-unknown-property")
			require.Len(t, found, 1, "a type exposing no property resolves no key")
			assert.Contains(t, found[0].Message,
				`addresses no property of the content's type "`+string(tc.target)+`"`)
		})
	}
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
			assert.Empty(t, withCode(pass.Validate(doc), "ir/encoding-key-unknown-property"))
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
	keys := make([]ir.PropID, 0, len(req.Contents[0].Encoding))
	for key := range req.Contents[0].Encoding {
		keys = append(keys, key)
	}
	return keys
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
	want := pointers(withCode(pass.Validate(doc), "ir/encoding-key-unknown-property"))
	require.Equal(t, []string{
		"op/request/contents/0/encoding/p/m/b",
		"op/request/contents/0/encoding/p/m/c",
		"op/request/contents/0/encoding/p/m/m",
		"op/request/contents/0/encoding/p/m/q",
		"op/request/contents/0/encoding/p/m/z",
	}, want)
	for range 8 {
		assert.Equal(t, want, pointers(withCode(pass.Validate(doc), "ir/encoding-key-unknown-property")))
	}
}
