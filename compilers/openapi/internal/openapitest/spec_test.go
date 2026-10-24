package openapitest_test

import (
	"maps"
	"slices"
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

// TestHolderSpec_DeclaresHolderBesideTheRest pins the parts a caller writes:
// the version, Holder's property p, and the schemas of rest after Holder.
func TestHolderSpec_DeclaresHolderBesideTheRest(t *testing.T) {
	t.Parallel()
	got := openapitest.HolderSpec("3.0.3", "{type: string, nullable: true}", "    Extra: {type: integer}\n")
	var doc struct {
		OpenAPI    string `yaml:"openapi"`
		Components struct {
			Schemas map[string]yaml.Node `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(got), &doc), got)
	assert.Equal(t, "3.0.3", doc.OpenAPI)
	assert.ElementsMatch(t, []string{"Plain", "Pet", "Holder", "Extra"}, slices.Collect(maps.Keys(doc.Components.Schemas)))
	assert.Contains(t, got, "      properties:\n        p: {type: string, nullable: true}\n")
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

// TestSchemaFromYAML_KeepsTheReferenceForm checks a schema written as a $ref
// comes back as a reference, which a bare *oas3.Schema could not hold.
func TestSchemaFromYAML_KeepsTheReferenceForm(t *testing.T) {
	t.Parallel()
	js := openapitest.SchemaFromYAML(t.Context(), t, "{$ref: '#/$defs/T'}")
	require.NotNil(t, js)
	assert.True(t, js.IsReference())
}

// TestSchemaFromYAML_FailsOnAFixtureThatDoesNotParse checks a fixture the
// library cannot read fails the test rather than handing back an empty schema.
func TestSchemaFromYAML_FailsOnAFixtureThatDoesNotParse(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	openapitest.SchemaFromYAML(t.Context(), rec, "{")
	assert.True(t, rec.failed(), "an unreadable fixture is reported")
}

// TestSchemaFromYAML_FailsOnAFixtureThatDoesNotValidate pins the second check:
// a fixture that parses but that the library reports invalid, here a required
// written as a scalar, fails the test rather than lowering a half-read schema.
func TestSchemaFromYAML_FailsOnAFixtureThatDoesNotValidate(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	openapitest.SchemaFromYAML(t.Context(), rec, "required: x")
	assert.True(t, rec.failed(), "a fixture the library refuses is reported")
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

// TestYAMLDocument_ReturnsTheDocumentNode checks the document node is kept, not
// unwrapped as YAMLNode unwraps it, and that a fixture that does not parse
// fails the test.
func TestYAMLDocument_ReturnsTheDocumentNode(t *testing.T) {
	t.Parallel()
	doc := openapitest.YAMLDocument(t, "{a: 1}")
	require.NotNil(t, doc)
	assert.Equal(t, yaml.DocumentNode, doc.Kind)
	require.Len(t, doc.Content, 1)
	assert.Equal(t, yaml.MappingNode, doc.Content[0].Kind)

	rec := &recorder{}
	openapitest.YAMLDocument(rec, "{a: [")
	assert.True(t, rec.failed(), "a fixture that does not parse is reported")
}

// TestClearAnchors_EmptiesEveryAnchorBeneathTheRoot pins the reach: an anchor
// nested under another is cleared too, and an alias still names its target.
func TestClearAnchors_EmptiesEveryAnchorBeneathTheRoot(t *testing.T) {
	t.Parallel()
	doc := openapitest.YAMLDocument(t, "a: &x {b: &y 1}\nc: *x\nd: *y\n")
	anchors := func() []string {
		var out []string
		stack := []*yaml.Node{doc}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if n.Anchor != "" {
				out = append(out, n.Anchor)
			}
			stack = append(stack, n.Content...)
		}
		return out
	}
	require.ElementsMatch(t, []string{"x", "y"}, anchors(), "the fixture writes two anchors")

	openapitest.ClearAnchors(doc)
	assert.Empty(t, anchors())
	alias := doc.Content[0].Content[3]
	require.Equal(t, yaml.AliasNode, alias.Kind)
	assert.Same(t, doc.Content[0].Content[1], alias.Alias)
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

// TestRootMergeChain_MergesEachLevelIntoTheNext pins the shape the merge-depth
// cases count on: levels anchored mappings, each merging the one before it, the
// innermost holding leaf, and the root merging the outermost before writing
// own. A chain one level off would put each case on the wrong side of the bound
// it tests.
func TestRootMergeChain_MergesEachLevelIntoTheNext(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "l0: &a0 {openapi: 3.1.0}\nl1: &a1 {<<: *a0}\nl2: &a2 {<<: *a1}\n<<: *a2\nown: x\n",
		string(openapitest.RootMergeChain(3, "openapi: 3.1.0", "own: x\n")))

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(openapitest.RootMergeChain(3, "openapi: 3.1.0", ""), &doc))
	assert.Equal(t, "3.1.0", doc["openapi"], "the leaf reaches the root through every level")
}

// TestNestedMerges_WritesOneMergePerLevel pins the spelling: a body merged in
// through as many `<<` keys as asked, a level of none being the body itself.
func TestNestedMerges_WritesOneMergePerLevel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "{k: v}", openapitest.NestedMerges(0, "{k: v}"))
	assert.Equal(t, "{<<: {<<: {k: v}}}", openapitest.NestedMerges(2, "{k: v}"))
	var n yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(openapitest.NestedMerges(3, "{k: v}")), &n), "it parses")
}
