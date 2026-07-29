// This file is the OpenAPI half of annotation retention: one case per
// annotation per kind of position, plus an assertion that no combination is
// silently uncovered. It complements conformance_test.go, which covers
// capability rows rather than annotation positions.
package openapi_test

import (
	"encoding/json"
	"fmt"
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

// minGapReasonLen is the shortest a knownGap reason may be, and
// minGapReasonWords the fewest space-separated words it must contain.
// Neither check reads the reason for sense — that is left to review — but
// together they are cheap insurance against a placeholder like "todo", or a
// run of repeated characters long enough to clear minGapReasonLen alone,
// standing in for the required explanation of why the compiler drops an
// annotation.
const (
	minGapReasonLen   = 20
	minGapReasonWords = 4
)

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
			require.NotNil(t, tc.assert, "%s: every case must assert something, gap or not", name)
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
// or carrying a reason too short or too terse to explain anything, would let
// a gap go unenforced or unexplained.
func TestAnnotationRetention_KnownGaps(t *testing.T) {
	t.Parallel()
	for _, tc := range retentionCases() {
		assert.NotNil(t, tc.assert, "%s/%s: every case must assert something, gap or not",
			tc.cell.Annotation, tc.cell.SiteKind)
		if tc.knownGap == "" {
			continue
		}
		t.Logf("GAP %s/%s: %s", tc.cell.Annotation, tc.cell.SiteKind, tc.knownGap)
		assertGapReasonExplains(t, tc.cell, tc.knownGap)
	}
}

// assertGapReasonExplains requires reason to be long enough, and to contain
// enough distinct words, to plausibly be the sentence retentionCase.knownGap's
// doc comment requires, rather than minGapReasonLen junk characters with no
// spaces.
func assertGapReasonExplains(t *testing.T, cell harness.Cell, reason string) {
	t.Helper()
	trimmed := strings.TrimSpace(reason)
	assert.GreaterOrEqual(t, len(trimmed), minGapReasonLen,
		"%s/%s: knownGap reason is too short to be a real explanation: %q",
		cell.Annotation, cell.SiteKind, reason)
	assert.GreaterOrEqual(t, len(strings.Fields(trimmed)), minGapReasonWords,
		"%s/%s: knownGap reason does not read as a sentence: %q",
		cell.Annotation, cell.SiteKind, reason)
}

func retentionCases() []retentionCase {
	return slices.Concat(
		examplesCases(),
		constraintsCases(),
		docsCases(),
		deprecatedCases(),
		defaultCases(),
		visibilityCases(),
		vendorExtensionCases(),
		xmlHintsCases(),
		validationOnlyCases(),
	)
}

// examplesCases returns the annotation-retention cases for the examples
// annotation: an example value at each SiteKind.
func examplesCases() []retentionCase {
	return []retentionCase{examplesModelCase(), examplesScalarCase(), examplesReferenceCase()}
}

func examplesModelCase() retentionCase {
	return retentionCase{
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
			ex := td.Common().Examples[0]
			require.NotNil(t, ex.Value)
			require.Equal(t, ir.ValueObject, ex.Value.Kind)
			require.Len(t, ex.Value.Object, 1)
			assert.Equal(t, "f", ex.Value.Object[0].Name)
			assert.Equal(t, "at-declaration", ex.Value.Object[0].Value.Str)
		},
	}
}

func examplesScalarCase() retentionCase {
	return retentionCase{
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

			assert.Empty(t, primitiveNode(t, doc, ir.TypeID("t/prim/string")).Common().Examples,
				"an example on the declaration must not leak onto the shared primitive")
		},
	}
}

func examplesReferenceCase() retentionCase {
	return retentionCase{
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
	}
}

// constraintsCases returns the annotation-retention cases for the constraints
// annotation: a value bound (e.g. minimum) at each SiteKind.
func constraintsCases() []retentionCase {
	return []retentionCase{constraintsModelCase(), constraintsScalarCase(), constraintsReferenceCase()}
}

func constraintsModelCase() retentionCase {
	return retentionCase{
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
	}
}

func constraintsScalarCase() retentionCase {
	return retentionCase{
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

			node := marshalToMap(t, primitiveNode(t, doc, ir.TypeID("t/prim/integer")))
			assert.NotContains(t, node, "constraints",
				"a minimum on the declaration must not leak onto the shared primitive")
		},
	}
}

func constraintsReferenceCase() retentionCase {
	return retentionCase{
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
	}
}

