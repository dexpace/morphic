// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"os"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestLoad_ATaggedMappingIsRefusedBeforeParsing pins the refusal on the fixture
// the harness sweeps. A mapping carrying a tag other than !!map at a request
// body faults the parser on a goroutine of its own, past the reach of
// buildRecovered's barrier, so the document has to be refused before the parser
// is handed the tree — as a spec problem naming the mapping, not a crash
// (GitHub #474).
func TestLoad_ATaggedMappingIsRefusedBeforeParsing(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../../testdata/openapi/tagged_mapping_request_body.yaml")
	require.NoError(t, err)

	doc, diags, err := Load(t.Context(), 5, compilers.Source{Path: "tagged.yaml", Data: data}, Options{})

	require.NoError(t, err, "a tagged mapping is a spec problem, not a Go error")
	assert.Nil(t, doc, "nothing is lowered")
	require.Len(t, diags, 1)
	assert.Equal(t, diag.TaggedMapping, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, ir.Provenance{Source: 5, Pointer: "/paths/~1blob/put/requestBody",
		Position: ir.Position{Line: 8, Column: 8}}, diags[0].Provenance,
		"the refusal names the mapping the tag is written on")
	assert.Contains(t, diags[0].Message, `"!content:"`, "and quotes the tag")
}

// taggedChildEnv hands a child (see inChild) a spec, which it hands the parser
// alone in place of the test it re-ran: the fault being asked about ends the
// process.
const taggedChildEnv = "MORPHIC_TAGGED_CHILD_SPEC"

// parseBare is the child's side of parserFaults: it hands the parser spec, as
// Load would after its refusals, and returns if the parser does.
func parseBare(t *testing.T, spec string) {
	t.Helper()
	_, _, err := soa.Unmarshal(t.Context(), strings.NewReader(spec))
	require.NoError(t, err)
}

// parserFaults reports whether the parser, handed spec with nothing refused
// first, ends the process on a nil dereference inside the library. Any other
// way for the child to end fails the test: it says nothing about the parser.
func parserFaults(t *testing.T, spec string) bool {
	t.Helper()
	out, end := inChild(t, "TestLoad_ATaggedMappingIsRefusedWhereverTheParserFaults", taggedChildEnv, spec)
	if end == childPassed {
		return false
	}
	require.Equal(t, childPanicked, end, "%.1500s", out)
	require.Contains(t, out, "invalid memory address or nil pointer dereference", "%.1500s", out)
	require.Contains(t, out, "github.com/speakeasy-api/openapi/", "the fault is in the library:\n%.1500s", out)
	return true
}

// TestLoad_ATaggedMappingIsRefusedWhereverTheParserFaults sweeps positions the
// parser builds through a reference and the tag spellings that reach them.
// The parser is asked first: each row, handed to it alone, must fault with its
// tag and parse without, so a row it stops faulting on has gone stale. Load
// must then refuse the row once. The alias row is the one the walk must not
// need special handling for: the tagged node is reached where its anchor is
// declared.
func TestLoad_ATaggedMappingIsRefusedWhereverTheParserFaults(t *testing.T) {
	if spec, ok := asChild(taggedChildEnv); ok {
		parseBare(t, spec)
		return
	}
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	const ok = "      responses: {\"200\": {description: ok}}\n"
	for _, tc := range []struct{ name, tag, body string }{
		{"request body", "!x", "paths:\n  /b:\n    put:\n      requestBody: TAG {content: {application/json: {}}}\n" + ok},
		{"response", "!x", "paths:\n  /b:\n    get:\n      responses: {\"200\": TAG {description: ok}}\n"},
		{"parameter", "!x", "paths:\n  /b:\n    get:\n      parameters: [TAG {name: q, in: query}]\n" + ok},
		{"header", "!x", "paths:\n  /b:\n    get:\n      responses: {\"200\": {description: ok, headers: {X-A: TAG {schema: {}}}}}\n"},
		{"example", "!x", "paths:\n  /b:\n    get:\n      responses: {\"200\": {description: ok, content: {application/json: {examples: {e: TAG {value: 1}}}}}}\n"},
		{"link", "!x", "paths:\n  /b:\n    get:\n      responses: {\"200\": {description: ok, links: {l: TAG {operationId: x}}}}\n"},
		{"security scheme", "!x", "paths: {}\ncomponents:\n  securitySchemes:\n    s: TAG {type: http, scheme: basic}\n"},
		{"standard tag !!str", "!!str", "paths:\n  /b:\n    put:\n      requestBody: TAG {content: {application/json: {}}}\n" + ok},
		{"standard tag !!set", "!!set", "paths:\n  /b:\n    put:\n      requestBody: TAG {content: {application/json: {}}}\n" + ok},
		{"through an alias", "!x", "x-body: &b TAG {content: {application/json: {}}}\npaths:\n  /b:\n    put:\n      requestBody: *b\n" + ok},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tagged := head + strings.ReplaceAll(tc.body, "TAG", tc.tag)
			untagged := head + strings.ReplaceAll(tc.body, "TAG ", "")

			parsed, _, err := soa.Unmarshal(t.Context(), strings.NewReader(untagged))
			require.NoError(t, err)
			require.NotNil(t, parsed, "the parser reads the row without its tag")
			require.True(t, parserFaults(t, tagged), "the parser faults on the row with its tag")

			doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(tagged), Options{})

			require.NoError(t, err)
			assert.Nil(t, doc)
			assert.Equal(t, 1, countErrorsAt(diags, diag.TaggedMapping), "diagnostics: %+v", diags)
		})
	}
}

