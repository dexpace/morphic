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
	cell   harness.Cell
	spec   string
	assert func(*testing.T, *ir.Document)
}

// compileGrid compiles one in-memory grid spec and fails on any error
// diagnostic, since a grid spec is always well-formed by construction.
func compileGrid(t *testing.T, name, spec string) *ir.Document {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: name + ".yaml", Data: []byte(spec)}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assertNoErrorDiags(t, diags)
	return doc
}

func TestGrid(t *testing.T) {
	t.Parallel()
	for _, tc := range gridCases() {
		name := string(tc.cell.Aspect) + "/" + string(tc.cell.SiteKind)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tc.assert(t, compileGrid(t, name, tc.spec))
		})
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
	}
}