// docsCases returns the annotation-retention cases for the docs annotation: a
// description at each SiteKind.
func docsCases() []retentionCase {
	return []retentionCase{docsModelCase(), docsScalarCase(), docsReferenceCase()}
}

func docsModelCase() retentionCase {
	return retentionCase{
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
	}
}

func docsScalarCase() retentionCase {
	return retentionCase{
		cell: harness.Cell{Annotation: harness.AnnotationDocs, SiteKind: harness.SiteDeclarationScalar},
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
			assert.Equal(t, "at-declaration", sc.Docs.Description)

			assert.Empty(t, primitiveNode(t, doc, ir.TypeID("t/prim/string")).Common().Docs.Description,
				"a description on the declaration must not leak onto the shared primitive")
		},
	}
}

func docsReferenceCase() retentionCase {
	return retentionCase{
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
	}
}

// deprecatedCases returns the annotation-retention cases for the deprecated
// annotation at each SiteKind.
func deprecatedCases() []retentionCase {
	return []retentionCase{deprecatedModelCase(), deprecatedScalarCase(), deprecatedReferenceCase()}
}

func deprecatedModelCase() retentionCase {
	return retentionCase{
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
	}
}

func deprecatedScalarCase() retentionCase {
	return retentionCase{
		cell: harness.Cell{Annotation: harness.AnnotationDeprecated, SiteKind: harness.SiteDeclarationScalar},
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
			assert.NotNil(t, sc.Deprecation)

			assert.Nil(t, primitiveNode(t, doc, ir.TypeID("t/prim/string")).Common().Deprecation,
				"a deprecated on the declaration must not leak onto the shared primitive")
		},
	}
}

func deprecatedReferenceCase() retentionCase {
	return retentionCase{
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
	}
}

// defaultCases returns the annotation-retention cases for the default
// annotation at each SiteKind.
func defaultCases() []retentionCase {
	return []retentionCase{defaultModelCase(), defaultScalarCase(), defaultReferenceCase()}
}

func defaultModelCase() retentionCase {
	return retentionCase{
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
				"default has no field to land in on a declaration site today (it is surfaced "+
					"for a property); closing this gap should turn this red")
		},
	}
}

func defaultScalarCase() retentionCase {
	return retentionCase{
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
			sc, ok := doc.Types[namedID("S")].(*ir.Scalar)
			require.True(t, ok, "a bare scalar component owns a Scalar node")
			node := marshalToMap(t, sc)
			assert.NotContains(t, node, "default",
				"default has no field to land in on a declaration site today (it is surfaced "+
					"for a property); closing this gap should turn this red")

			primNode := marshalToMap(t, primitiveNode(t, doc, ir.TypeID("t/prim/integer")))
			assert.NotContains(t, primNode, "default",
				"a default on the declaration must not leak onto the shared primitive")
		},
	}
}

func defaultReferenceCase() retentionCase {
	return retentionCase{
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

			target, ok := doc.Types[namedID("Target")].(*ir.Scalar)
			require.True(t, ok)
			assert.NotContains(t, marshalToMap(t, target), "default",
				"a reference-site default must not attach to the referent")
		},
	}
}

// visibilityCases returns the annotation-retention cases for the visibility
// annotation (readOnly) at each SiteKind.
func visibilityCases() []retentionCase {
	return []retentionCase{visibilityModelCase(), visibilityScalarCase(), visibilityReferenceCase()}
}

func visibilityModelCase() retentionCase {
	return retentionCase{
		cell: harness.Cell{Annotation: harness.AnnotationVisibility, SiteKind: harness.SiteDeclarationModel},
		knownGap: "ir.TypeCommon has no Visibility field; Access and Usage exist but the compiler " +
			"never sets them",
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
	}
}

func visibilityScalarCase() retentionCase {
	return retentionCase{
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

			primNode := marshalToMap(t, primitiveNode(t, doc, ir.TypeID("t/prim/string")))
			assert.NotContains(t, primNode, "visibility",
				"a readOnly on the declaration must not leak onto the shared primitive")
		},
	}
}

func visibilityReferenceCase() retentionCase {
	return retentionCase{
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

			target, ok := doc.Types[namedID("Target")].(*ir.Scalar)
			require.True(t, ok)
			assert.NotContains(t, marshalToMap(t, target), "visibility",
				"a reference-site readOnly must not attach to the referent")
		},
	}
}

