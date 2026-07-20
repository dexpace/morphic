package openapi

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
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

func TestParse_UnsupportedVersion(t *testing.T) {
	t.Parallel()
	spec := "openapi: 2.0.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
	doc, diags, err := New().Compile(context.Background(), []compilers.Source{sourceOf(spec)}, compilers.Options{})
	require.NoError(t, err)
	assert.Nil(t, doc, "unsupported version refuses to lower")
	var sawUnsupported bool
	for _, d := range diags {
		if d.Code == codeUnsupportedVersion {
			sawUnsupported = true
		}
	}
	assert.True(t, sawUnsupported)
}

func TestParse_UnmarshalError(t *testing.T) {
	t.Parallel()
	_, _, err := New().Compile(context.Background(),
		[]compilers.Source{sourceOf("\t\t: : : not valid : yaml\n\x00")}, compilers.Options{})
	require.Error(t, err)
}

func TestParse_WrongFormatOptions(t *testing.T) {
	t.Parallel()
	_, _, err := New().Compile(context.Background(),
		[]compilers.Source{sourceOf("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n")},
		compilers.Options{FormatOptions: "not-openapi-options"})
	require.Error(t, err, "wrong FormatOptions type is a programmer error")
}

func TestParse_ExplicitOptions(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a/b:\n    get: {operationId: ab, responses: {\"200\": {description: ok}}}\n"
	doc, _, err := New().Compile(context.Background(), []compilers.Source{sourceOf(spec)},
		compilers.Options{FormatOptions: Options{Grouping: GroupByPathPrefix}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "a", doc.Services[0].Groups[0].Name.Source)
}

// ghostRefsSpec references non-existent components everywhere so every
// resolve-or-skip path (unresolved GetObject → nil) and resolution-error branch
// is exercised without a panic.
const ghostRefsSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    parameters:
      - {$ref: '#/components/parameters/GhostParam'}
    get:
      operationId: getA
      callbacks:
        good:
          '{$url}': {$ref: '#/components/pathItems/GhostInner'}
        bad: {$ref: '#/components/callbacks/GhostCb'}
      requestBody: {$ref: '#/components/requestBodies/GhostBody'}
      responses:
        "200": {$ref: '#/components/responses/GhostResp'}
        "201":
          description: ok
          headers:
            X-H: {$ref: '#/components/headers/GhostHeader'}
          content:
            application/json:
              schema: {type: string}
              examples:
                one: {$ref: '#/components/examples/GhostEx'}
  /ref: {$ref: '#/components/pathItems/GhostItem'}
webhooks:
  hook: {$ref: '#/components/pathItems/GhostHook'}
`

func TestGhostRefs_AllResolversDegradeGracefully(t *testing.T) {
	t.Parallel()
	// Uses the internal lowerer directly so resolution errors surface as
	// diagnostics without failing the parse; the point is no panic and coverage
	// of every resolve-or-skip branch.
	loadedDoc, diags, err := load(t.Context(), 0, sourceOf(ghostRefsSpec), Options{}.withDefaults())
	require.NoError(t, err)
	require.NotNil(t, loadedDoc)
	l := newLowerer(0, loadedDoc, Options{}.withDefaults())
	out := l.run()
	require.NotNil(t, out)
	var sawUnresolved bool
	for _, d := range append(diags, l.diags...) {
		if d.Code == codeUnresolvedRef {
			sawUnresolved = true
		}
	}
	assert.True(t, sawUnresolved, "unresolved refs reported")
}

func TestPrimID_SecondCallReuses(t *testing.T) {
	t.Parallel()
	l := newLowerer(0, &loaded{Doc: nil, Source: ir.SourceInfo{}}, Options{}.withDefaults())
	first := l.primID(ir.PrimString)
	second := l.primID(ir.PrimString)
	assert.Equal(t, first, second, "interned primitive reused on second call")
}