// TestLoad_TheMapTagInAnyOfItsSpellingsLoads is the control: a request body
// whose tag is written as YAML resolves it — explicitly, as the non-specific
// tag, or in verbatim form — carries the same tag every untagged mapping does,
// and loads clean.
func TestLoad_TheMapTagInAnyOfItsSpellingsLoads(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /b:\n    put:\n" +
		"      requestBody: TAG {content: {application/json: {}}}\n      responses: {\"200\": {description: ok}}\n"

	for name, tag := range map[string]string{
		"explicit !!map":   "!!map",
		"non-specific !":   "!",
		"verbatim map tag": "!<tag:yaml.org,2002:map>",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(strings.Replace(spec, "TAG", tag, 1)), Options{})

			require.NoError(t, err)
			require.NotNil(t, doc, "the tag YAML resolves a mapping to is not a refusal: %+v", diags)
			assert.False(t, diag.HasError(diags), "unexpected refusal: %+v", diags)
		})
	}
}

// TestLoad_RefusesATaggedMappingAnOverlayIntroduced pins that the refusal reads
// the tree the parser is handed. An overlay's update value is parsed from the
// overlay document, tag and all, and a key the target lacks is grafted as that
// node — so an overlay can put a tagged mapping at a response the source never
// declared; without the second scan it would reach the parser. (An update to a
// key the target already has merges into the existing node, which keeps its
// own tag, so the new key is the shape that carries one across.)
func TestLoad_RefusesATaggedMappingAnOverlayIntroduced(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /b:\n    get:\n" +
		"      responses: {\"200\": {description: ok}}\n"
	clean, _, err := Load(t.Context(), 0, openapitest.SourceOf(spec), Options{})
	require.NoError(t, err)
	require.NotNil(t, clean, "the source alone gives the scan nothing to find")

	got, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec),
		overlayOptions("  - target: $.paths['/b'].get.responses\n    update: {\"404\": !x {description: missing}}\n"))

	require.NoError(t, err)
	assert.Nil(t, got, "the patched document is refused before the parser sees it")
	assert.Equal(t, 1, countErrorsAt(diags, diag.TaggedMapping), "the introduced tag is what refused it")
}

// TestLoad_ACycleAndATaggedMappingAreBothReported pins that the tag refusal is
// added to the alias scan's answer rather than replacing it, so an author with
// both problems is told about both from one compile. A loop of schema $refs is
// no refusal: it loads and is reported by the binding
// (TestLoad_ADegenerateCycleLoadsAndIsReportedOnce).
func TestLoad_ACycleAndATaggedMappingAreBothReported(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /b:\n    get:\n" +
		"      responses: {\"200\": !x {description: ok}}\ncomponents:\n  schemas:\n    A: &a {properties: {self: *a}}\n"

	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec), Options{})

	require.NoError(t, err)
	assert.Nil(t, doc)
	assert.Equal(t, 1, countErrorsAt(diags, diag.CyclicRef), "diagnostics: %+v", diags)
	assert.Equal(t, 1, countErrorsAt(diags, diag.TaggedMapping), "diagnostics: %+v", diags)
}
