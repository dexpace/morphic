package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestModel_FourOptionalityStates(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      required: [reqPlain, reqNull]
      properties:
        reqPlain: {type: string}
        reqNull: {type: [string, "null"]}
        optPlain: {type: string}
        optNull: {type: [string, "null"]}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	require.True(t, ok)
	require.Len(t, m.Properties, 4)
	byName := map[string]ir.Property{}
	for _, p := range m.Properties {
		byName[p.WireName] = p
	}
	assert.True(t, byName["reqPlain"].Required)
	assert.False(t, byName["reqPlain"].Type.Nullable)
	assert.True(t, byName["reqNull"].Required)
	assert.True(t, byName["reqNull"].Type.Nullable)
	assert.False(t, byName["optPlain"].Required)
	assert.False(t, byName["optPlain"].Type.Nullable)
	assert.False(t, byName["optNull"].Required)
	assert.True(t, byName["optNull"].Type.Nullable)
}

func TestConstraints_NumericPrecisionSurvives(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        ratio:
          type: number
          minimum: 0.30000000000000004
          maximum: 9007199254740993
          multipleOf: 0.1
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	c := m.Properties[0].Constraints
	require.NotNil(t, c)
	// Exact decimal strings — a float64 path would corrupt all three.
	assert.Equal(t, ir.BigVal("0.30000000000000004"), *c.Min)
	assert.Equal(t, ir.BigVal("9007199254740993"), *c.Max)
	assert.Equal(t, ir.BigVal("0.1"), *c.MultipleOf)
}

func TestModel_ValidationOnlyKeywordPreserved(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties: {a: {type: string}}
      not: {required: [b]}
`
	doc, diags := lowerSpec(t, spec)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	raw, ok := m.Extensions["openapi:not"]
	require.True(t, ok, "not-keyword must be preserved verbatim")
	assert.JSONEq(t, `{"required":["b"]}`, string(raw))
	found := false
	for _, d := range diags {
		if d.Code == codeValidationOnlyKeyword {
			found = true
			assert.Equal(t, ir.SeverityInfo, d.Severity)
		}
	}
	assert.True(t, found, "expected a validation-only-keyword info diagnostic")
}

func TestModel_DefaultBigLiteral(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        n: {type: integer, default: 9007199254740993}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	require.NotNil(t, m.Properties[0].Default)
	assert.Equal(t, ir.ValueNumber, m.Properties[0].Default.Kind)
	assert.Equal(t, ir.BigVal("9007199254740993"), m.Properties[0].Default.Num)
}

func TestModel_ReadOnlyWriteOnlyVisibility(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        r: {type: string, readOnly: true}
        w: {type: string, writeOnly: true}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	byName := map[string]ir.Property{}
	for _, p := range m.Properties {
		byName[p.WireName] = p
	}
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead}}, byName["r"].Visibility)
	assert.Equal(t, ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}, byName["w"].Visibility)
}

func TestModel_PasswordFormatSecret(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        pw: {type: string, format: password}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	assert.True(t, m.Properties[0].Secret)
}

func TestModel_AdditionalPropertiesFalseClosed(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties: {a: {type: string}}
      additionalProperties: false
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	assert.Equal(t, ir.AdditionalClosed, m.Additional)
}

func TestModel_AdditionalPropertiesSchema(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      additionalProperties: {type: integer}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	require.NotNil(t, m.AdditionalProps)
	assert.Equal(t, ir.TypeID("t/prim/integer"), m.AdditionalProps.Value.Target)
}

func TestModel_PatternPropertiesOrder(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      patternProperties:
        "^x-": {type: string}
        "^y-": {type: integer}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	require.NotNil(t, m.AdditionalProps)
	require.Len(t, m.AdditionalProps.Patterns, 2)
	assert.Equal(t, "^x-", m.AdditionalProps.Patterns[0].Pattern)
	assert.Equal(t, "^y-", m.AdditionalProps.Patterns[1].Pattern)
}

func TestModel_UnevaluatedPropertiesClosedAfterComposition(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties: {a: {type: string}}
      unevaluatedProperties: false
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	assert.Equal(t, ir.AdditionalClosedAfterComposition, m.Additional)
}

func TestModel_SchemaExtensionPreserved(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      x-rate-limit: 100
      properties: {a: {type: string}}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	raw, ok := m.Extensions["openapi:x-rate-limit"]
	require.True(t, ok)
	assert.JSONEq(t, "100", string(raw))
}

func TestModel_TitleDescriptionDocs(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      title: "My Title"
      description: "My Desc"
      properties: {a: {type: string}}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	assert.Equal(t, "My Title", m.Docs.Summary)
	assert.Equal(t, "My Desc", m.Docs.Description)
}

func TestModel_PropertyDeprecation(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        old: {type: string, deprecated: true}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	assert.NotNil(t, m.Properties[0].Deprecation)
}

func TestModel_PropertyXML(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        p: {type: string, xml: {name: n, attribute: true}}
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	require.NotNil(t, m.Properties[0].XML)
	assert.Equal(t, "n", m.Properties[0].XML.Name)
	assert.Equal(t, "attribute", m.Properties[0].XML.NodeType)
}

func TestModel_RefSiblingDescriptionWins(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string, description: "target desc"}
    S:
      type: object
      properties:
        p:
          $ref: '#/components/schemas/Target'
          description: "sibling desc"
`
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m := doc.Types[ir.TypeID("t/openapi/components/schemas/S")].(*ir.Model)
	assert.Equal(t, "sibling desc", m.Properties[0].Docs.Description)
}
