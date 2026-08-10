package operation

import (
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestBodyModelPointer_NoModelBehindBody covers the walk's dead ends on a
// hand-built registry: an ID nothing declares, an opaque scalar, a kind that is
// no model, and an alias chain that closes on itself. The last is what the bound
// is for — a $ref cycle is refused at load, so no document can spell one, and a
// walk that relied on that would loop forever the day one arrived by another
// route.
func TestBodyModelPointer_NoModelBehindBody(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.types.Register("t/opaque", &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}})
	l.types.Register("t/cycle/a", &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/cycle/a"}, Base: &ir.TypeRef{Target: "t/cycle/b"},
	})
	l.types.Register("t/cycle/b", &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/cycle/b"}, Base: &ir.TypeRef{Target: "t/cycle/a"},
	})
	prim := l.types.PrimID(ir.PrimString)

	cases := []struct {
		name string
		body ir.TypeID
	}{
		{"an ID no registry entry declares", "t/absent"},
		{"an opaque scalar standing for nothing", "t/opaque"},
		{"a kind that declares no properties", prim},
		{"an alias chain that closes on itself", "t/cycle/a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pointer, ok := bodyModelPointer(l.types, tc.body)
			assert.False(t, ok, "no model stands behind this body")
			assert.Empty(t, pointer, "and no pointer is invented for one")
		})
	}
}

// TestBodySchemaPointer_LocalRefFragment pins the fallback's own ref hop, which
// the production path reaches only for a body standing for no model.
func TestBodySchemaPointer_LocalRefFragment(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	js := oas3.NewJSONSchemaFromReference("#/components/schemas/Form")
	assert.Equal(t, "/components/schemas/Form", bodySchemaPointer(l.ctx, js, "/local"))
}

// TestBodySchemaPointer_ForeignDocumentRefStaysLocal pins the document half of a
// $ref deciding the pointer. Cutting the fragment off blindly turned another
// document's property into an identity in this one, silently naming whichever
// local schema shared the path.
func TestBodySchemaPointer_ForeignDocumentRefStaysLocal(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.ctx.Source = ir.SourceInfo{Path: "spec.yaml"}
	js := oas3.NewJSONSchemaFromReference("./ext-form.yaml#/components/schemas/Form")
	assert.Equal(t, "/local", bodySchemaPointer(l.ctx, js, "/local"),
		"a fragment from another document must not become a pointer into this one")
}

// TestBodySchemaPointer_SelfNamedRefFollowsFragment pins the other half: a ref
// spelling this document's own filename is internal, so its fragment is a
// pointer here after all.
func TestBodySchemaPointer_SelfNamedRefFollowsFragment(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.ctx.Source = ir.SourceInfo{Path: "spec.yaml"}
	js := oas3.NewJSONSchemaFromReference("spec.yaml#/components/schemas/Form")
	assert.Equal(t, "/components/schemas/Form", bodySchemaPointer(l.ctx, js, "/local"))
}

func TestContentTypeKeys_Nil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, contentTypeKeys(nil))
}

func TestFillSequential_EmptyItemEncoding(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	content := &ir.Content{}
	media := &soa.MediaType{ItemEncoding: &soa.Encoding{}}
	diags := fillSequential(l.ctx, l.types, &l.anchors, content, media, "/mp", "h")
	assert.Equal(t, &ir.PartEncoding{Multi: true}, content.ItemEncoding,
		"a config-free itemEncoding still records that the tail repeats")
	assert.Nil(t, content.Unmodeled, "nothing is preserved raw")
	assert.Empty(t, diags)
}

func TestEncodingConfig_NilEncoding(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	pe, unmodeled, diags := encodingConfig(l.ctx, l.types, &l.anchors, nil, "/mp", "itemEncoding")
	assert.Equal(t, ir.PartEncoding{}, pe)
	assert.Nil(t, unmodeled)
	assert.Empty(t, diags)
}

