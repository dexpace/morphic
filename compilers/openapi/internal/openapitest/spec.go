package openapitest

import (
	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
)

// SourceOf wraps a spec string as a compilers.Source.
func SourceOf(src string) compilers.Source {
	return compilers.Source{Path: "spec.yaml", Data: []byte(src)}
}

// ComponentSpec wraps a components/schemas block in a minimal 3.1 document.
func ComponentSpec(schemas string) string {
	return ComponentSpecVer("3.1.0", schemas)
}

// ComponentSpecVer wraps a components/schemas block in a minimal document of
// the given OpenAPI version.
func ComponentSpecVer(version, schemas string) string {
	return "openapi: " + version + "\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths: {}\n" +
		"components:\n  schemas:\n" + schemas
}

// PathsSpec wraps a paths block in a minimal 3.1 document with no components.
func PathsSpec(paths string) string {
	return PathsSpecVer("3.1.0", paths)
}

// PathsSpecVer wraps a paths block in a minimal document of the given OpenAPI
// version, with no components.
func PathsSpecVer(version, paths string) string {
	return "openapi: " + version + "\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths:\n" + paths
}

// DocDeclaring builds a document declaring the named component schemas, with no
// parser and no fixture — the shape a test wants when what it needs from the
// document is only which components it declares.
func DocDeclaring(names ...string) *soa.OpenAPI {
	elems := make([]*sequencedmap.Element[string, *oas3.JSONSchema[oas3.Referenceable]], 0, len(names))
	for _, n := range names {
		elems = append(elems, sequencedmap.NewElem(n,
			oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})))
	}
	return &soa.OpenAPI{Components: &soa.Components{Schemas: sequencedmap.New(elems...)}}
}

// EmptyEitherSchema is a JSONSchema whose either-value has neither a Left schema
// nor a Right bool set: IsSchema() is true (IsLeft defaults true) yet
// GetSchema() is nil. The parser never produces this, so it drives the
// nil-schema guards.
func EmptyEitherSchema() *oas3.JSONSchema[oas3.Referenceable] {
	return oas3.NewJSONSchemaFromSchema[oas3.Referenceable](nil)
}

// YAMLNode parses a YAML snippet and returns its root value node (the document
// node's single content child), matching what schema fields expose.
func YAMLNode(t TB, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	require.Len(t, doc.Content, 1, "expected a single document node")
	return doc.Content[0]
}

// StrNode builds a bare string-scalar yaml.Node.
func StrNode(val string) *yaml.Node {
	return ScalarNode("!!str", val)
}

// ScalarNode builds a bare scalar yaml.Node with the given tag and value.
func ScalarNode(tag, val string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: val}
}

// InlineProbeBody is the body every inline-position case writes: one annotation
// of each kind the declared-annotation reader takes, one validation-only
// keyword, and one value constraint — all of them position-scoped, so a position
// that lowers this to the shared string primitive loses every one. All three
// documentation keywords are here because a home that keeps only the description
// passes a probe that writes only a description.
const InlineProbeBody = `{type: string, title: SUM, description: DOC, ` +
	`externalDocs: {url: 'https://e.example', description: ED}, deprecated: true, ` +
	`example: abc, x-vendor: V, xml: {name: X}, not: {const: N}, maxLength: 3}`