// vendorExtensionCases returns the annotation-retention cases for the
// vendor-extension annotation (x-vendor) at each SiteKind.
func vendorExtensionCases() []retentionCase {
	return []retentionCase{vendorExtensionModelCase(), vendorExtensionScalarCase(), vendorExtensionReferenceCase()}
}

func vendorExtensionModelCase() retentionCase {
	return retentionCase{
		cell: harness.Cell{Annotation: harness.AnnotationVendorExtension, SiteKind: harness.SiteDeclarationModel},
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
			raw, ok := td.Common().Preserved["openapi:x-vendor"]
			require.True(t, ok, "x-vendor must be preserved under the openapi: namespace")
			assert.JSONEq(t, `"at-declaration"`, string(raw.Value))
			assert.Equal(t, ir.ReasonVendorExtension, raw.Reason)
		},
	}
}

func vendorExtensionScalarCase() retentionCase {
	return retentionCase{
		cell: harness.Cell{Annotation: harness.AnnotationVendorExtension, SiteKind: harness.SiteDeclarationScalar},
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
			raw, ok := sc.Preserved["openapi:x-vendor"]
			require.True(t, ok, "x-vendor must be preserved under the openapi: namespace")
			assert.JSONEq(t, `"at-declaration"`, string(raw.Value))
			assert.Equal(t, ir.ReasonVendorExtension, raw.Reason)

			assert.Empty(t, primitiveNode(t, doc, ir.TypeID("t/prim/string")).Common().Preserved,
				"an x-vendor on the declaration must not leak onto the shared primitive")
		},
	}
}

func vendorExtensionReferenceCase() retentionCase {
	return retentionCase{
		cell: harness.Cell{Annotation: harness.AnnotationVendorExtension, SiteKind: harness.SiteReference},
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
			raw, ok := p.Preserved["openapi:x-vendor"]
			require.True(t, ok, "x-vendor beside a $ref binds the reference site")
			assert.JSONEq(t, `"at-reference"`, string(raw.Value))
			assert.Equal(t, ir.ReasonVendorExtension, raw.Reason)

			target, ok := doc.Types[namedID("Target")]
			require.True(t, ok)
			assert.Empty(t, target.Common().Preserved,
				"a reference-site extension must not attach to the referent")
		},
	}
}

// xmlHintsCases returns the annotation-retention cases for the xmlHints
// annotation at each SiteKind.
func xmlHintsCases() []retentionCase {
	return []retentionCase{xmlHintsModelCase(), xmlHintsScalarCase(), xmlHintsReferenceCase()}
}

func xmlHintsModelCase() retentionCase {
	return retentionCase{
		cell: harness.Cell{Annotation: harness.AnnotationXMLHints, SiteKind: harness.SiteDeclarationModel},
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
			require.NotNil(t, td.Common().XML)
			assert.Equal(t, "Renamed", td.Common().XML.Name)
		},
	}
}

func xmlHintsScalarCase() retentionCase {
	return retentionCase{
		cell: harness.Cell{Annotation: harness.AnnotationXMLHints, SiteKind: harness.SiteDeclarationScalar},
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
			require.NotNil(t, sc.XML)
			assert.Equal(t, "Renamed", sc.XML.Name)

			assert.Nil(t, primitiveNode(t, doc, ir.TypeID("t/prim/string")).Common().XML,
				"an xml hint on the declaration must not leak onto the shared primitive")
		},
	}
}

func xmlHintsReferenceCase() retentionCase {
	return retentionCase{
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
	}
}

// validationOnlyCases returns the annotation-retention cases for the
// validationOnly annotation (if/then/else) at each SiteKind.
//
// The reference case below writes the keywords beside a *property's* $ref,
// where the property carries them. The grid has one `reference` cell per
// annotation, not one per position, so this cell going green still says
// nothing about the request-body, parameter and response-content positions.
// They keep the keywords too; TestValidationOnly_BesideARefAtABodyPosition is
// what says so.
func validationOnlyCases() []retentionCase {
	return []retentionCase{
		validationOnlyModelCase(), validationOnlyScalarCase(), validationOnlyReferenceCase(),
	}
}

func validationOnlyModelCase() retentionCase {
	return retentionCase{
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
			raw, ok := m.Preserved["openapi:if-then-else"]
			require.True(t, ok, "if/then must be kept verbatim under Preserved")
			assert.JSONEq(t, `{"if":{"type":"string"},"then":{"minLength":1}}`, string(raw.Value))
			assert.Equal(t, ir.ReasonValidationOnly, raw.Reason)
		},
		assertDiags: func(t *testing.T, diags []ir.Diagnostic) {
			assert.True(t, hasDiagCode(diags, "openapi/validation-only-keyword"),
				"expected a validation-only-keyword info diagnostic")
		},
	}
}

