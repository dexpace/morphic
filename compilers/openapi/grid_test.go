// This file is the OpenAPI half of the (format × aspect × site-kind)
// conformance grid: one case per annotation slot per kind of position, plus an
// assertion that no cell is silently uncovered. It complements
// conformance_test.go, which covers capability rows rather than annotation
// positions.
package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/internal/harness"
	"github.com/dexpace/morphic/ir"
)

// gridCase is one cell of the conformance grid: a minimal spec exercising one
// annotation slot at one kind of position, and the assertion that the
// annotation survived to the right place in the IR.
type gridCase struct {
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

// compileGrid compiles one in-memory grid spec, since a grid spec is always
// well-formed by construction; the caller checks diagnostics against what its
// own cell expects.
func compileGrid(t *testing.T, name, spec string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".yaml", Data: []byte(spec)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

func TestGrid(t *testing.T) {
	t.Parallel()
	for _, tc := range gridCases() {
		name := string(tc.cell.Aspect) + "/" + string(tc.cell.SiteKind)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileGrid(t, name, tc.spec)
			assertNoErrorDiags(t, diags)
			tc.assert(t, doc)
			if tc.assertDiags != nil {
				tc.assertDiags(t, diags)
			}
		})
	}
}

// TestGrid_KnownGapsAreListed logs every cell marked knownGap so the gaps stay
// visible in test output rather than buried inside a passing subtest's body.
func TestGrid_KnownGapsAreListed(t *testing.T) {
	t.Parallel()
	for _, tc := range gridCases() {
		if tc.knownGap != "" {
			t.Logf("GAP %s/%s: %s", tc.cell.Aspect, tc.cell.SiteKind, tc.knownGap)
		}
	}
}

func gridCases() []gridCase {
	return []gridCase{
		{
			cell: harness.Cell{Aspect: harness.AspectExamples, SiteKind: harness.SiteDeclaration},
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
			cell: harness.Cell{Aspect: harness.AspectExamples, SiteKind: harness.SiteReference},
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
		{
			cell: harness.Cell{Aspect: harness.AspectConstraints, SiteKind: harness.SiteDeclaration},
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
			cell: harness.Cell{Aspect: harness.AspectConstraints, SiteKind: harness.SiteReference},
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
			},
		},
		{
			cell: harness.Cell{Aspect: harness.AspectDocs, SiteKind: harness.SiteDeclaration},
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
			cell: harness.Cell{Aspect: harness.AspectDocs, SiteKind: harness.SiteReference},
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
		{
			cell: harness.Cell{Aspect: harness.AspectDeprecated, SiteKind: harness.SiteDeclaration},
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
			cell: harness.Cell{Aspect: harness.AspectDeprecated, SiteKind: harness.SiteReference},
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
		{
			cell:     harness.Cell{Aspect: harness.AspectDefault, SiteKind: harness.SiteDeclaration},
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
				_ = td
			},
		},
		{
			cell: harness.Cell{Aspect: harness.AspectDefault, SiteKind: harness.SiteReference},
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
		{
			cell:     harness.Cell{Aspect: harness.AspectVisibility, SiteKind: harness.SiteDeclaration},
			knownGap: "ir.TypeCommon has no Visibility field; Access and Usage exist but the compiler never sets them",
			spec: `openapi: 3.1.0
info: {title: g, version: "1"}
paths: {}
components:
  schemas:
    S: {type: string, readOnly: true}
`,
			assert: func(t *testing.T, doc *ir.Document) {
				td, ok := doc.Types[namedID("S")]
				require.True(t, ok)
				assert.Empty(t, td.Common().Access, "Access is never set today; closing this gap should turn this red")
			},
		},
		{
			cell: harness.Cell{Aspect: harness.AspectVisibility, SiteKind: harness.SiteReference},
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
		{
			cell: harness.Cell{Aspect: harness.AspectExtensions, SiteKind: harness.SiteDeclaration},
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
			cell: harness.Cell{Aspect: harness.AspectExtensions, SiteKind: harness.SiteReference},
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
		{
			cell:     harness.Cell{Aspect: harness.AspectXMLHints, SiteKind: harness.SiteDeclaration},
			knownGap: "ir.TypeCommon.XML exists but the OpenAPI compiler never assigns it; only Property.XML is filled (fillPropertyDetail), so a component-level xml hint has nowhere to land",
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
			cell: harness.Cell{Aspect: harness.AspectXMLHints, SiteKind: harness.SiteReference},
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
			},
		},
		{
			cell: harness.Cell{Aspect: harness.AspectValidationOnly, SiteKind: harness.SiteDeclaration},
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
			cell: harness.Cell{Aspect: harness.AspectValidationOnly, SiteKind: harness.SiteReference},
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

// excusedCells lists grid cells OpenAPI cannot express, with the reason. An
// excuse is a claim about the format, not about the compiler — a cell that the
// format *can* express but the compiler drops belongs in gridCases as a failing
// case, not here.
func excusedCells() []harness.Cell {
	return nil
}

func TestGrid_EveryCellCoveredOrExcused(t *testing.T) {
	t.Parallel()
	cases := gridCases()
	excused := excusedCells()
	covered := make([]harness.Cell, 0, len(cases)+len(excused))
	for _, tc := range cases {
		covered = append(covered, tc.cell)
	}
	covered = append(covered, excused...)

	missing := harness.MissingCells(covered)
	assert.Empty(t, missing, "grid cells with neither a case nor an excuse: %+v", missing)
}
