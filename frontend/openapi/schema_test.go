package openapi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/frontend"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// lowerSpec loads src and lowers its component schemas, returning the document
// under construction and all diagnostics. It drives the same lowerer Parse
// will use, without requiring the not-yet-written operation lowering.
func lowerSpec(t *testing.T, src string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	loadedDoc, diags, err := load(t.Context(), 0, frontend.Source{Path: "spec.yaml", Data: []byte(src)}, Options{}.withDefaults())
	require.NoError(t, err)
	require.NotNil(t, loadedDoc, "load returned no document: %+v", diags)
	l := newLowerer(0, loadedDoc, Options{}.withDefaults())
	l.lowerComponentSchemas() // named components; the entry Parse's run() calls first
	return l.out, append(diags, l.diags...)
}

// requireNoErrorDiags fails the test if any diagnostic has error severity.
func requireNoErrorDiags(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		require.NotEqual(t, ir.SeverityError, d.Severity, "unexpected error diagnostic: %+v", d)
	}
}

func TestSchemaRef_NullableNormalization(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, version, schema string // YAML fragment under components/schemas/S/properties/p
		wantNullable          bool
		wantTarget            ir.TypeID
	}{
		{"3.0 nullable", "3.0.3", "{type: string, nullable: true}", true, "t/prim/string"},
		{"3.1 type array", "3.1.0", `{type: [string, "null"]}`, true, "t/prim/string"},
		{"plain", "3.1.0", "{type: string}", false, "t/prim/string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := "openapi: " + tc.version + "\n" +
				"info: {title: T, version: \"1\"}\n" +
				"paths: {}\n" +
				"components:\n" +
				"  schemas:\n" +
				"    S:\n" +
				"      type: object\n" +
				"      properties:\n" +
				"        p: " + tc.schema + "\n"
			doc, diags := lowerSpec(t, spec)
			requireNoErrorDiags(t, diags)
			model, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
			require.True(t, ok)
			require.Len(t, model.Properties, 1)
			assert.Equal(t, tc.wantTarget, model.Properties[0].Type.Target)
			assert.Equal(t, tc.wantNullable, model.Properties[0].Type.Nullable)
		})
	}
}

func TestLower_NamedScalarComponentResolves(t *testing.T) {
	t.Parallel()
	// A named component whose body is a plain scalar must register a resolvable
	// node at its own component pointer, so a $ref to it never dangles.
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    MyId: {type: string, format: uuid}
    Holder:
      type: object
      properties:
        id: {$ref: "#/components/schemas/MyId"}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	scalar, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/MyId")].(*ir.Scalar)
	require.True(t, ok, "named scalar component registers a Scalar at its own ID")
	require.NotNil(t, scalar.Base)
	assert.Equal(t, ir.TypeID("t/prim/uuid"), scalar.Base.Target)
	assert.Equal(t, "MyId", scalar.Name.Source, "the component name is preserved")

	holder := doc.Types[ir.TypeID("t/openapi/components/schemas/Holder")].(*ir.Model)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/MyId"), holder.Properties[0].Type.Target)

	// The reference must resolve: the validate pass finds no dangling type ref.
	for _, d := range pass.Validate(doc) {
		assert.NotEqual(t, "ir/dangling-type-ref", d.Code, "ref to named scalar dangles: %+v", d)
	}
}

func TestLower_OneOfWithStructuralSiblingsPreserved(t *testing.T) {
	t.Parallel()
	// The "exactly one of" idiom co-declares object structure with oneOf. The
	// structural body must survive AND the union must be preserved verbatim.
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Thing:
      type: object
      additionalProperties: false
      required: [common]
      properties:
        common: {type: string}
      oneOf:
        - {required: [a]}
        - {required: [b]}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)

	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Thing")].(*ir.Model)
	require.True(t, ok, "structural body lowers to a Model, not a bare Union")
	require.Len(t, m.Properties, 1)
	assert.Equal(t, "common", m.Properties[0].Name.Source)
	assert.True(t, m.Properties[0].Required)
	assert.Equal(t, ir.AdditionalClosed, m.Additional)
	raw, ok := m.Extensions["openapi:oneOf"]
	require.True(t, ok, "the dropped union is preserved verbatim under extensions")
	assert.Contains(t, string(raw), "required")

	found := false
	for _, d := range diags {
		if d.Severity == ir.SeverityInfo && strings.Contains(d.Message, "oneOf/anyOf co-declared") {
			found = true
		}
	}
	assert.True(t, found, "coexistence emits one info diagnostic")
}

func TestLower_AllOfWithOneOfKeepsBoth(t *testing.T) {
	t.Parallel()
	// allOf co-declared with oneOf must not drop the allOf composition.
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Base:
      type: object
      properties:
        id: {type: string}
    Combo:
      allOf:
        - {$ref: "#/components/schemas/Base"}
      oneOf:
        - {type: string}
        - {type: integer}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Combo")].(*ir.Model)
	require.True(t, ok, "allOf composition survives (Model), oneOf preserved raw")
	require.NotNil(t, m.Base, "the allOf $ref becomes Base")
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Base"), m.Base.Target)
	_, ok = m.Extensions["openapi:oneOf"]
	assert.True(t, ok, "the oneOf is preserved verbatim under extensions")
}

func TestLower_RecursiveSchemaTerminates(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Node:
      type: object
      properties:
        next: {$ref: "#/components/schemas/Node"}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	node, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/Node")].(*ir.Model)
	require.True(t, ok)
	require.Equal(t, ir.TypeRef{Target: "t/openapi/components/schemas/Node"}, node.Properties[0].Type)
}

func TestLower_InlineSchemaHoistedOnce(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        tags:
          type: array
          items:
            type: object
            properties:
              name: {type: string}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	itemsID := ir.TypeID("t/anon/components/schemas/S/properties/tags/items")
	item, ok := doc.Types[itemsID].(*ir.Model)
	require.True(t, ok, "items object should be hoisted as a model")
	assert.True(t, item.Anonymous)
	assert.Equal(t, "tags_item", item.Name.Hint)
	assert.Empty(t, item.Name.Source, "hoisted inline types carry a hint, not a source name")
}

func TestCanonicalWords(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"userID": "user_id", "HTTPServer": "http_server", "list-users": "list_users",
		"User": "user", "APIKey2": "api_key_2",
	} {
		assert.Equal(t, want, canonicalWords(in), "input %q", in)
	}
}