func validationOnlyScalarCase() retentionCase {
	return retentionCase{
		cell: harness.Cell{Annotation: harness.AnnotationValidationOnly, SiteKind: harness.SiteDeclarationScalar},
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
			raw, ok := sc.Preserved["openapi:if-then-else"]
			require.True(t, ok, "if/then must be kept verbatim under Preserved")
			assert.JSONEq(t, `{"if":{"type":"string"},"then":{"minLength":1}}`, string(raw.Value))
			assert.Equal(t, ir.ReasonValidationOnly, raw.Reason)

			_, leaked := primitiveNode(t, doc, ir.TypeID("t/prim/string")).Common().Preserved["openapi:if-then-else"]
			assert.False(t, leaked, "if/then on the declaration must not leak onto the shared primitive")
		},
		assertDiags: func(t *testing.T, diags []ir.Diagnostic) {
			assert.True(t, hasDiagCode(diags, "openapi/validation-only-keyword"),
				"expected a validation-only-keyword info diagnostic")
		},
	}
}

func validationOnlyReferenceCase() retentionCase {
	return retentionCase{
		cell: harness.Cell{Annotation: harness.AnnotationValidationOnly, SiteKind: harness.SiteReference},
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
			raw, ok := p.Preserved["openapi:if-then-else"]
			require.True(t, ok, "if/then beside a $ref binds the reference site")
			assert.JSONEq(t, `{"if":{"type":"string"},"then":{"minLength":1}}`, string(raw.Value))
			assert.Equal(t, ir.ReasonValidationOnly, raw.Reason)

			target, ok := doc.Types[namedID("Target")]
			require.True(t, ok)
			assert.Empty(t, target.Common().Preserved,
				"a reference-site keyword must not attach to the referent")
		},
		assertDiags: func(t *testing.T, diags []ir.Diagnostic) {
			assert.True(t, hasDiagCode(diags, "openapi/validation-only-keyword"),
				"expected a validation-only-keyword info diagnostic")
		},
	}
}

// validationOnlyRefSitesSpec writes if/then beside a $ref at each reference
// position the grid's single `reference` cell leaves out, with a `then` bound
// per position so a keyword landing at the wrong one is visible rather than
// merely present.
const validationOnlyRefSitesSpec = `openapi: 3.1.0
info: {title: g, version: "1"}
paths:
  /x:
    post:
      operationId: g
      parameters:
        - name: q
          in: query
          schema:
            $ref: '#/components/schemas/Target'
            if: {type: string}
            then: {minLength: 1}
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Target'
              if: {type: string}
              then: {minLength: 2}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Target'
                if: {type: string}
                then: {minLength: 3}
components:
  schemas:
    Target: {type: string}
`

// TestValidationOnly_BesideARefAtABodyPosition covers the reference positions
// the retention grid does not reach. A validation-only keyword written beside a
// $ref binds the reference site rather than the referent, so it can never go on
// the shared target — and each of these three positions keeps it, by whichever
// route it has: a parameter carries it on the ir.Parameter itself, while a
// request body and a response content have no carrier and so hoist an alias of
// the referent to hold it (GitHub #116).
func TestValidationOnly_BesideARefAtABodyPosition(t *testing.T) {
	t.Parallel()
	doc, diags := compileAnnotationSpec(t, "validation-only-ref-sites", validationOnlyRefSitesSpec)
	assertNoErrorDiags(t, diags)
	op, ok := opByName(doc, "g")
	require.True(t, ok)

	for _, tc := range []struct {
		name      string
		minLength int
		preserved ir.Preserved
	}{
		{"parameter", 1, paramPreserved(t, op)},
		{"request body", 2, refSiteAlias(t, doc, requestContent(t, op)).Common().Preserved},
		{"response content", 3, refSiteAlias(t, doc, firstContent(t, op)).Common().Preserved},
	} {
		raw, found := tc.preserved["openapi:if-then-else"]
		require.True(t, found, "%s: if/then beside its $ref must be kept", tc.name)
		assert.JSONEq(t,
			fmt.Sprintf(`{"if":{"type":"string"},"then":{"minLength":%d}}`, tc.minLength),
			string(raw.Value), "%s keeps the keywords written at its own site", tc.name)
		assert.Equal(t, ir.ReasonValidationOnly, raw.Reason, tc.name)
	}

	target, found := doc.Types[namedID("Target")]
	require.True(t, found)
	assert.Empty(t, target.Common().Preserved,
		"three reference sites, and none of them wrote onto the referent")
}

