// This file is the OpenAPI half of annotation retention: one case per
// annotation per kind of position, plus an assertion that no combination is
// silently uncovered. It complements conformance_test.go, which covers
// capability rows rather than annotation positions.
package openapi_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// minGapReasonLen is the shortest a knownGap reason may be. It is cheap
// insurance against a placeholder like "todo" masquerading as the required
// explanation of why the compiler drops an annotation.
const minGapReasonLen = 20

// retentionCase is one cell of annotation retention: a minimal spec exercising
// one annotation at one kind of position, and the assertion that the
// annotation survived to the right place in the IR.
type retentionCase struct {
	cell harness.Cell
	spec string

	// knownGap, when non-empty, records that the compiler currently drops this
	// annotation at this kind of position and why there is nowhere for it to go.
	// The case then asserts the annotation is ABSENT, so closing the gap turns
	// the cell red and whoever closes it removes the marker.
	knownGap string

	assert func(*testing.T, *ir.Document)

	// assertDiags, when set, additionally checks the compile's diagnostics.
	assertDiags func(*testing.T, []ir.Diagnostic)
}

// compileAnnotationSpec compiles one in-memory annotation-retention spec.
// Every spec in this suite is well-formed by construction, so compilation is
// required to succeed with no Go error; the caller checks diagnostics
// separately, against what its own cell expects.
func compileAnnotationSpec(t *testing.T, name, spec string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".yaml", Data: []byte(spec)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

func TestAnnotationRetention(t *testing.T) {
	t.Parallel()
	for _, tc := range retentionCases() {
		name := string(tc.cell.Annotation) + "/" + string(tc.cell.SiteKind)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileAnnotationSpec(t, name, tc.spec)
			assertNoErrorDiags(t, diags)
			tc.assert(t, doc)
			if tc.assertDiags != nil {
				tc.assertDiags(t, diags)
			}
		})
	}
}

// TestAnnotationRetention_KnownGaps logs every cell marked knownGap so the gaps
// stay visible in test output rather than buried inside a passing subtest's
// body, and checks the marker itself: retentionCase's doc comment promises
// that a knownGap case asserts absence, so a case missing its own assertion,
// or carrying a reason too short to explain anything, would let a gap go
// unenforced or unexplained.
func TestAnnotationRetention_KnownGaps(t *testing.T) {
	t.Parallel()
	for _, tc := range retentionCases() {
		assert.NotNil(t, tc.assert, "%s/%s: every case must assert something, gap or not",
			tc.cell.Annotation, tc.cell.SiteKind)
		if tc.knownGap == "" {
			continue
		}
		t.Logf("GAP %s/%s: %s", tc.cell.Annotation, tc.cell.SiteKind, tc.knownGap)
		reason := strings.TrimSpace(tc.knownGap)
		assert.GreaterOrEqual(t, len(reason), minGapReasonLen,
			"%s/%s: knownGap reason is too short to be a real explanation: %q",
			tc.cell.Annotation, tc.cell.SiteKind, tc.knownGap)
	}
}

func retentionCases() []retentionCase {
	return slices.Concat(
		examplesCases(),
		constraintsCases(),
		docsCases(),
		deprecatedCases(),
		defaultCases(),
		visibilityCases(),
		extensionsCases(),
		xmlHintsCases(),
		validationOnlyCases(),
	)
}

// examplesCases returns the annotation-retention cases for the examples
// annotation: an example value at each SiteKind.
func examplesCases() []retentionCase {
	return []retentionCase{
		{
			cell: harness.Cell{Annotation: harness.AnnotationExamples, SiteKind: harness.SiteDeclarationModel},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        f: {type: string}
      example: {f: at-declaration}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				td, ok := doc.Types[namedID("S")]
				require.True(t, ok, "component S must own a node")
				require.Len(t, td.Common().Examples, 1)
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationExamples, SiteKind: harness.SiteDeclarationScalar},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: string, example: at-declaration}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				sc, ok := doc.Types[namedID("S")].(*ir.Scalar)
				require.True(t, ok, "a bare scalar component owns a Scalar node")
				require.Len(t, sc.Examples, 1)
				require.NotNil(t, sc.Examples[0].Value)
				assert.Equal(t, "at-declaration", sc.Examples[0].Value.Str)
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationExamples, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          example: at-reference
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)

				// The example belongs to the position it is written at.
				require.Len(t, p.Examples, 1, "example beside a $ref belongs to the reference site")
				require.NotNil(t, p.Examples[0].Value)
				assert.Equal(t, "at-reference", p.Examples[0].Value.Str)

				// ...and must not have leaked onto the referent.
				target, ok := doc.Types[namedID("Target")]
				require.True(t, ok)
				assert.Empty(t, target.Common().Examples,
					"a reference-site example must not attach to the referent")
			},
		},
	}
}

