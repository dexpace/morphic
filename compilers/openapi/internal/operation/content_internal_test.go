package operation

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/engine/reader"
	"github.com/dexpace/morphic/ir"
)

// TestBodyModelPointer_NoModelBehindBody covers the walk's dead ends on a
// hand-built registry: an ID nothing declares, an opaque scalar, a kind that is
// no model, and an alias chain that closes on itself. The last is what the bound
// is for: a $ref cycle compiles rather than being refused at load, so nothing
// upstream keeps a chain from closing, and a walk that relied on that would loop
// forever.
func TestBodyModelPointer_NoModelBehindBody(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	l.types.Register("t/opaque", &ir.Scalar{ID: "t/opaque"})
	l.types.Register("t/cycle/a", &ir.Scalar{
		ID: "t/cycle/a", Base: &ir.TypeRef{Target: "t/cycle/b"},
	})
	l.types.Register("t/cycle/b", &ir.Scalar{
		ID: "t/cycle/b", Base: &ir.TypeRef{Target: "t/cycle/a"},
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
			model, ok := bodyModel(l.types, tc.body)
			assert.False(t, ok, "no model stands behind this body")
			assert.Zero(t, model, "and no position is invented for one")
		})
	}
}

// refSite loads a source declaring a component Ref whose $ref spells ref, and
// the object schema Form it may name, and returns the lowerer over it with the
// schema Ref: the position bodySchemaPointer is asked about.
func refSite(t *testing.T, ref string) (*lowerer, *oas3.JSONSchema[oas3.Referenceable]) {
	t.Helper()
	l, _ := loweredFor(t, openapitest.ComponentSpec("    Form: {type: object}\n    Ref: {$ref: '"+ref+"'}\n"))
	return l, refComponent(t, l)
}

// keyed is where bodySchemaPointer keys an encoding entry: a document and a
// pointer there.
type keyed struct {
	doc     ids.Document
	pointer jsontext.Pointer
}

// local is the source's "/local", the pointer every keyedAt below falls back to.
var local = keyed{pointer: "/local"}

// keyedAt is bodySchemaPointer's answer for js under c, falling back to
// "/local", as one comparable value.
func keyedAt(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) keyed {
	d, p := bodySchemaPointer(c, js, "/local")
	return keyed{doc: d, pointer: p}
}

// refComponent returns the component Ref of the document l holds.
func refComponent(t *testing.T, l *lowerer) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	js, ok := l.ctx.Doc.Components.Schemas.Get("Ref")
	require.True(t, ok, "the fixture declares component Ref")
	return js
}

// TestBodySchemaPointer_LocalRefFragment pins the fallback's own ref hop, which
// the production path reaches only for a body standing for no model: a $ref the
// compiler resolved to a position in the source is keyed by that position.
func TestBodySchemaPointer_LocalRefFragment(t *testing.T) {
	t.Parallel()
	l, js := refSite(t, "#/components/schemas/Form")
	assert.Equal(t, keyed{pointer: "/components/schemas/Form"}, keyedAt(l.ctx, js))
}

// TestBodySchemaPointer_AnotherDocumentsRefKeysInThatDocument pins the document
// half of a $ref deciding the key. Cutting the fragment off blindly turned
// another document's property into an identity in this one, silently naming
// whichever local schema shared the path. A document that is read keys the
// entry in that document, whose IDs it qualifies; one that is not read, or
// that the compile does not list, lands nowhere, so the local pointer stands.
func TestBodySchemaPointer_AnotherDocumentsRefKeysInThatDocument(t *testing.T) {
	t.Parallel()
	t.Run("not read", func(t *testing.T) {
		t.Parallel()
		l, js := refSite(t, "./ext-form.yaml#/components/schemas/Form")
		assert.Equal(t, local, keyedAt(l.ctx, js),
			"a fragment from another document must not become a pointer into this one")
	})

	t.Run("read", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ext-form.yaml"),
			[]byte("components:\n  schemas:\n    Form: {type: object}\n"), 0o600))
		src := openapitest.ComponentSpec("    Form: {type: object}\n    Ref: {$ref: './ext-form.yaml#/components/schemas/Form'}\n")
		l, _ := loweredAt(t, compilers.Source{Path: filepath.Join(dir, "spec.yaml"), Data: []byte(src)},
			load.Options{Reader: reader.Disk{}})
		js := refComponent(t, l)
		e, ok := l.ctx.Bindings().Entry(js)
		require.True(t, ok)
		require.Equal(t, bind.Resolved, e.Hop.Status, "the $ref lands in the other document")

		assert.Equal(t, keyed{doc: ids.In("ext-form.yaml"), pointer: "/components/schemas/Form"}, keyedAt(l.ctx, js),
			"the other document's position, in its own IDs")

		unlisted := l.ctx.WithLoaded(&load.Document{Doc: l.ctx.Doc, Bindings: l.ctx.Bindings()})
		assert.Equal(t, local, keyedAt(unlisted, js),
			"a landing in a document the compile did not list keeps the local pointer")
	})
}