// paramPreserved returns the operation's single parameter's preserved entries.
func paramPreserved(t *testing.T, op ir.Operation) ir.Preserved {
	t.Helper()
	require.Len(t, op.Params, 1)
	return op.Params[0].Preserved
}

// requestContent returns an operation's single request-body content.
func requestContent(t *testing.T, op ir.Operation) ir.Content {
	t.Helper()
	require.NotNil(t, op.Request)
	require.Len(t, op.Request.Contents, 1)
	return op.Request.Contents[0]
}

// refSiteAlias returns the node a body position hoisted to hold what was
// written beside its $ref, requiring it to be an alias of the referent rather
// than the referent itself — which is the difference between keeping the
// keywords at the reference site and writing them onto a shared target.
func refSiteAlias(t *testing.T, doc *ir.Document, c ir.Content) ir.TypeDef {
	t.Helper()
	td, ok := doc.Types[c.Type.Target]
	require.True(t, ok, "the content's type is registered")
	sc, ok := td.(*ir.Scalar)
	require.True(t, ok, "a $ref with siblings hoists an alias; got %T", td)
	require.NotNil(t, sc.Base, "and that alias points at what it referenced")
	assert.Equal(t, namedID("Target"), sc.Base.Target)
	return td
}

// marshalToMap JSON-marshals v — a type node — and decodes the result back
// into a generic map keyed by wire field name. It backs two kinds of check
// among the retention cases above: proving an annotation has no field to
// land in on the declaration itself (constraints, default, and visibility
// all read this way), and, in several of those same cases — gap or not —
// additionally proving that same annotation has not leaked onto an
// unrelated, field-less node the case also holds a handle to: the shared
// primitive a scalar declaration aliases, or the target a reference case's
// $ref points at.
//
// Either way, NotContains(node, key) only rules out that exact JSON key —
// not preservation under a different one, such as Preserved (ir-design
// §4.7's carve-out for validation-only keywords with no structural home).
// A knownGap case is waiting on a real field; this assertion exists to
// catch exactly that gap, not every other way it could be routed around.
func marshalToMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	node := make(map[string]any)
	require.NoError(t, json.Unmarshal(raw, &node))
	return node
}

// primitiveNode returns the shared primitive node already registered at id.
// A declaration-scalar case's Scalar aliases a shared primitive through its
// own Base field; asserting on the alias says nothing about whether the
// annotation also leaked onto the shared node every other use of that
// primitive in the document sees too, so every such case checks both.
func primitiveNode(t *testing.T, doc *ir.Document, id ir.TypeID) ir.TypeDef {
	t.Helper()
	td, ok := doc.Types[id]
	require.True(t, ok, "shared primitive %s must still be registered", id)
	return td
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

// declShape is one declaration shape the SiteKind axis does not name
// individually. body holds the keywords that give component S that shape, and
// value a literal legal for it, reused for both `example` and `default`.
type declShape struct {
	name  string
	body  string
	value string
	// kind is the node this shape must lower to. Asserting it is what keeps the
	// grid honest: the four shapes are here because they take four different
	// lower() destinations, and a body that quietly stopped doing so would make
	// every assertion below a duplicate of another row rather than a new one.
	kind ir.TypeKind
	// constraint is a bound legal on this shape, and constraintKept whether the
	// node it lowers to has a field to hold it. This is the one annotation of
	// the nine whose answer is shape-dependent: ir.List carries Constraints,
	// while Literal, Enum and Union do not, so a single blanket expectation
	// would be wrong either way.
	constraint     string
	constraintKept bool
}

// spec renders a component declaration of this shape carrying every annotation
// the retention grid names, so one compile answers all nine at once.
func (s declShape) spec() string {
	return `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S:
      description: at-declaration
      deprecated: true
      xml: {name: Renamed}
      x-vendor: at-declaration
      if: {type: string}
      then: {minLength: 1}
      readOnly: true
      example: ` + s.value + `
      default: ` + s.value + "\n" + s.constraint + s.body + "\n"
}

func declShapes() []declShape {
	return []declShape{
		{name: "const", body: "      const: c", value: "c", kind: ir.KindLiteral},
		{name: "enum", body: "      enum: [a, b]", value: "a", kind: ir.KindEnum},
		{
			name: "array", body: "      type: array\n      items: {type: string}", value: "[x]",
			kind: ir.KindList, constraint: "      minItems: 1\n", constraintKept: true,
		},
		{
			name:  "oneOf",
			body:  "      oneOf:\n        - {type: string}\n        - {type: integer}",
			value: "x", kind: ir.KindUnion,
		},
	}
}

// TestAnnotationRetention_OtherDeclarationShapes runs the full nine-annotation
// grid against the declaration shapes the SiteKind axis does not name
// individually: const, enum, array, and a sibling-less oneOf. Each reaches a
// different lower() destination, and every one of them used to bypass the
// annotation reading that lived inside the model branch — so this is what says
// the reading moved above the dispatch rather than being copied onto a second
// path, and it says it for every annotation rather than for docs alone.
func TestAnnotationRetention_OtherDeclarationShapes(t *testing.T) {
	t.Parallel()
	for _, shape := range declShapes() {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileAnnotationSpec(t, shape.name, shape.spec())
			assertNoErrorDiags(t, diags)
			td, ok := doc.Types[namedID("S")]
			require.True(t, ok, "component S must own a node")
			require.Equal(t, shape.kind, td.Kind(), "this shape must reach its own lower() destination")
			assertDeclaredAnnotationsKept(t, td)
			assertDeclarationGapsHold(t, td, shape)
		})
	}
}

