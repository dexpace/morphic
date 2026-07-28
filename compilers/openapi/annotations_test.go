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
		extensionsCases(),
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

// extensionsCases returns the annotation-retention cases for the extensions
// annotation (x-vendor) at each SiteKind.
func extensionsCases() []retentionCase {
	return []retentionCase{extensionsModelCase(), extensionsScalarCase(), extensionsReferenceCase()}
}

func extensionsModelCase() retentionCase {
	return retentionCase{
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
	}
}

func extensionsScalarCase() retentionCase {
	return retentionCase{
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

			assert.Empty(t, primitiveNode(t, doc, ir.TypeID("t/prim/string")).Common().Extensions,
				"an x-vendor on the declaration must not leak onto the shared primitive")
		},
	}
}

func extensionsReferenceCase() retentionCase {
	return retentionCase{
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
			assert.Nil(t, td.Common().XML,
				"XML is never set on a declaration site today; closing this gap should turn this red")
		},
	}
}

func xmlHintsScalarCase() retentionCase {
	return retentionCase{
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
			assert.Nil(t, sc.XML,
				"XML is never set on a declaration site today; closing this gap should turn this red")

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
			raw, ok := m.Extensions["openapi:if-then-else"]
			require.True(t, ok, "if/then must be preserved verbatim in extensions")
			assert.JSONEq(t, `{"if":{"type":"string"},"then":{"minLength":1}}`, string(raw))
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

			_, leaked := primitiveNode(t, doc, ir.TypeID("t/prim/string")).Common().Extensions["openapi:if-then-else"]
			assert.False(t, leaked, "if/then on the declaration must not leak onto the shared primitive")
		},
		assertDiags: func(t *testing.T, diags []ir.Diagnostic) {
			assert.False(t, hasDiagCode(diags, "openapi/validation-only-keyword"),
				"no keyword is read for a scalar declaration today, so no diagnostic fires; "+
					"closing this gap should turn this red")
		},
	}
}

func validationOnlyReferenceCase() retentionCase {
	return retentionCase{
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
			assert.False(t, ok,
				"if/then beside a $ref is never read today; closing this gap should turn this red")
		},
		assertDiags: func(t *testing.T, diags []ir.Diagnostic) {
			assert.False(t, hasDiagCode(diags, "openapi/validation-only-keyword"),
				"no keyword is read beside a $ref today, so no diagnostic fires; "+
					"closing this gap should turn this red")
		},
	}
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
// not preservation under a different one, such as Extensions (ir-design
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