// TestBodySchemaPointer_SelfNamedRefFollowsFragment pins the other half: a ref
// spelling this document's own filename is internal, so its fragment is a
// pointer here after all.
func TestBodySchemaPointer_SelfNamedRefFollowsFragment(t *testing.T) {
	t.Parallel()
	l, js := refSite(t, "spec.yaml#/components/schemas/Form")
	assert.Equal(t, keyed{pointer: "/components/schemas/Form"}, keyedAt(l.ctx, js))
}

// TestBodySchemaPointer_NonUTF8FragmentStaysLocal pins another reason a $ref
// falls back to the local pointer: a fragment that decodes to bytes that are
// not UTF-8 can never resolve, because no document key can spell them. Reading
// it anyway would have carried the raw byte into a PropID the IR cannot encode
// (GitHub #520).
func TestBodySchemaPointer_NonUTF8FragmentStaysLocal(t *testing.T) {
	t.Parallel()
	l, js := refSite(t, "#/components/schemas/%FF")
	assert.Equal(t, local, keyedAt(l.ctx, js), "a fragment that is not UTF-8 must not become a pointer")
}

// TestBodySchemaPointer_UnresolvedRefStaysLocal pins the last reason: a $ref
// naming nothing lands nowhere, so it keys no encoding entry by a position the
// document does not hold. A scope with no bindings lands none either.
func TestBodySchemaPointer_UnresolvedRefStaysLocal(t *testing.T) {
	t.Parallel()
	l, js := refSite(t, "#/components/schemas/Missing")
	assert.Equal(t, local, keyedAt(l.ctx, js))

	bare := newRawLowerer(&soa.OpenAPI{})
	assert.Equal(t, local, keyedAt(bare.ctx, js), "a context that was given no bindings resolves no $ref")
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
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
		"nothing was kept, so nothing announces that it was")
}

// TestBodySchemaPointer_ExternalRefNoFragment pins that a $ref naming another
// document with no fragment falls back to the local pointer.
func TestBodySchemaPointer_ExternalRefNoFragment(t *testing.T) {
	t.Parallel()
	l, js := refSite(t, "external.yaml")
	assert.Equal(t, local, keyedAt(l.ctx, js), "a fragmentless ref falls back to the local pointer")
}

func TestBodySchemaPointer_NilSchema(t *testing.T) {
	t.Parallel()
	l := newRawLowerer(&soa.OpenAPI{})
	assert.Equal(t, local, keyedAt(l.ctx, nil))
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
	l.types.Register("t/opaque", &ir.Scalar{ID: "t/opaque"})
	l.types.Register("t/cycle/a", &ir.Scalar{
		ID: "t/cycle/a", Base: &ir.TypeRef{Target: "t/cycle/b"},
	})
	l.types.Register("t/cycle/b", &ir.Scalar{
		ID: "t/cycle/b", Base: &ir.TypeRef{Target: "t/cycle/a"},
	})
	l.types.Register("t/model/cycle", &ir.Model{
		ID: "t/model/cycle", Base: &ir.TypeRef{Target: "t/model/cycle"},
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

// TestBodyParts_DepthBound covers the composition walk's bound. A $ref cycle
// compiles rather than being refused at load, so the bound is what keeps the
// walk terminating whatever the document holds.
func TestBodyParts_DepthBound(t *testing.T) {
	t.Parallel()
	js := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})
	assert.Nil(t, bodyParts(nil, js, maxPartCompositionDepth+1),
		"past the bound the walk stops rather than descending further")
}
