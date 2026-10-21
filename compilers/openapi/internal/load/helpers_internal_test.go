package load

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// buildDoc runs a source through the steps build runs before resolving:
// decode, release anchors, populate the model. It returns the populated
// document and the raw tree, so a test can drive one stage directly instead
// of paying for the whole Load pipeline.
func buildDoc(t *testing.T, spec string) (*soa.OpenAPI, *yaml.Node) {
	t.Helper()
	parsed, err := parsedFor(compilers.Source{Path: "spec.yaml", Data: []byte(spec)})
	require.NoError(t, err)
	releaseAnchors(parsed.root)
	doc, _, err := unmarshal(t.Context(), []byte(spec), parsed.root)
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, parsed.root
}

// mappingSpec is a source whose component schemas are schemas, and whose
// other top-level entries are rest, which follows them.
func mappingSpec(schemas, rest string) string {
	return openapitest.ComponentSpec(schemas) + rest
}

// petMapping is a Pet base whose discriminator maps each tag to its target.
func petMapping(mapping string) string {
	return "    Pet:\n      type: object\n      discriminator: {propertyName: k, mapping: {" + mapping + "}}\n"
}

// loadTargets loads spec at path, external references allowed or not, and
// requires it to load.
func loadTargets(t *testing.T, path, spec string, external bool) (*Document, []ir.Diagnostic) {
	t.Helper()
	got, diags, err := Load(t.Context(), 0, compilers.Source{Path: path, Data: []byte(spec)},
		Options{AllowExternalRefs: external})
	require.NoError(t, err)
	require.NotNil(t, got, "%+v", diags)
	return got, diags
}