// assertDeclaredAnnotationsKept checks the six annotations
// attachDeclaredAnnotations reads — the ones whose retention depends on where
// in the dispatch the reading happens.
func assertDeclaredAnnotationsKept(t *testing.T, td ir.TypeDef) {
	t.Helper()
	c := td.Common()
	assert.Equal(t, "at-declaration", c.Docs.Description, "docs")
	assert.NotNil(t, c.Deprecation, "deprecated")
	if assert.NotNil(t, c.XML, "xmlHints") {
		assert.Equal(t, "Renamed", c.XML.Name)
	}
	vendor, ok := c.Preserved["openapi:x-vendor"]
	if assert.True(t, ok, "vendorExtension: got %v", c.Preserved) {
		assert.JSONEq(t, `"at-declaration"`, string(vendor.Value))
		assert.Equal(t, ir.ReasonVendorExtension, vendor.Reason)
	}
	ite, ok := c.Preserved["openapi:if-then-else"]
	if assert.True(t, ok, "validationOnly: got %v", c.Preserved) {
		assert.Equal(t, ir.ReasonValidationOnly, ite.Reason)
	}
	require.Len(t, c.Examples, 1, "examples")
	require.NotNil(t, c.Examples[0].Value)
	assert.NotEqual(t, ir.Value{}, *c.Examples[0].Value, "the example converted to a typed value")
}

// assertDeclarationGapsHold checks the three annotations with no home on a
// declaration, so this grid stays red-on-close the same way the model and
// scalar knownGap cells above do. Only constraints varies by shape.
func assertDeclarationGapsHold(t *testing.T, td ir.TypeDef, shape declShape) {
	t.Helper()
	node := marshalToMap(t, td)
	assert.NotContains(t, node, "default",
		"default has no field on a type node regardless of declaration shape")
	assert.NotContains(t, node, "visibility",
		"readOnly has no field on a type node regardless of declaration shape")
	if shape.constraintKept {
		assert.Contains(t, node, "constraints",
			"a collection bound has a home on the node this shape lowers to")
		return
	}
	assert.NotContains(t, node, "constraints",
		"this shape lowers to a node with no Constraints field")
}

// TestAnnotationRetention_EveryCellCovered requires every cell in the
// harness's annotation-by-site-kind grid to have a case here, gap or not. A
// cell can also be excused instead of cased: an excuse is a claim about what
// OpenAPI itself can express, not about what this compiler currently does
// with it (retentionCase.knownGap is for the latter) — e.g. a SiteKind this
// format has no syntax for at all. No cell currently needs one, so there is
// no excusedCells list here; add one back if a future cell does.
func TestAnnotationRetention_EveryCellCovered(t *testing.T) {
	t.Parallel()
	cases := retentionCases()
	covered := make([]harness.Cell, 0, len(cases))
	for _, tc := range cases {
		covered = append(covered, tc.cell)
	}

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
