package openapitest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestSourceOf_CarriesTheSpecBytes pins that a spec string arrives as source
// bytes under the fixed path the suites' diagnostics are stamped against.
func TestSourceOf_CarriesTheSpecBytes(t *testing.T) {
	t.Parallel()
	src := openapitest.SourceOf("openapi: 3.1.0\n")
	assert.Equal(t, "spec.yaml", src.Path)
	assert.Equal(t, "openapi: 3.1.0\n", string(src.Data))
}

// TestComponentSpec_WrapsSchemasInAMinimalDocument checks the wrapper produces
// a document the compiler accepts and that the version is settable, since the
// two entry points differ only in that.
//
// The empty paths key is asserted because 3.0 requires it and 3.1 does not, and
// callers pass both versions: dropping it leaves every 3.1 case green and turns
// the 3.0 ones into a different document than they were written against.
func TestComponentSpec_WrapsSchemasInAMinimalDocument(t *testing.T) {
	t.Parallel()
	got := openapitest.ComponentSpec("    A: {type: string}\n")
	assert.True(t, strings.HasPrefix(got, "openapi: 3.1.0\n"), got)
	assert.Contains(t, got, "info: {title: T, version: \"1\"}\n")
	assert.Contains(t, got, "paths: {}\n")
	assert.Contains(t, got, "components:\n  schemas:\n    A: {type: string}\n")
	assert.True(t, strings.HasPrefix(openapitest.ComponentSpecVer("3.0.3", ""), "openapi: 3.0.3\n"))
}

// TestPathsSpec_WrapsPathsInAMinimalDocument is the paths counterpart: no
// components block, and the same settable version.
func TestPathsSpec_WrapsPathsInAMinimalDocument(t *testing.T) {
	t.Parallel()
	got := openapitest.PathsSpec("  /a:\n    get: {responses: {\"200\": {description: ok}}}\n")
	assert.True(t, strings.HasPrefix(got, "openapi: 3.1.0\n"), got)
	assert.Contains(t, got, "info: {title: T, version: \"1\"}\n")
	assert.NotContains(t, got, "components:")
	assert.Contains(t, got, "paths:\n  /a:\n")
	assert.True(t, strings.HasPrefix(openapitest.PathsSpecVer("3.0.3", ""), "openapi: 3.0.3\n"))
}

// TestBothWrappers_ParseAsYAML guards the property every caller depends on and
// no string assertion above establishes: the wrappers emit well-formed YAML.
func TestBothWrappers_ParseAsYAML(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"components": openapitest.ComponentSpec("    A: {type: string}\n"),
		"paths":      openapitest.PathsSpec("  /a: {}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var doc map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
			assert.Equal(t, "3.1.0", doc["openapi"])
		})
	}
}

// TestDocDeclaring_DeclaresEveryNamedComponent pins that each name reaches the
// document as its own schema entry, in the order given.
func TestDocDeclaring_DeclaresEveryNamedComponent(t *testing.T) {
	t.Parallel()
	doc := openapitest.DocDeclaring("A", "B")
	require.NotNil(t, doc.Components)
	require.NotNil(t, doc.Components.Schemas)

	var names []string
	for name := range doc.Components.Schemas.All() {
		names = append(names, name)
	}
	assert.Equal(t, []string{"A", "B"}, names)
}

// TestEmptyEitherSchema_IsASchemaWithNoSchema pins the shape the nil-schema
// guards are driven with: the either-value says it holds a schema, and holds
// none.
func TestEmptyEitherSchema_IsASchemaWithNoSchema(t *testing.T) {
	t.Parallel()
	js := openapitest.EmptyEitherSchema()
	require.NotNil(t, js)
	assert.True(t, js.IsSchema())
	assert.Nil(t, js.GetSchema())
}

// TestYAMLNode_ReturnsTheRootValueNode checks the document node is unwrapped,
// which is what makes the result comparable to what a parsed schema field
// exposes.
func TestYAMLNode_ReturnsTheRootValueNode(t *testing.T) {
	t.Parallel()
	node := openapitest.YAMLNode(t, "{a: 1}")
	require.NotNil(t, node)
	assert.Equal(t, yaml.MappingNode, node.Kind)
	require.Len(t, node.Content, 2)
	assert.Equal(t, "a", node.Content[0].Value)
}

// TestScalarNode_BuildsABareScalar covers both builders, StrNode being the
// string-tagged case of ScalarNode.
func TestScalarNode_BuildsABareScalar(t *testing.T) {
	t.Parallel()
	assert.Equal(t, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "7"},
		openapitest.ScalarNode("!!int", "7"))
	assert.Equal(t, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "x"},
		openapitest.StrNode("x"))
}

// TestInlineProbeBody_WritesEveryKeywordTheProbeAssertsOn keeps the constant
// and the assertions that read it in step: AssertProbeDocsKept and
// AssertProbeExample look for exactly these, and a keyword dropped here would
// silently weaken every inline-position case in two packages.
func TestInlineProbeBody_WritesEveryKeywordTheProbeAssertsOn(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"title: SUM", "description: DOC", "externalDocs", "description: ED",
		"deprecated: true", "example: abc", "x-vendor: V", "xml:", "not:", "maxLength: 3",
	} {
		assert.Contains(t, openapitest.InlineProbeBody, want)
	}
}
