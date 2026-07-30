// This file is the carrier half of annotation retention. A carrier is a
// position whose annotations land on an ir.Property or an ir.Parameter rather
// than on a type node, and until the matrix grew an axis for it the grid could
// not express a cell there at all — so two dropped-annotation defects at a
// parameter lived in a position the matrix was structurally silent about, while
// looking complete (GitHub #142).
package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// carrierPropertySpec wraps a schema body as the single property of one model.
func carrierPropertySpec(body string) string {
	return `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        f: ` + body + `
`
}

// carrierParamSpec wraps the same schema body as the single parameter of one
// operation. The two builders take identical bodies on purpose: a cell at each
// carrier then exercises the same declaration, so a difference between them is
// a difference in the carriers rather than in the fixtures.
func carrierParamSpec(body string) string {
	return `openapi: 3.1.0
info: {title: g, version: "1"}
paths:
  /x:
    get:
      operationId: op
      parameters:
        - name: q
          in: query
          schema: ` + body + `
      responses: {"200": {description: ok}}
`
}

// carriedProperty returns the single property carrierPropertySpec declares.
func carriedProperty(t *testing.T, doc *ir.Document) ir.Property {
	t.Helper()
	m, ok := doc.Types[namedID("S")].(*ir.Model)
	require.True(t, ok, "component S must lower to a model")
	p, ok := propByWire(m, "f")
	require.True(t, ok, "the model must carry property f")
	return p
}

// carriedParameter returns the single parameter carrierParamSpec declares.
func carriedParameter(t *testing.T, doc *ir.Document) ir.Parameter {
	t.Helper()
	op, ok := opByName(doc, "op")
	require.True(t, ok, "the operation must lower")
	p, ok := paramByName(op, "q")
	require.True(t, ok, "the operation must carry parameter q")
	return p
}

// paramSchemaPtr is the pointer of the parameter schema carrierParamSpec
// declares, which the fieldless cells report their preservation at.
const paramSchemaPtr = "/paths/~1x/get/parameters/0/schema"

// carrierCases returns the annotation-retention cases for the two carriers.
//
// Every cell asserts retention rather than a gap. The two annotations
// ir.Parameter has no field for — the XML hints and the visibility keywords —
// are kept verbatim under Unmodeled with a diagnostic naming them, so nothing
// is dropped at either carrier; those cells assert the preservation rather than
// an absence.
func carrierCases() []retentionCase {
	return []retentionCase{
		docsPropertyCase(), docsParameterCase(),
		examplesPropertyCase(), examplesParameterCase(),
		constraintsPropertyCase(), constraintsParameterCase(),
		defaultPropertyCase(), defaultParameterCase(),
		deprecatedPropertyCase(), deprecatedParameterCase(),
		visibilityPropertyCase(), visibilityParameterCase(),
		vendorExtensionPropertyCase(), vendorExtensionParameterCase(),
		xmlHintsPropertyCase(), xmlHintsParameterCase(),
		validationOnlyPropertyCase(), validationOnlyParameterCase(),
	}
}

// carrierCell builds the cell for one annotation at one carrier.
func carrierCell(a harness.Annotation, k harness.SiteKind) harness.Cell {
	return harness.Cell{Annotation: a, SiteKind: k}
}

func docsPropertyCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationDocs, harness.SiteCarrierProperty),
		spec: carrierPropertySpec(`{type: string, description: at-carrier}`),
		assert: func(t *testing.T, doc *ir.Document) {
			assert.Equal(t, "at-carrier", carriedProperty(t, doc).Docs.Description)
			assert.Empty(t, primitiveNode(t, doc, ir.TypeID("t/prim/string")).Common().Docs.Description,
				"a carrier's docs must not leak onto the shared primitive")
		},
	}
}

func docsParameterCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationDocs, harness.SiteCarrierParameter),
		spec: carrierParamSpec(`{type: string, description: at-carrier}`),
		assert: func(t *testing.T, doc *ir.Document) {
			assert.Equal(t, "at-carrier", carriedParameter(t, doc).Docs.Description)
		},
	}
}

func examplesPropertyCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationExamples, harness.SiteCarrierProperty),
		spec: carrierPropertySpec(`{type: string, example: at-carrier}`),
		assert: func(t *testing.T, doc *ir.Document) {
			ex := carriedProperty(t, doc).Examples
			require.Len(t, ex, 1)
			require.NotNil(t, ex[0].Value)
			assert.Equal(t, "at-carrier", ex[0].Value.Str)
		},
	}
}

func examplesParameterCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationExamples, harness.SiteCarrierParameter),
		spec: carrierParamSpec(`{type: string, example: at-carrier}`),
		assert: func(t *testing.T, doc *ir.Document) {
			ex := carriedParameter(t, doc).Examples
			require.Len(t, ex, 1)
			require.NotNil(t, ex[0].Value)
			assert.Equal(t, "at-carrier", ex[0].Value.Str)
		},
	}
}

func constraintsPropertyCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationConstraints, harness.SiteCarrierProperty),
		spec: carrierPropertySpec(`{type: string, minLength: 2}`),
		assert: func(t *testing.T, doc *ir.Document) {
			p := carriedProperty(t, doc)
			require.NotNil(t, p.Constraints)
			require.NotNil(t, p.Constraints.MinLength)
			assert.Equal(t, int64(2), *p.Constraints.MinLength)
			assert.Equal(t, ir.TypeID("t/prim/string"), p.Type.Target,
				"the bound rides on the carrier, so the position still resolves to the shared primitive")
		},
	}
}

func constraintsParameterCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationConstraints, harness.SiteCarrierParameter),
		spec: carrierParamSpec(`{type: string, minLength: 2}`),
		assert: func(t *testing.T, doc *ir.Document) {
			c := carriedParameter(t, doc).Constraints
			require.NotNil(t, c)
			require.NotNil(t, c.MinLength)
			assert.Equal(t, int64(2), *c.MinLength)
		},
	}
}

func defaultPropertyCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationDefault, harness.SiteCarrierProperty),
		spec: carrierPropertySpec(`{type: string, default: at-carrier}`),
		assert: func(t *testing.T, doc *ir.Document) {
			d := carriedProperty(t, doc).Default
			require.NotNil(t, d)
			assert.Equal(t, "at-carrier", d.Str)
		},
	}
}

func defaultParameterCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationDefault, harness.SiteCarrierParameter),
		spec: carrierParamSpec(`{type: string, default: at-carrier}`),
		assert: func(t *testing.T, doc *ir.Document) {
			d := carriedParameter(t, doc).Default
			require.NotNil(t, d)
			assert.Equal(t, "at-carrier", d.Str)
		},
	}
}

func deprecatedPropertyCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationDeprecated, harness.SiteCarrierProperty),
		spec: carrierPropertySpec(`{type: string, deprecated: true}`),
		assert: func(t *testing.T, doc *ir.Document) {
			assert.NotNil(t, carriedProperty(t, doc).Deprecation)
			assert.Nil(t, primitiveNode(t, doc, ir.TypeID("t/prim/string")).Common().Deprecation,
				"a carrier's deprecation must not leak onto the shared primitive")
		},
	}
}

func deprecatedParameterCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationDeprecated, harness.SiteCarrierParameter),
		spec: carrierParamSpec(`{type: string, deprecated: true}`),
		assert: func(t *testing.T, doc *ir.Document) {
			assert.NotNil(t, carriedParameter(t, doc).Deprecation)
		},
	}
}

func visibilityPropertyCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationVisibility, harness.SiteCarrierProperty),
		spec: carrierPropertySpec(`{type: string, readOnly: true}`),
		assert: func(t *testing.T, doc *ir.Document) {
			assert.Equal(t, []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery},
				carriedProperty(t, doc).Visibility.Only)
		},
	}
}

// visibilityParameterCase is one of the two cells that made a separate
// parameter kind necessary. ir.Parameter has no Visibility field, so readOnly
// reaches nothing there and is kept verbatim instead of dropped — a different
// outcome from the property carrier for the same declaration, which one shared
// "carrier" row could not have stated.
func visibilityParameterCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationVisibility, harness.SiteCarrierParameter),
		spec: carrierParamSpec(`{type: string, readOnly: true}`),
		assert: func(t *testing.T, doc *ir.Document) {
			entry := unmodeledEntry(t, carriedParameter(t, doc).Unmodeled, "openapi:readOnly")
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
			assert.JSONEq(t, `true`, string(entry.Value))
		},
		assertDiags: func(t *testing.T, diags []ir.Diagnostic) {
			assert.Equal(t, []ir.Severity{ir.SeverityInfo},
				diagsAt(diags, "openapi/degraded-construct", paramSchemaPtr+"/readOnly"),
				"the position with no field says so rather than dropping in silence")
		},
	}
}

func vendorExtensionPropertyCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationVendorExtension, harness.SiteCarrierProperty),
		spec: carrierPropertySpec(`{type: string, x-vendor: at-carrier}`),
		assert: func(t *testing.T, doc *ir.Document) {
			entry := unmodeledEntry(t, carriedProperty(t, doc).Unmodeled, "openapi:x-vendor")
			assert.Equal(t, ir.ReasonVendorExtension, entry.Reason)
			assert.JSONEq(t, `"at-carrier"`, string(entry.Value))
		},
	}
}

func vendorExtensionParameterCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationVendorExtension, harness.SiteCarrierParameter),
		spec: carrierParamSpec(`{type: string, x-vendor: at-carrier}`),
		assert: func(t *testing.T, doc *ir.Document) {
			entry := unmodeledEntry(t, carriedParameter(t, doc).Unmodeled, "openapi:x-vendor")
			assert.Equal(t, ir.ReasonVendorExtension, entry.Reason)
			assert.JSONEq(t, `"at-carrier"`, string(entry.Value))
		},
	}
}

func xmlHintsPropertyCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationXMLHints, harness.SiteCarrierProperty),
		spec: carrierPropertySpec(`{type: string, xml: {name: AtCarrier}}`),
		assert: func(t *testing.T, doc *ir.Document) {
			x := carriedProperty(t, doc).XML
			require.NotNil(t, x, "ir.Property has an XML field, so the hints land on it")
			assert.Equal(t, "AtCarrier", x.Name)
		},
	}
}

// xmlHintsParameterCase is the other cell the parameter kind exists for.
// ir.Parameter is the one annotation carrier with no XML field, so the same
// declaration that fills Property.XML is kept verbatim here instead.
func xmlHintsParameterCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationXMLHints, harness.SiteCarrierParameter),
		spec: carrierParamSpec(`{type: string, xml: {name: AtCarrier}}`),
		assert: func(t *testing.T, doc *ir.Document) {
			entry := unmodeledEntry(t, carriedParameter(t, doc).Unmodeled, "openapi:xml")
			assert.Equal(t, ir.ReasonNoIRHome, entry.Reason)
			assert.JSONEq(t, `{"name":"AtCarrier"}`, string(entry.Value))
		},
		assertDiags: func(t *testing.T, diags []ir.Diagnostic) {
			assert.Equal(t, []ir.Severity{ir.SeverityInfo},
				diagsAt(diags, "openapi/degraded-construct", paramSchemaPtr+"/xml"),
				"the position with no field says so rather than dropping in silence")
		},
	}
}

func validationOnlyPropertyCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationValidationOnly, harness.SiteCarrierProperty),
		spec: carrierPropertySpec(`{type: string, not: {const: nope}}`),
		assert: func(t *testing.T, doc *ir.Document) {
			entry := unmodeledEntry(t, carriedProperty(t, doc).Unmodeled, "openapi:not")
			assert.Equal(t, ir.ReasonValidationOnly, entry.Reason)
			assert.JSONEq(t, `{"const":"nope"}`, string(entry.Value))
		},
	}
}

func validationOnlyParameterCase() retentionCase {
	return retentionCase{
		cell: carrierCell(harness.AnnotationValidationOnly, harness.SiteCarrierParameter),
		spec: carrierParamSpec(`{type: string, not: {const: nope}}`),
		assert: func(t *testing.T, doc *ir.Document) {
			entry := unmodeledEntry(t, carriedParameter(t, doc).Unmodeled, "openapi:not")
			assert.Equal(t, ir.ReasonValidationOnly, entry.Reason)
			assert.JSONEq(t, `{"const":"nope"}`, string(entry.Value))
		},
	}
}

// TestAnnotationRetention_CarriersDifferWhereTheirFieldsDo states the reason
// the two carriers are separate rows rather than one, as a claim rather than an
// arrangement: the same declaration reaches a field on one and Unmodeled on the
// other. A single "carrier" kind would have had to pick one of these answers,
// and covering either would have read as covering both.
func TestAnnotationRetention_CarriersDifferWhereTheirFieldsDo(t *testing.T) {
	t.Parallel()
	const body = `{type: string, xml: {name: AtCarrier}, readOnly: true}`

	propDoc, diags := compileAnnotationSpec(t, "carrier-property", carrierPropertySpec(body))
	assertNoErrorDiags(t, diags)
	prop := carriedProperty(t, propDoc)
	require.NotNil(t, prop.XML, "ir.Property has an XML field")
	assert.NotEmpty(t, prop.Visibility.Only, "and a Visibility field")
	assert.NotContains(t, prop.Unmodeled, "openapi:xml", "so neither is kept verbatim there")
	assert.NotContains(t, prop.Unmodeled, "openapi:readOnly")

	paramDoc, diags := compileAnnotationSpec(t, "carrier-parameter", carrierParamSpec(body))
	assertNoErrorDiags(t, diags)
	param := carriedParameter(t, paramDoc)
	assert.Contains(t, param.Unmodeled, "openapi:xml", "ir.Parameter has neither field")
	assert.Contains(t, param.Unmodeled, "openapi:readOnly")
}
