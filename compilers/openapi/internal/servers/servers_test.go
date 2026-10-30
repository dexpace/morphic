package servers_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/servers"
	"github.com/dexpace/morphic/ir"
)

// parseFull runs the whole public compiler pipeline over src, which is what
// reaches every list a document declares servers in.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{openapitest.SourceOf(src)}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// serviceServers returns the servers the document's one service names, in its
// order, each read from the registry, failing when one names nothing there.
func serviceServers(t *testing.T, doc *ir.Document) []ir.Server {
	t.Helper()
	require.Len(t, doc.Services, 1)
	return resolve(t, doc, doc.Services[0].Servers)
}

// resolve reads each of ids from doc's registry, in order.
func resolve(t *testing.T, doc *ir.Document, ids []ir.ServerID) []ir.Server {
	t.Helper()
	out := make([]ir.Server, 0, len(ids))
	for _, id := range ids {
		s, ok := doc.Servers[id]
		require.True(t, ok, "%s names no registered server", id)
		out = append(out, s)
	}
	return out
}

// TestDuplicateServerName_BothServersLower is GitHub #631's repro compiled end
// to end: the repeat is reported, a warning at its own name naming the first,
// and nothing is dropped or merged, each server lowering with the name it
// declares. The load package's tests cover every list the rule reads.
func TestDuplicateServerName_BothServersLower(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://prod.example.com
    name: prod
  - url: https://prod-mirror.example.com
    name: prod
paths: {}
`)
	require.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.DuplicateServerName, ir.SeverityWarning),
		"one report: %+v", diags)
	assert.Contains(t,
		openapitest.DiagMessageAt(t, diags, diag.DuplicateServerName, ir.SeverityWarning, "/servers/1/name"),
		"/servers/0/name")
	got := serviceServers(t, doc)
	require.Len(t, got, 2)
	for i, url := range []string{"https://prod.example.com", "https://prod-mirror.example.com"} {
		assert.Equal(t, "prod", got[i].Name.Source)
		assert.Equal(t, url, got[i].URLTemplate)
	}
}

// TestDuplicateServerName_CanonicalCollisionsAreNotReported is the tolerated
// half: servers whose lowered names collide although the document never wrote
// one name twice. A hint colliding with a hint, a declared name equal to
// another entry's hint, and two names equal only once canonicalized are all
// silent, since an emitter uniquifies them. Each row first proves its collision
// is real.
func TestDuplicateServerName_CanonicalCollisionsAreNotReported(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec string
		// collide picks the two lowered words a row claims are equal, and the
		// row requires them to be, non-empty: it pins a boundary only if the
		// collision really happens rather than by passing vacuously.
		collide func(a, b ir.Naming) (string, string)
	}{
		{
			name: "two unnamed servers whose URL hints collide",
			spec: `openapi: 3.1.0
info: {title: T, version: "1"}
servers:
  - url: https://api.example.com/v1
  - url: https://api.example.com/v-1
paths: {}
`,
			collide: func(a, b ir.Naming) (string, string) { return a.Hint, b.Hint },
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
			collide: func(a, b ir.Naming) (string, string) { return a.Hint, b.Canonical },
		},
		{
			name: "two names that differ only in case",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://a.example.com
    name: prod
  - url: https://b.example.com
    name: Prod
paths: {}
`,
			collide: func(a, b ir.Naming) (string, string) { return a.Canonical, b.Canonical },
		},
		{
			name: "two names that differ only in their separator",
			spec: `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://a.example.com
    name: prod-eu
  - url: https://b.example.com
    name: prod_eu
paths: {}
`,
			collide: func(a, b ir.Naming) (string, string) { return a.Canonical, b.Canonical },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.spec)

			assert.False(t, openapitest.HasDiag(diags, diag.DuplicateServerName),
				"nothing is reported: %+v", diags)
			got := serviceServers(t, doc)
			require.Len(t, got, 2)
			a, b := tc.collide(got[0].Name, got[1].Name)
			assert.NotEmpty(t, a, "the row compares lowered words, so they must be present")
			assert.Equal(t, a, b, "and they must really collide, or the row asserts nothing")
		})
	}
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
			assert.Equal(t, tc.want, servers.ServerName(&soa.Server{URL: tc.url}))
		})
	}
}

// TestServerName_DistinguishesServersDifferingOnlyInPath is why the hint is the
// whole URL template and not the host inside it: one host serving two API
// versions is an ordinary OpenAPI document, and a host-derived hint would name
// both servers the same.
func TestServerName_DistinguishesServersDifferingOnlyInPath(t *testing.T) {
	t.Parallel()
	v1 := servers.ServerName(&soa.Server{URL: "https://api.example.com/v1"})
	v2 := servers.ServerName(&soa.Server{URL: "https://api.example.com/v2"})
	assert.NotEqual(t, v1.Hint, v2.Hint, "two distinct servers get two distinct hints")
}

