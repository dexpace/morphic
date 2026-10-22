package openapi

import (
	"encoding/json/jsontext"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

const metaSpec = `openapi: 3.2.0
info:
  title: Meta
  version: "2"
  summary: A summary
  description: A description
  contact: {name: Team, url: 'https://team', email: t@example.com}
  license: {name: Apache-2.0, identifier: Apache-2.0}
externalDocs: {url: 'https://docs', description: docs}
x-top: {a: 1}
servers:
  - url: https://{region}.example.com
    name: primary
    description: Primary
    variables:
      region: {default: us, enum: [us, eu], description: Region}
paths:
  /p:
    get: {operationId: p, responses: {"200": {description: ok}}}
`

// TestMeta_UnserializableExtensionStillWarns pins the document-level twin of
// TestAuth_UnserializableExtensionStillWarns: when every top-level x-* extension
// fails to serialize, lowerMeta ends up with an empty Unmodeled map, so the
// warning must come from the extension reader rather than from a branch guarded
// on what was kept — the shape that used to drop it silently.
func TestMeta_UnserializableExtensionStillWarns(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
x-bad: {1: intkey}
`
	doc, diags := parseFull(t, spec)
	assert.Positive(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning),
		"an entirely unserializable top-level extension still warns even though Unmodeled ends up empty")
	assert.Empty(t, doc.Unmodeled, "the unserializable extension is dropped, not stored")
}

func TestMeta_FullDocumentMetadata(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, metaSpec)
	assert.Equal(t, "Meta", doc.Name)
	assert.Equal(t, "2", doc.Version)
	require.NotNil(t, doc.Contact)
	assert.Equal(t, "t@example.com", doc.Contact.Email)
	require.NotNil(t, doc.License)
	assert.Equal(t, "Apache-2.0", doc.License.Identifier)
	assert.NotEmpty(t, doc.Docs.ExternalDocs, "root externalDocs folded into docs")
	assert.NotEmpty(t, doc.Unmodeled, "top-level x-* extension")
	require.Len(t, doc.Servers, 1)
	assert.Equal(t, ir.Naming{Source: "primary", Canonical: "primary"}, doc.Servers[0].Name,
		"a declared 3.2 name is the server's name; the URL hint is for a server with none")
	require.Len(t, doc.Servers[0].Variables, 1)
	assert.Equal(t, []string{"us", "eu"}, doc.Servers[0].Variables[0].Enum)
}

// TestTagExtensions_NilEntrySkipped pins the same guard lowerTagDefs keeps on
// the other side of the tag list: a nil entry contributes no extension site, so
// nothing dereferences it and no site is keyed at an index holding no tag.
func TestTagExtensions_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Tags: []*soa.Tag{nil, {Name: "kept"}}}

	got := tagExtensions(lowering.Ctx{Doc: doc})

	require.Len(t, got, 2, "one surviving tag contributes its own site and its externalDocs one")
	assert.Equal(t, "tags/1", got[0].Scope, "the site is keyed at the tag's own index, not its position")
	assert.Equal(t, jsontext.Pointer("/tags/1"), got[0].Owner)
}

// TestTagUnknownSites_NilEntrySkipped is TestTagExtensions_NilEntrySkipped's
// twin on the census side: the two walks of the tag list keep the same guard, so
// neither can be the one that dereferences a nil entry or keys a site at an
// index holding no tag.
func TestTagUnknownSites_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Tags: []*soa.Tag{nil, {Name: "kept"}}}

	got := tagUnknownSites(lowering.Ctx{Doc: doc})

	require.Len(t, got, 2,
		"the nil tag contributes nothing, and the surviving one contributes its own site and its externalDocs")
	assert.Equal(t, "tags/1", got[0].scope, "the site is keyed at the tag's own index, not its position")
	assert.Equal(t, jsontext.Pointer("/tags/1"), got[0].owner)
	assert.Equal(t, "tags/1/externalDocs", got[1].scope, "and its externalDocs is scoped under that same index")
	assert.Equal(t, jsontext.Pointer("/tags/1/externalDocs"), got[1].owner)
}

func TestMeta_NoInfoNoServers(t *testing.T) {
	t.Parallel()
	// With no info block the title is empty; with no servers the library injects
	// a default "/" server that is lowered.
	spec := "openapi: 3.1.0\npaths: {}\n"
	doc, _ := parseFull(t, spec)
	assert.Empty(t, doc.Name)
	require.Len(t, doc.Servers, 1)
	assert.Equal(t, "/", doc.Servers[0].URLTemplate)
	assert.Equal(t, ir.Naming{Hint: "server"}, doc.Servers[0].Name,
		"the injected server is still a server an emitter has to name (GitHub #258)")
}

// TestMeta_JSONSchemaDialectIsKept pins the document's jsonSchemaDialect, which
// nothing read: it is kept verbatim on the document with an info, as a schema's
// own `$schema` is, written directly or merged in through `<<`, and in a 3.0
// document, which it postdates, load also reports it. Declaring none keeps
// nothing.
func TestMeta_JSONSchemaDialectIsKept(t *testing.T) {
	t.Parallel()
	const dialect = "https://json-schema.org/draft/2020-12/schema"
	for _, tc := range []struct {
		name, version, line string
	}{
		{"3.1", "3.1.0", "jsonSchemaDialect: " + dialect},
		{"3.1 merged in", "3.1.0", "<<: {jsonSchemaDialect: '" + dialect + "'}"},
		{"3.0", "3.0.3", "jsonSchemaDialect: " + dialect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, "openapi: "+tc.version+"\n"+tc.line+"\ninfo: {title: T, version: \"1\"}\npaths: {}\n")
			kept, ok := doc.Unmodeled["openapi:jsonSchemaDialect"]
			require.True(t, ok, "the dialect is kept on the document")
			assert.Equal(t, ir.ReasonOutOfScope, kept.Reason)
			assert.JSONEq(t, `"`+dialect+`"`, string(kept.Value))
			openapitest.AssertInfoDiagAt(t, diags, "/jsonSchemaDialect")
			assert.Equal(t, tc.version == "3.0.3",
				openapitest.HasDiagCodeAt(diags, diag.LaterVersionConstruct, "/jsonSchemaDialect"))
		})
	}
	doc, _ := parseFull(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n")
	assert.NotContains(t, doc.Unmodeled, "openapi:jsonSchemaDialect")
}