// constraintsCases returns the annotation-retention cases for the constraints
// annotation: a value bound (e.g. minimum) at each SiteKind.
func constraintsCases() []retentionCase {
	return []retentionCase{
		{
			cell: harness.Cell{Annotation: harness.AnnotationConstraints, SiteKind: harness.SiteDeclarationModel},
			knownGap: "ir.Model has no Constraints field, and lowerModel already owns the component's " +
				"pointer before lowerComponentSchema's componentConstraints/internAlias fallback would run, " +
				"so that fallback never fires for an object-shaped component; minProperties is read into " +
				"Constraints.MinProps for a property or a scalar component (constraints.go), but an " +
				"object-shaped component's own minProperties has no field to land in and is dropped",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        f: {type: string}
      minProperties: 1
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok, "S still owns a Model node even though its minProperties is dropped")
				node := marshalToMap(t, m)
				assert.NotContains(t, node, "constraints",
					"minProperties has no field to land in for an object-shaped component (it is surfaced "+
						"for a property and a scalar component); closing this gap should turn this red")
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationConstraints, SiteKind: harness.SiteDeclarationScalar},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: integer, minimum: 5}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				sc, ok := doc.Types[namedID("S")].(*ir.Scalar)
				require.True(t, ok, "a constrained scalar component owns a Scalar node")
				require.NotNil(t, sc.Constraints)
				require.NotNil(t, sc.Constraints.Min)
				assert.Equal(t, "5", sc.Constraints.Min.String())
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationConstraints, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: integer}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          minimum: 5
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)
				require.NotNil(t, p.Constraints, "a bound beside a $ref binds the reference site")
				require.NotNil(t, p.Constraints.Min)
				assert.Equal(t, "5", p.Constraints.Min.String())

				target, ok := doc.Types[namedID("Target")].(*ir.Scalar)
				require.True(t, ok)
				assert.Nil(t, target.Constraints, "a reference-site bound must not attach to the referent")
			},
		},
	}
}

// docsCases returns the annotation-retention cases for the docs annotation: a
// description at each SiteKind.
func docsCases() []retentionCase {
	return []retentionCase{
		{
			cell: harness.Cell{Annotation: harness.AnnotationDocs, SiteKind: harness.SiteDeclarationModel},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        f: {type: string}
      description: at-declaration
`,
			assert: func(t *testing.T, doc *ir.Document) {
				td, ok := doc.Types[namedID("S")]
				require.True(t, ok)
				assert.Equal(t, "at-declaration", td.Common().Docs.Description)
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationDocs, SiteKind: harness.SiteDeclarationScalar},
			knownGap: "fillTypeDocs is only called from fillModelDetail; a scalar component's internAlias " +
				"path never calls it, so a scalar's own description is dropped",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: string, description: at-declaration}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				sc, ok := doc.Types[namedID("S")].(*ir.Scalar)
				require.True(t, ok, "a bare scalar component owns a Scalar node")
				assert.Empty(t, sc.Docs.Description,
					"docs is never set for a scalar declaration today; closing this gap should turn this red")
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationDocs, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          description: at-reference
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)
				assert.Equal(t, "at-reference", p.Docs.Description,
					"a description beside a $ref binds the reference site")

				target, ok := doc.Types[namedID("Target")]
				require.True(t, ok)
				assert.Empty(t, target.Common().Docs.Description,
					"a reference-site description must not attach to the referent")
			},
		},
	}
}

// deprecatedCases returns the annotation-retention cases for the deprecated
// annotation at each SiteKind.
func deprecatedCases() []retentionCase {
	return []retentionCase{
		{
			cell: harness.Cell{Annotation: harness.AnnotationDeprecated, SiteKind: harness.SiteDeclarationModel},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        f: {type: string}
      deprecated: true
`,
			assert: func(t *testing.T, doc *ir.Document) {
				td, ok := doc.Types[namedID("S")]
				require.True(t, ok)
				assert.NotNil(t, td.Common().Deprecation)
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationDeprecated, SiteKind: harness.SiteDeclarationScalar},
			knownGap: "effectiveDeprecated is only called from fillModelDetail (model) and " +
				"fillPropertyDetail (property); internAlias never calls it, so a scalar component's own " +
				"deprecated is dropped",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: string, deprecated: true}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				sc, ok := doc.Types[namedID("S")].(*ir.Scalar)
				require.True(t, ok, "a bare scalar component owns a Scalar node")
				assert.Nil(t, sc.Deprecation,
					"Deprecation is never set for a scalar declaration today; closing this gap should turn this red")
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationDeprecated, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          deprecated: true
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)
				assert.NotNil(t, p.Deprecation, "deprecated beside a $ref binds the reference site")

				target, ok := doc.Types[namedID("Target")]
				require.True(t, ok)
				assert.Nil(t, target.Common().Deprecation,
					"a reference-site deprecation must not attach to the referent")
			},
		},
	}
}