// TestServerName_CollidesOnPunctuationAlone bounds that claim, which must not be
// read as "distinct servers get distinct hints": canonicalizing drops every
// non-word character, so two URLs differing only in punctuation reduce to one
// word sequence. Uniquifying it is an emitter's job, and pinning it keeps that a
// known bound rather than a later discovery.
func TestServerName_CollidesOnPunctuationAlone(t *testing.T) {
	t.Parallel()
	dotted := servers.ServerName(&soa.Server{URL: "https://api.example.com/v1"})
	dashed := servers.ServerName(&soa.Server{URL: "https://api.example.com/v-1"})
	assert.Equal(t, dotted.Hint, dashed.Hint,
		"neutral words carry no punctuation, so these two collide")
}

// TestRegistry_DeclaredNilEntryKeepsItsIndex pins that an entry no valid
// document writes lowers nothing and moves no other: the entry after it keeps
// its own position as its ID.
func TestRegistry_DeclaredNilEntryKeepsItsIndex(t *testing.T) {
	t.Parallel()
	reg := servers.NewRegistry()
	got, diags := reg.Declared(lowering.Ctx{}, []*soa.Server{nil, {URL: "https://x.example.com"}}, "/paths/~1a")

	assert.Empty(t, diags)
	assert.Equal(t, []ir.ServerID{"server/openapi/paths/~1a/servers/1"}, got)
	require.Len(t, reg.Servers(), 1)
	assert.Equal(t, "https://x.example.com", reg.Servers()[got[0]].URLTemplate)
}

// TestRegistry_DeclaredEveryEntryNilIsNil pins, for a list, the guard auth's
// TestLowerSecuritySchemes_NothingLoweredIsNilNotEmpty pins for the schemes:
// when no entry survives, the list names nothing and the registry stays unset
// rather than becoming an empty map the source never declared.
func TestRegistry_DeclaredEveryEntryNilIsNil(t *testing.T) {
	t.Parallel()
	reg := servers.NewRegistry()
	got, diags := reg.Declared(lowering.Ctx{}, []*soa.Server{nil, nil}, "")
	assert.Nil(t, got)
	assert.Empty(t, diags)
	assert.Nil(t, reg.Servers())
}

func TestServerVariables_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	vars := sequencedmap.New(
		sequencedmap.NewElem("skip", (*soa.ServerVariable)(nil)),
		sequencedmap.NewElem("keep", &soa.ServerVariable{}),
	)
	reg := servers.NewRegistry()
	got, diags := reg.Declared(lowering.Ctx{}, []*soa.Server{{URL: "https://x", Variables: vars}}, "")
	assert.Empty(t, diags)
	require.Len(t, got, 1)
	srv := reg.Servers()[got[0]]
	require.Len(t, srv.Variables, 1, "nil variable entry skipped")
	assert.Equal(t, "keep", srv.Variables[0].Name)
}

