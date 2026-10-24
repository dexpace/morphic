package document_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/document"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

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
	require.Len(t, doc.Servers, 2)
	for i, url := range []string{"https://prod.example.com", "https://prod-mirror.example.com"} {
		assert.Equal(t, "prod", doc.Servers[i].Name.Source)
		assert.Equal(t, url, doc.Servers[i].URLTemplate)
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
			require.Len(t, doc.Servers, 2)
			a, b := tc.collide(doc.Servers[0].Name, doc.Servers[1].Name)
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
			assert.Equal(t, tc.want, document.ServerName(&soa.Server{URL: tc.url}))
		})
	}
}

// TestServerName_DistinguishesServersDifferingOnlyInPath is why the hint is the
// whole URL template and not the host inside it: one host serving two API
// versions is an ordinary OpenAPI document, and a host-derived hint would name
// both servers the same.
func TestServerName_DistinguishesServersDifferingOnlyInPath(t *testing.T) {
	t.Parallel()
	v1 := document.ServerName(&soa.Server{URL: "https://api.example.com/v1"})
	v2 := document.ServerName(&soa.Server{URL: "https://api.example.com/v2"})
	assert.NotEqual(t, v1.Hint, v2.Hint, "two distinct servers get two distinct hints")
}

// TestServerName_CollidesOnPunctuationAlone bounds that claim, which must not be
// read as "distinct servers get distinct hints": canonicalizing drops every
// non-word character, so two URLs differing only in punctuation reduce to one
// word sequence. Uniquifying it is an emitter's job, and pinning it keeps that a
// known bound rather than a later discovery.
func TestServerName_CollidesOnPunctuationAlone(t *testing.T) {
	t.Parallel()
	dotted := document.ServerName(&soa.Server{URL: "https://api.example.com/v1"})
	dashed := document.ServerName(&soa.Server{URL: "https://api.example.com/v-1"})
	assert.Equal(t, dotted.Hint, dashed.Hint,
		"neutral words carry no punctuation, so these two collide")
}

func TestLowerServers_NilEntrySkipped(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Servers: []*soa.Server{nil, {URL: "https://x.example.com"}}}
	got, diags := document.LowerServers(lowering.Ctx{Doc: doc})

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
	srv, diags := document.LowerServer(lowering.Ctx{}, &soa.Server{URL: "https://x", Variables: vars}, "/servers/0")
	assert.Empty(t, diags)
	require.Len(t, srv.Variables, 1, "nil variable entry skipped")
	assert.Equal(t, "keep", srv.Variables[0].Name)
}

// TestLowerServers_EveryEntrySkippedIsNil pins, for the servers, the guard
// auth's TestLowerSecuritySchemes_NothingLoweredIsNilNotEmpty pins for the
// schemes: when no entry survives, the field stays unset rather than becoming
// an empty list the source never declared.
func TestLowerServers_EveryEntrySkippedIsNil(t *testing.T) {
	t.Parallel()
	doc := &soa.OpenAPI{Servers: []*soa.Server{nil, nil}}

	got, diags := document.LowerServers(lowering.Ctx{Doc: doc})
	assert.Nil(t, got)
	assert.Empty(t, diags)
}
