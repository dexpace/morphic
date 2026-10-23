package document

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

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