// TestPositionalEncoding_WithoutRootNode covers the media type whose source node
// cannot be read. prefixEncoding is declared — nothing else routes a content
// entry here — so nothing being written is a construct lost rather than one that
// was never there, and the position reports that instead of announcing a
// preservation it did not make (GitHub #144).
func TestPositionalEncoding_WithoutRootNode(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	content := &ir.Content{}
	media := &soa.MediaType{PrefixEncoding: []*soa.Encoding{{}}, ItemEncoding: &soa.Encoding{}}
	diags := fillSequential(l.ctx, l.types, &l.anchors, content, media, "/mp", "h")
	assert.Nil(t, content.ItemEncoding, "prefixes still block the every-item lowering")
	assert.Nil(t, content.Unmodeled, "a media type with no source node has nothing verbatim to keep")
	openapitest.AssertHasCode(t, diags, diag.UnpreservableConstruct, ir.SeverityError)
	assert.False(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo) > 0,
		"nothing was kept, so nothing announces that it was")
}

func TestBodySchemaPointer_ExternalRefNoFragment(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	js := oas3.NewJSONSchemaFromReference("external.yaml")
	assert.Equal(t, "/local", bodySchemaPointer(l.ctx, js, "/local"), "a fragmentless ref falls back to the local pointer")
}

func TestBodySchemaPointer_NilSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	assert.Equal(t, "/local", bodySchemaPointer(l.ctx, nil, "/local"))
}

func TestLowerPayload_NilMediaEntriesYieldNil(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	content := sequencedmap.New(
		sequencedmap.NewElem("application/json", (*soa.MediaType)(nil)),
	)
	payload, diags := lowerPayload(l.ctx, l.types, &l.anchors, content, "/p", "hint")
	assert.Nil(t, payload, "all-nil media map yields no payload")
	assert.Empty(t, diags)
}

// TestPropIDByWire_DeadEnds mirrors TestBodyModelPointer_NoModelBehindBody for
// the property walk: the same dead ends, asked for a part name rather than a
// pointer. Neither may invent an ID — a part keyed by a PropID nothing declares
// is a dangling reference the encoding map would carry silently.
func TestPropIDByWire_DeadEnds(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.types.Register("t/opaque", &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}})
	l.types.Register("t/cycle/a", &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/cycle/a"}, Base: &ir.TypeRef{Target: "t/cycle/b"},
	})
	l.types.Register("t/cycle/b", &ir.Scalar{
		TypeCommon: ir.TypeCommon{ID: "t/cycle/b"}, Base: &ir.TypeRef{Target: "t/cycle/a"},
	})
	l.types.Register("t/model/cycle", &ir.Model{
		TypeCommon: ir.TypeCommon{ID: "t/model/cycle"}, Base: &ir.TypeRef{Target: "t/model/cycle"},
	})
	prim := l.types.PrimID(ir.PrimString)

	cases := []struct {
		name string
		body ir.TypeID
	}{
		{"an ID no registry entry declares", "t/absent"},
		{"an opaque scalar standing for nothing", "t/opaque"},
		{"a kind that declares no properties", prim},
		{"an alias chain that closes on itself", "t/cycle/a"},
		{"a model whose base closes on itself", "t/model/cycle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, ok := propIDByWire(l.types, tc.body, "file", 0)
			assert.False(t, ok, "no property with that wire name stands behind this body")
			assert.Empty(t, id, "and no ID is invented for one")
		})
	}
}

// TestBodyParts_DepthBound covers the composition walk's bound. A $ref cycle is
// refused at load, so no document can spell a composition this deep; the bound is
// what keeps the walk terminating without relying on that.
func TestBodyParts_DepthBound(t *testing.T) {
	t.Parallel()
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
	assert.Nil(t, bodyParts(js, maxPartCompositionDepth+1),
		"past the bound the walk stops rather than descending further")
}