// defaultCases returns the annotation-retention cases for the default
// annotation at each SiteKind.
func defaultCases() []retentionCase {
	return []retentionCase{
		{
			cell: harness.Cell{Annotation: harness.AnnotationDefault, SiteKind: harness.SiteDeclarationModel},
			knownGap: "ir.Model and TypeCommon have no Default field, so a component-level default has " +
				"nowhere to land regardless of declaration shape",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        f: {type: string}
      default: {f: x}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok, "S still owns a Model node even though its default is dropped")
				node := marshalToMap(t, m)
				assert.NotContains(t, node, "default",
					"default is never surfaced anywhere in the IR today; closing this gap should turn this red")
			},
		},
		{
			cell:     harness.Cell{Annotation: harness.AnnotationDefault, SiteKind: harness.SiteDeclarationScalar},
			knownGap: "ir.Scalar and ir.TypeCommon have no Default field, so a component-level default has nowhere to land",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: integer, default: 7}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				td, ok := doc.Types[namedID("S")]
				require.True(t, ok, "S still owns a node even though its default is dropped")
				node := marshalToMap(t, td)
				assert.NotContains(t, node, "default",
					"default is never surfaced anywhere in the IR today; closing this gap should turn this red")
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationDefault, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: integer}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          default: 7
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)
				require.NotNil(t, p.Default, "a default beside a $ref binds the reference site")
				assert.Equal(t, "7", p.Default.Num.String())
			},
		},
	}
}

// visibilityCases returns the annotation-retention cases for the visibility
// annotation (readOnly) at each SiteKind.
func visibilityCases() []retentionCase {
	return []retentionCase{
		{
			cell: harness.Cell{Annotation: harness.AnnotationVisibility, SiteKind: harness.SiteDeclarationModel},
			knownGap: "ir.TypeCommon has no Visibility field; Access and Usage exist but the compiler " +
				"never sets them, for either declaration shape",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        f: {type: string}
      readOnly: true
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok, "S still owns a Model node even though its readOnly is dropped")
				node := marshalToMap(t, m)
				assert.NotContains(t, node, "visibility",
					"readOnly has no field to land in on a declaration site today; closing this gap should turn this red")
			},
		},
		{
			cell:     harness.Cell{Annotation: harness.AnnotationVisibility, SiteKind: harness.SiteDeclarationScalar},
			knownGap: "ir.TypeCommon has no Visibility field; Access and Usage exist but the compiler never sets them",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: string, readOnly: true}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				sc, ok := doc.Types[namedID("S")].(*ir.Scalar)
				require.True(t, ok, "a bare scalar component owns a Scalar node")
				node := marshalToMap(t, sc)
				assert.NotContains(t, node, "visibility",
					"readOnly has no field to land in on a declaration site today; closing this gap should turn this red")
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationVisibility, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          readOnly: true
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)
				assert.Equal(t,
					[]ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery},
					p.Visibility.Only)
			},
		},
	}
}