// TestRegistry_DeclaredOneDeclarationLowersOnce pins the registry's identity
// rule: one list reached twice, as a path item two paths mount is, names the
// same IDs both times, and what lowering it reports is reported once.
func TestRegistry_DeclaredOneDeclarationLowersOnce(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/Shared'}
  /b: {$ref: '#/components/pathItems/Shared'}
components:
  pathItems:
    Shared:
      servers:
        - {url: https://shared.example.com, bogus: 1}
      get: {responses: {"200": {description: ok}}}
`)
	const shared = ir.ServerID("server/openapi/components/pathItems/Shared/servers/0")
	var named int
	ir.ForEachOperation(doc, func(op ir.Operation) {
		assert.Equal(t, []ir.ServerID{shared}, op.Servers, "%s names the one declaration", op.ID)
		named++
	})
	assert.Equal(t, 2, named, "both mounts lower an operation")
	assert.Contains(t, doc.Servers, shared)
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.UnknownObjectKey, ir.SeverityWarning),
		"the unknown key is reported once, by the lowering, not once per mount: %+v", diags)
}

// TestRegistry_RootServesADocumentWithNoServersAtTheRoot pins OpenAPI's default
// for a servers field that is absent or empty: one server at "/", minted in a
// space of its own and recording no pointer, since no position writes it
// (GitHub #672).
func TestRegistry_RootServesADocumentWithNoServersAtTheRoot(t *testing.T) {
	t.Parallel()
	for name, spec := range map[string]string{
		"absent": "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n",
		"empty":  "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\nservers: []\npaths: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, _ := parseFull(t, spec)
			got := serviceServers(t, doc)
			require.Len(t, got, 1)
			assert.Equal(t, ir.ServerID("server/default"), got[0].ID)
			assert.Equal(t, "/", got[0].URLTemplate)
			assert.Equal(t, ir.Naming{Hint: "server"}, got[0].Name,
				"the default is still a server an emitter has to name (GitHub #258)")
			assert.Equal(t, ir.Provenance{Source: 0}, got[0].Provenance,
				"the whole document implies it, so it records no pointer")
			assert.Len(t, doc.Servers, 1, "nothing else is registered")
		})
	}
}

// TestRegistry_RootNamesTheDeclaredServersInSourceOrder pins the document's own
// list: each server registered at its own pointer, the service naming them in
// source order, and a declared server never joined by the default.
func TestRegistry_RootNamesTheDeclaredServersInSourceOrder(t *testing.T) {
	t.Parallel()
	doc, _ := parseFull(t, `openapi: 3.2.0
info: {title: T, version: "1"}
servers:
  - url: https://{region}.example.com
    name: primary
    description: Primary
    variables:
      zone: {default: a}
      region: {default: us, enum: [us, eu], description: Region}
  - url: https://backup.example.com
paths: {}
`)
	got := serviceServers(t, doc)
	require.Len(t, got, 2)
	assert.Equal(t, ir.ServerID("server/openapi/servers/0"), got[0].ID)
	assert.Equal(t, ir.ServerID("server/openapi/servers/1"), got[1].ID)
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/servers/1"}, got[1].Provenance)
	assert.Equal(t, ir.Naming{Source: "primary", Canonical: "primary"}, got[0].Name,
		"a declared 3.2 name is the server's name; the URL hint is for a server with none")
	assert.Equal(t, "Primary", got[0].Description.Description)
	require.Len(t, got[0].Variables, 2)
	assert.Equal(t, "zone", got[0].Variables[0].Name, "variables keep the order the document writes them in")
	assert.Equal(t, []string{"us", "eu"}, got[0].Variables[1].Enum)
	assert.Len(t, doc.Servers, 2, "no default joins a declared list")
}

// TestRegistry_DeclaredSecondReachLowersNothing pins the registry's own rule
// beneath the run's de-duplication of diagnostics: a list reached a second
// time, as a path item's is from a second mount, names the IDs the first reach
// registered and reports nothing, without lowering the declaration again.
func TestRegistry_DeclaredSecondReachLowersNothing(t *testing.T) {
	t.Parallel()
	loaded, _, err := load.Load(t.Context(), 0, openapitest.SourceOf(`openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    servers: [{url: https://a.example, bogus: 1}]
    get: {responses: {"200": {description: ok}}}
`), load.Options{})
	require.NoError(t, err)
	c := lowering.New(0, loaded.Doc, loaded.Source, lowering.GroupByTags, lowering.Limits{},
		lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{},
		overlay.Origin{}).WithLoaded(loaded)
	item, ok := loaded.Doc.GetPaths().Get("/a")
	require.True(t, ok)
	list := item.GetObject().GetServers()
	require.Len(t, list, 1)

	reg := servers.NewRegistry()
	first, diags := reg.Declared(c, list, "/paths/~1a")
	require.Len(t, diags, 1, "the first reach lowers the server and reports its undeclared key")
	again, diags := reg.Declared(c, list, "/paths/~1a")
	assert.Equal(t, first, again, "the second reach names the same server")
	assert.Empty(t, diags, "and reports nothing, having lowered nothing")
	assert.Len(t, reg.Servers(), 1)
}

// TestRegistry_RootServesNoDocumentAtTheRoot pins Root on a context holding no
// document, which every getter the walk reads answers for as empty: the default
// stands in, as for a document writing no servers.
func TestRegistry_RootServesNoDocumentAtTheRoot(t *testing.T) {
	t.Parallel()
	reg := servers.NewRegistry()
	got, diags := reg.Root(lowering.Ctx{})
	assert.Empty(t, diags)
	assert.Equal(t, []ir.ServerID{"server/default"}, got)
}

// TestRegistry_ServersHandsOverACopy pins that the map Servers returns is the
// caller's: writing to it changes nothing a later call returns.
func TestRegistry_ServersHandsOverACopy(t *testing.T) {
	t.Parallel()
	reg := servers.NewRegistry()
	got, diags := reg.Root(lowering.Ctx{})
	require.Empty(t, diags)
	require.Equal(t, []ir.ServerID{"server/default"}, got)
	first := reg.Servers()
	delete(first, "server/default")
	assert.Contains(t, reg.Servers(), ir.ServerID("server/default"))
}
