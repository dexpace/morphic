package openapi

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
// fails to serialize, lowerMeta's "if len(ext) > 0" guard is false, so the
// warning must already have been recorded by l.extensions rather than appended
// inside the guard — the shape that used to drop it silently.
func TestMeta_UnserializableExtensionStillWarns(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
x-bad: {1: intkey}
`
	doc, diags := parseFull(t, spec)
	assert.True(t, hasDiagAt(diags, codeDegradedConstruct, ir.SeverityWarning),
		"an entirely unserializable top-level extension still warns even though Extensions ends up empty")
	assert.Empty(t, doc.Extensions, "the unserializable extension is dropped, not stored")
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
	assert.NotEmpty(t, doc.Extensions, "top-level x-* extension")
	require.Len(t, doc.Servers, 1)
	assert.Equal(t, "primary", doc.Servers[0].Name.Source, "3.2 server name")
	require.Len(t, doc.Servers[0].Variables, 1)
	assert.Equal(t, []string{"us", "eu"}, doc.Servers[0].Variables[0].Enum)
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
}

func TestLowerServers_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Servers: []*soa.Server{nil, {URL: "https://x.example.com"}}}
	l := newRawLowerer(doc)
	l.lowerServers()
	require.Len(t, l.out.Servers, 1, "nil server entry skipped, valid one lowered")
	assert.Equal(t, "https://x.example.com", l.out.Servers[0].URLTemplate)
}

func TestServerVariables_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	vars := sequencedmap.New(
		sequencedmap.NewElem("skip", (*soa.ServerVariable)(nil)),
		sequencedmap.NewElem("keep", &soa.ServerVariable{}),
	)
	srv := lowerServer(&soa.Server{URL: "https://x", Variables: vars})
	require.Len(t, srv.Variables, 1, "nil variable entry skipped")
	assert.Equal(t, "keep", srv.Variables[0].Name)
}
