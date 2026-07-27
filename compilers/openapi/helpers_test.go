package openapi

import (
	"context"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// deepPointer is a sub-schema pointer (not a top-level component pointer), so it
// exercises the interning-lookup paths rather than the named-component path.
// Shared across ids_test.go and schema_test.go.
const deepPointer = "/components/schemas/Obj/properties/inner"

// sourceOf wraps a spec string as a compilers.Source.
func sourceOf(src string) compilers.Source {
	return compilers.Source{Path: "spec.yaml", Data: []byte(src)}
}

// parseFull runs the whole public compiler pipeline over src.
func parseFull(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := New().Compile(context.Background(),
		[]compilers.Source{{Path: "spec.yaml", Data: []byte(src)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// findOp returns the operation whose source name matches.
func findOp(t *testing.T, doc *ir.Document, source string) ir.Operation {
	t.Helper()
	for _, g := range doc.Services[0].Groups {
		for _, op := range g.Operations {
			if op.Name.Source == source {
				return op
			}
		}
	}
	t.Fatalf("operation %q not found", source)
	return ir.Operation{}
}

// lowerSpec loads src and lowers its component schemas, returning the document
// under construction and all diagnostics. It drives the same lowerer Compile
// will use, without requiring the not-yet-written operation lowering.
func lowerSpec(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, diags, err := load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, Options{}.withDefaults())
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	l := newLowerer(0, loadedDoc, Options{}.withDefaults())
	l.lowerComponentSchemas() // named components; the entry Compile's run() calls first
	return l.out, append(diags, l.diags...)
}

// requireNoErrorDiags fails the test if any diagnostic has error severity.
func requireNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		require.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
}

// lowerServiceSpec lowers components and the service layer of src.
func lowerServiceSpec(t *testing.T, src string) (*ir.Document, ir.Service, []ir.Diagnostic) {
	t.Helper()
	doc, diags := func() (*ir.Document, []ir.Diagnostic) {
		loadedDoc, loadDiags, err := load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, Options{}.withDefaults())
		require.NoError(t, err)
		require.NotNil(t, loadedDoc)
		l := newLowerer(0, loadedDoc, Options{}.withDefaults())
		l.lowerComponentSchemas()
		l.lowerSecuritySchemes()
		l.out.Services = []ir.Service{l.lowerService()}
		return l.out, append(loadDiags, l.diags...)
	}()
	require.Len(t, doc.Services, 1)
	return doc, doc.Services[0], diags
}

// componentSpec wraps a components/schemas block in a minimal 3.1 document.
func componentSpec(schemas string) string {
	return componentSpecVer("3.1.0", schemas)
}

// componentSpecVer wraps a components/schemas block in a minimal document of the
// given OpenAPI version.
func componentSpecVer(version, schemas string) string {
	return "openapi: " + version + "\n" +
		"info: {title: T, version: \"1\"}\n" +
		"paths: {}\n" +
		"components:\n  schemas:\n" + schemas
}

// typeByName returns the named component schema's lowered TypeDef.
func typeByName(doc *ir.Document, name string) ir.TypeDef {
	return doc.Types[ir.TypeID("t/openapi/components/schemas/"+name)]
}

// conflictDiags returns every conflicting-redeclaration diagnostic in diags.
func conflictDiags(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == codeConflictingRedecl {
			out = append(out, d)
		}
	}
	return out
}

// newRawLowerer builds a lowerer over a hand-constructed document, bypassing the
// parser so nil slice/map entries (which the parser panics on) can be exercised.
func newRawLowerer(doc *soa.OpenAPI) *lowerer {
	return &lowerer{
		doc:                  doc,
		out:                  &ir.Document{Types: ir.TypeRegistry{}},
		byPointer:            map[string]ir.TypeID{},
		diagnosedConstraints: map[string]bool{},
	}
}

// emptyEitherSchema is a JSONSchema whose either-value has neither a Left schema
// nor a Right bool set: IsSchema() is true (IsLeft defaults true) yet GetSchema()
// is nil. The parser never produces this, so it drives the nil-schema guards.
func emptyEitherSchema() *oas3.JSONSchema[oas3.Referenceable] {
	return oas3.NewJSONSchemaFromSchema[oas3.Referenceable](nil)
}

// yamlNode parses a YAML snippet and returns its root value node (the
// document node's single content child), matching what schema fields expose.
func yamlNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	require.Len(t, doc.Content, 1, "expected a single document node")
	return doc.Content[0]
}

// scalarNode builds a bare scalar yaml.Node with the given tag and value.
func scalarNode(tag, val string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: val}
}
