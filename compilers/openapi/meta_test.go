package openapi

import (
	"encoding/json/jsontext"
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

// dupServerNameMessage is the message every reported row expects. It pins all
// three parts the diagnostic must carry: the repeated name, the pointer of the
// first entry that claimed it, and that nothing was dropped.
const dupServerNameMessage = `server name "prod" is also declared by the server at /servers/0/name; ` +
	`both are kept, and the name identifies two hosts`

// TestDuplicateServerName_EveryRepeatIsReported is the reported half of the
// rule: a name the document's own servers list declares more than once is an
// error, once per repeat rather than once per name, sited at the repeat's own
// name key and naming the first entry that claimed it. Both servers still lower
// with the name the document wrote: nothing is dropped and nothing is merged.
func TestDuplicateServerName_EveryRepeatIsReported(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spec    string
		want    []string // the pointer of each repeat, in source order
		servers []string // each lowered server's declared name
	}{
		{
			name: "the issue's repro: two entries declaring one name",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://prod.example.com
    name: prod
  - url: https://prod-mirror.example.com
    name: prod
paths: {}
`,
			want:    []string{"/servers/1/name"},
			servers: []string{"prod", "prod"},
		},
		{
			name: "three entries sharing one name: one report per repeat",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://a.example.com
    name: prod
  - url: https://b.example.com
    name: prod
  - url: https://c.example.com
    name: prod
paths: {}
`,
			want:    []string{"/servers/1/name", "/servers/2/name"},
			servers: []string{"prod", "prod", "prod"},
		},
		{
			name: "one declaration mounted twice by a YAML alias",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - &s
    url: https://prod.example.com
    name: prod
  - *s
paths: {}
`,
			want:    []string{"/servers/1/name"},
			servers: []string{"prod", "prod"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.spec)

			assert.Equal(t, len(tc.want),
				openapitest.CountDiagsAt(diags, diag.DuplicateServerName, ir.SeverityError),
				"one diagnostic per repeat, not one per name: %+v", diags)
			var got []string
			for _, d := range diags {
				if d.Code == diag.DuplicateServerName {
					got = append(got, string(d.Provenance.Pointer))
				}
			}
			assert.Equal(t, tc.want, got, "each repeat sits at its own name key, in source order")
			for _, ptr := range tc.want {
				assert.Equal(t, dupServerNameMessage,
					openapitest.DiagMessageAt(t, diags, diag.DuplicateServerName, ir.SeverityError, ptr),
					"%s names the repeated name and the first claimant's key", ptr)
			}

			names := make([]string, 0, len(doc.Servers))
			for _, s := range doc.Servers {
				names = append(names, s.Name.Source)
			}
			assert.Equal(t, tc.servers, names, "every server lowered, each with the name the document wrote")
		})
	}
}

// TestDuplicateServerName_NilEntryKeepsSourceIndices pins the rule against a
// list the parser cannot produce: a nil entry between the claimant and the
// repeat. The pointers must carry the *source* indices, so the repeat at index 2
// names the claimant at index 1 rather than the first surviving server. The
// document is hand-built for the same reason TestLowerServers_NilEntrySkipped's
// is: a nil entry never reaches lowering through the parser.
func TestDuplicateServerName_NilEntryKeepsSourceIndices(t *testing.T) {
	t.Parallel()
	prod := "prod"
	doc := &soa.OpenAPI{Servers: []*soa.Server{
		nil,
		{URL: "https://prod.example.com", Name: &prod},
		{URL: "https://prod-mirror.example.com", Name: &prod},
	}}

	got, diags := lowerServers(lowering.Ctx{Doc: doc})

	require.Len(t, got, 2, "the nil entry is skipped and both real servers still lower")
	assert.Equal(t, []string{"prod", "prod"}, []string{got[0].Name.Source, got[1].Name.Source})
	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DuplicateServerName, ir.SeverityError),
		"one report at the repeat: %+v", diags)
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DuplicateServerName, "/servers/2/name"),
		"the repeat sits at its source index: %+v", diags)
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DuplicateServerName, ir.SeverityError, "/servers/2/name"),
		"/servers/1/name", "and names the first claimant at its source index")
}

// TestDuplicateServerName_NotReported is the tolerated half: the collisions the
// rule deliberately ignores. Declared names are compared only to other declared
// names, so a hint colliding with a hint, a declared name equal to another
// entry's hint, and two empty names are all silent — none of them is a name the
// document declared twice.
func TestDuplicateServerName_NotReported(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec string
		// hintsCollide requires both lowered names to carry the same non-empty
		// hint, so the row pins the hint boundary only if the hints really do
		// collide rather than by passing vacuously.
		hintsCollide bool
	}{
		{
			name: "distinct declared names",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://prod.example.com
    name: prod
  - url: https://staging.example.com
    name: staging
paths: {}
`,
		},
		{
			name: "two unnamed servers whose URL hints collide",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
servers:
  - url: https://api.example.com/v1
  - url: https://api.example.com/v-1
paths: {}
`,
			hintsCollide: true,
		},
		{
			name: "a declared name equal to another entry's hint",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://api.example.com/v1
  - url: https://other.example.com
    name: https_api_example_com_v_1
paths: {}
`,
		},
		{
			name: "an empty name is not a name",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://a.example.com
    name: ""
  - url: https://b.example.com
    name: ""
paths: {}
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.spec)

			assert.False(t, openapitest.HasDiag(diags, diag.DuplicateServerName),
				"nothing is reported: %+v", diags)
			if !tc.hintsCollide {
				return
			}
			require.Len(t, doc.Servers, 2)
			assert.NotEmpty(t, doc.Servers[0].Name.Hint, "the row is about hints, so the names must be hints")
			assert.Equal(t, doc.Servers[0].Name.Hint, doc.Servers[1].Name.Hint,
				"and they must really collide, or the row asserts nothing")
		})
	}
}
