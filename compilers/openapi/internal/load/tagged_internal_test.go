package load

import (
	"os"
	"strings"
	"testing"

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
// unmarshal's recover, so the document has to be refused before the parser is
// handed the tree — as a spec problem naming the mapping, not a crash
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
	assert.Equal(t, ir.Provenance{Source: 5, Pointer: "8:8"}, diags[0].Provenance,
		"the refusal names the mapping the tag is written on")
	assert.Contains(t, diags[0].Message, `"!content:"`, "and quotes the tag")
}

// TestLoad_ATaggedMappingIsRefusedWhereverTheParserFaults sweeps the positions
// the parser builds through a reference — each of which it nil-dereferences on
// a tagged mapping — and the tag spellings that reach them, so the refusal is
// pinned to the class rather than to the one fixture above. The alias case is
// the one the walk must not need special handling for: the tagged node is
// reached where its anchor is declared.
func TestLoad_ATaggedMappingIsRefusedWhereverTheParserFaults(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	for name, body := range map[string]string{
		"request body": "paths:\n  /b:\n    put:\n      requestBody: !x {content: {application/json: {}}}\n" +
			"      responses: {\"200\": {description: ok}}\n",
		"response":        "paths:\n  /b:\n    get:\n      responses: {\"200\": !x {description: ok}}\n",
		"parameter":       "paths:\n  /b:\n    get:\n      parameters: [!x {name: q, in: query}]\n      responses: {\"200\": {description: ok}}\n",
		"header":          "paths:\n  /b:\n    get:\n      responses: {\"200\": {description: ok, headers: {X-A: !x {schema: {}}}}}\n",
		"example":         "paths:\n  /b:\n    get:\n      responses: {\"200\": {description: ok, content: {application/json: {examples: {e: !x {value: 1}}}}}}\n",
		"link":            "paths:\n  /b:\n    get:\n      responses: {\"200\": {description: ok, links: {l: !x {operationId: x}}}}\n",
		"security scheme": "paths: {}\ncomponents:\n  securitySchemes:\n    s: !x {type: http, scheme: basic}\n",
		"standard tag !!str": "paths:\n  /b:\n    put:\n      requestBody: !!str {content: {application/json: {}}}\n" +
			"      responses: {\"200\": {description: ok}}\n",
		"standard tag !!set": "paths:\n  /b:\n    put:\n      requestBody: !!set {content: {application/json: {}}}\n" +
			"      responses: {\"200\": {description: ok}}\n",
		"through an alias": "x-body: &b !x {content: {application/json: {}}}\npaths:\n  /b:\n    put:\n      requestBody: *b\n" +
			"      responses: {\"200\": {description: ok}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(head+body), Options{})

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
// added to the cycle scan's answer rather than replacing it, so an author with
// both problems is told about both from one compile.
func TestLoad_ACycleAndATaggedMappingAreBothReported(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /b:\n    get:\n" +
		"      responses: {\"200\": !x {description: ok}}\ncomponents:\n  schemas:\n    A: {$ref: '#/components/schemas/A'}\n"

	doc, diags, err := Load(t.Context(), 0, openapitest.SourceOf(spec), Options{})

	require.NoError(t, err)
	assert.Nil(t, doc)
	assert.Equal(t, 1, countErrorsAt(diags, diag.CyclicRef), "diagnostics: %+v", diags)
	assert.Equal(t, 1, countErrorsAt(diags, diag.TaggedMapping), "diagnostics: %+v", diags)
}
