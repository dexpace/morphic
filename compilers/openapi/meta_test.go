package openapi

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
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
	assert.True(t, openapitest.CountDiagsAt(diags, diag.DegradedConstruct, ir.SeverityWarning) > 0,
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
	assert.Equal(t, "/tags/1", got[0].Owner)
}

// TestTagUnknownSites_NilEntrySkipped is TestTagExtensions_NilEntrySkipped's
// twin on the census side: the two walks of the tag list keep the same guard, so
// neither can be the one that dereferences a nil entry or keys a site at an
// index holding no tag.
func TestTagUnknownSites_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Tags: []*soa.Tag{nil, {Name: "kept"}}}

	got := tagUnknownSites(lowering.Ctx{Doc: doc})

	require.Len(t, got, 1, "only the surviving tag contributes a census site")
	assert.Equal(t, "tags/1", got[0].scope, "the site is keyed at the tag's own index, not its position")
	assert.Equal(t, "/tags/1", got[0].owner)
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

// TestServerName_DerivedFromURLWhenUnnamed pins how a server is named when the
// source declares no name for it, which before OpenAPI 3.2 it has no way to do.
// Every such server used to reach the IR with all three name channels empty,
// leaving an emitter that renders one client per server nothing to name them by
// (GitHub #258).
func TestServerName_DerivedFromURLWhenUnnamed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		url  string
		want ir.Naming
	}{
		{"absolute url with a template variable", "https://{env}.example.com/v1",
			ir.Naming{Hint: "https_env_example_com_v_1"}},
		{"relative url", "/v2", ir.Naming{Hint: "v_2"}},
		{"root url has no word in it", "/", ir.Naming{Hint: "server"}},
		{"no url at all", "", ir.Naming{Hint: "server"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, serverName(&soa.Server{URL: tc.url}))
		})
	}
}

// TestServerName_DistinguishesServersDifferingOnlyInPath is why the hint is the
// whole URL template and not the host inside it: one host serving two API
// versions is an ordinary OpenAPI document, and a host-derived hint would name
// both servers the same.
func TestServerName_DistinguishesServersDifferingOnlyInPath(t *testing.T) {
	t.Parallel()
	v1 := serverName(&soa.Server{URL: "https://api.example.com/v1"})
	v2 := serverName(&soa.Server{URL: "https://api.example.com/v2"})
	assert.NotEqual(t, v1.Hint, v2.Hint, "two distinct servers get two distinct hints")
}

// TestServerName_CollidesOnPunctuationAlone bounds that claim, which must not be
// read as "distinct servers get distinct hints": canonicalizing drops every
// non-word character, so two URLs differing only in punctuation reduce to one
// word sequence. Uniquifying it is an emitter's job, and pinning it keeps that a
// known bound rather than a later discovery.
func TestServerName_CollidesOnPunctuationAlone(t *testing.T) {
	t.Parallel()
	dotted := serverName(&soa.Server{URL: "https://api.example.com/v1"})
	dashed := serverName(&soa.Server{URL: "https://api.example.com/v-1"})
	assert.Equal(t, dotted.Hint, dashed.Hint,
		"neutral words carry no punctuation, so these two collide")
}

func TestLowerServers_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Servers: []*soa.Server{nil, {URL: "https://x.example.com"}}}
	got, diags := lowerServers(lowering.Ctx{Doc: doc})

	assert.Empty(t, diags)
	require.Len(t, got, 1, "nil server entry skipped, valid one lowered")
	assert.Equal(t, "https://x.example.com", got[0].URLTemplate)
}

func TestServerVariables_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	vars := sequencedmap.New(
		sequencedmap.NewElem("skip", (*soa.ServerVariable)(nil)),
		sequencedmap.NewElem("keep", &soa.ServerVariable{}),
	)
	srv, diags := lowerServer(lowering.Ctx{}, &soa.Server{URL: "https://x", Variables: vars}, "/servers/0")
	assert.Empty(t, diags)
	require.Len(t, srv.Variables, 1, "nil variable entry skipped")
	assert.Equal(t, "keep", srv.Variables[0].Name)
}

// TestLowerServers_EveryEntrySkippedIsNil pins the same guard on the servers
// side: when no entry survives, the field stays unset rather than becoming an
// empty list the source never declared.
func TestLowerServers_EveryEntrySkippedIsNil(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Servers: []*soa.Server{nil, nil}}

	got, diags := lowerServers(lowering.Ctx{Doc: doc})
	assert.Nil(t, got)
	assert.Empty(t, diags)
}