// extensionsCases returns the annotation-retention cases for the extensions
// annotation (x-vendor) at each SiteKind.
func extensionsCases() []retentionCase {
	return []retentionCase{
		{
			cell: harness.Cell{Annotation: harness.AnnotationExtensions, SiteKind: harness.SiteDeclarationModel},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        f: {type: string}
      x-vendor: at-declaration
`,
			assert: func(t *testing.T, doc *ir.Document) {
				td, ok := doc.Types[namedID("S")]
				require.True(t, ok)
				raw, ok := td.Common().Extensions["openapi:x-vendor"]
				require.True(t, ok, "x-vendor must be preserved under the openapi: namespace")
				assert.JSONEq(t, `"at-declaration"`, string(raw))
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationExtensions, SiteKind: harness.SiteDeclarationScalar},
			knownGap: "schemaExtensions is merged into Model.Extensions only inside fillModelDetail; " +
				"internAlias never attaches it, so a scalar component's own x-vendor is dropped",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: string, x-vendor: at-declaration}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				sc, ok := doc.Types[namedID("S")].(*ir.Scalar)
				require.True(t, ok, "a bare scalar component owns a Scalar node")
				assert.Empty(t, sc.Extensions,
					"Extensions is never set for a scalar declaration today; closing this gap should turn this red")
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationExtensions, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          x-vendor: at-reference
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)
				raw, ok := p.Extensions["openapi:x-vendor"]
				require.True(t, ok, "x-vendor beside a $ref binds the reference site")
				assert.JSONEq(t, `"at-reference"`, string(raw))

				target, ok := doc.Types[namedID("Target")]
				require.True(t, ok)
				assert.Empty(t, target.Common().Extensions,
					"a reference-site extension must not attach to the referent")
			},
		},
	}
}

// xmlHintsCases returns the annotation-retention cases for the xmlHints
// annotation at each SiteKind.
func xmlHintsCases() []retentionCase {
	return []retentionCase{
		{
			cell: harness.Cell{Annotation: harness.AnnotationXMLHints, SiteKind: harness.SiteDeclarationModel},
			knownGap: "ir.TypeCommon.XML exists but the OpenAPI compiler never assigns it; only " +
				"Property.XML is filled (fillPropertyDetail), so a component-level xml hint has " +
				"nowhere to land",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        f: {type: string}
      xml: {name: Renamed}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				td, ok := doc.Types[namedID("S")]
				require.True(t, ok)
				assert.Nil(t, td.Common().XML, "XML is never set today; closing this gap should turn this red")
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationXMLHints, SiteKind: harness.SiteDeclarationScalar},
			knownGap: "xmlHints() is called only from fillPropertyDetail; neither fillModelDetail nor " +
				"internAlias ever calls it, so a scalar component's own xml hint is dropped same as a model's",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: string, xml: {name: Renamed}}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				sc, ok := doc.Types[namedID("S")].(*ir.Scalar)
				require.True(t, ok, "a bare scalar component owns a Scalar node")
				assert.Nil(t, sc.XML, "XML is never set today; closing this gap should turn this red")
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationXMLHints, SiteKind: harness.SiteReference},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          xml: {name: Renamed}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)
				require.NotNil(t, p.XML, "an xml hint beside a $ref binds the reference site")
				assert.Equal(t, "Renamed", p.XML.Name)

				target, ok := doc.Types[namedID("Target")]
				require.True(t, ok)
				assert.Nil(t, target.Common().XML, "a reference-site xml hint must not attach to the referent")
			},
		},
	}
}

// validationOnlyCases returns the annotation-retention cases for the
// validationOnly annotation (if/then/else) at each SiteKind.
func validationOnlyCases() []retentionCase {
	return []retentionCase{
		{
			cell: harness.Cell{Annotation: harness.AnnotationValidationOnly, SiteKind: harness.SiteDeclarationModel},
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: object
      properties:
        f: {type: string}
      if: {type: string}
      then: {minLength: 1}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				raw, ok := m.Extensions["openapi:if-then-else"]
				require.True(t, ok, "if/then must be preserved verbatim in extensions")
				assert.JSONEq(t, `{"if":{"type":"string"},"then":{"minLength":1}}`, string(raw))
			},
			assertDiags: func(t *testing.T, diags []ir.Diagnostic) {
				assert.True(t, hasDiagCode(diags, "openapi/validation-only-keyword"),
					"expected a validation-only-keyword info diagnostic")
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationValidationOnly, SiteKind: harness.SiteDeclarationScalar},
			knownGap: "fillValidationOnly takes *ir.Model and is only reached via fillModelDetail; a " +
				"scalar component's internAlias path never calls it, so if/then/else on a scalar is " +
				"neither preserved nor diagnosed",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: string
      if: {type: string}
      then: {minLength: 1}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				sc, ok := doc.Types[namedID("S")].(*ir.Scalar)
				require.True(t, ok, "a bare scalar component owns a Scalar node")
				_, ok = sc.Extensions["openapi:if-then-else"]
				assert.False(t, ok, "if/then is never read for a scalar declaration today; closing this gap should turn this red")
			},
			assertDiags: func(t *testing.T, diags []ir.Diagnostic) {
				assert.False(t, hasDiagCode(diags, "openapi/validation-only-keyword"),
					"no keyword is read today, so no diagnostic fires; closing this gap should turn this red")
			},
		},
		{
			cell: harness.Cell{Annotation: harness.AnnotationValidationOnly, SiteKind: harness.SiteReference},
			knownGap: "$ref dispatch (schemaRef -> refTypeRef) returns before lower()/lowerModel() runs, so " +
				"fillValidationOnly never sees if/then/else written beside a property's $ref; there is no " +
				"property-level validation-only preservation to fall back to",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    Target: {type: string}
    S:
      type: object
      properties:
        f:
          $ref: '#/components/schemas/Target'
          if: {type: string}
          then: {minLength: 1}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				m, ok := doc.Types[namedID("S")].(*ir.Model)
				require.True(t, ok)
				p, ok := propByWire(m, "f")
				require.True(t, ok)
				_, ok = p.Extensions["openapi:if-then-else"]
				assert.False(t, ok, "if/then is never read today; closing this gap should turn this red")
			},
			assertDiags: func(t *testing.T, diags []ir.Diagnostic) {
				assert.False(t, hasDiagCode(diags, "openapi/validation-only-keyword"),
					"no keyword is read today, so no diagnostic fires; closing this gap should turn this red")
			},
		},
	}
}

// marshalToMap JSON-marshals v — a type node — and decodes the result back
// into a generic map keyed by wire field name. Five of this suite's twelve
// knownGap cases assert the dropped annotation's absence this way, because
// the annotation has no dedicated Go field to read off directly; the other
// seven assert a specific field directly instead.
//
// NotContains(node, key) only checks that no field JSON-encodes under that
// exact key — it cannot rule out the annotation being preserved under some
// other key instead, such as Extensions. That is a deliberate scope limit,
// not an oversight: Extensions-based preservation is ir-design §4.7's
// carve-out for validation-only keywords with no structural home, and every
// field-less gap this helper checks (constraints, default, visibility) names
// a field missing from the IR that Property or Scalar already has for the
// same annotation, so the fix each is waiting on is that field — and this
// assertion is written to catch exactly that, not to anticipate every other
// way the gap could theoretically be routed around.
func marshalToMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	node := make(map[string]any)
	require.NoError(t, json.Unmarshal(raw, &node))
	return node
}

// hasDiagCode reports whether diags contains a diagnostic with the given
// stable code.
func hasDiagCode(diags []ir.Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

// excusedCells lists cells OpenAPI cannot express, with the reason. An
// excuse is a claim about the format, not about the compiler — a cell that the
// format *can* express but the compiler drops belongs in retentionCases as a
// failing case, not here.
func excusedCells() []harness.Cell {
	return nil
}

func TestAnnotationRetention_EveryCellCovered(t *testing.T) {
	t.Parallel()
	cases := retentionCases()
	excused := excusedCells()
	covered := make([]harness.Cell, 0, len(cases)+len(excused))
	for _, tc := range cases {
		covered = append(covered, tc.cell)
	}
	covered = append(covered, excused...)

	missing := harness.MissingCells(covered)
	assert.Empty(t, missing, "cells with neither a case nor an excuse: %+v", missing)
}

// TestAnnotationRetention_NoDuplicateCells guards the case table against two
// cases claiming the same cell: TestAnnotationRetention_EveryCellCovered would
// still pass with one case silently shadowing the other, since it only checks
// which cells are covered, never how many times.
func TestAnnotationRetention_NoDuplicateCells(t *testing.T) {
	t.Parallel()
	seen := make(map[harness.Cell]bool)
	for _, tc := range retentionCases() {
		require.False(t, seen[tc.cell], "duplicate case for cell %+v", tc.cell)
		seen[tc.cell] = true
	}
}
