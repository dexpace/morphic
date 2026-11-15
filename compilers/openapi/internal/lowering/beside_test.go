// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering/loweringtest"
)

// besideSpec has a parameter use writing keys beside its $ref, a use that
// writes none, an inline parameter and an alias of the first target.
const besideSpec = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters:
        - {$ref: '#/components/parameters/P', x-note: kept, required: true, summary: use-summary}
        - {$ref: '#/components/parameters/P'}
        - {name: inline, in: query, schema: {type: string}}
        - {$ref: '#/components/parameters/Alias', x-use: 0}
      responses: {"200": {description: ok}}
components:
  parameters:
    P: {name: p, in: query, schema: {type: string}}
    Alias: {$ref: '#/components/parameters/P', x-alias: 1}
`

// besideParams returns the context of besideSpec and the parameters of GET /a.
func besideParams(t *testing.T) (lowering.Ctx, []*soa.ReferencedParameter) {
	t.Helper()
	c := loweringtest.Context(t, besideSpec, lowering.Policy{})
	a, ok := c.Doc.Paths.Get("/a")
	require.True(t, ok)
	return c, a.GetObject().Get().GetParameters()
}

// TestBeside_KeysTheUseAndEachAlias holds what a chain writes beside its $ref
// to openapi:reference/<i>/<key>: i is 0 for the use and counts the aliases
// nearest first, a key is kept whole under its own provenance, and summary and
// description are left out where the use folds them into docs and kept where
// it does not.
func TestBeside_KeysTheUseAndEachAlias(t *testing.T) {
	t.Parallel()
	c, params := besideParams(t)

	got, diags := lowering.Beside[soa.Parameter](c, params[0], "/paths/~1a/get/parameters/0", true)
	assert.Equal(t, []string{"openapi:reference/0/required", "openapi:reference/0/x-note"}, sortedKeys(got))
	assert.Equal(t, `"kept"`, string(got["openapi:reference/0/x-note"].Value))
	assert.Equal(t, "/paths/~1a/get/parameters/0/x-note", string(got["openapi:reference/0/x-note"].Provenance.Pointer))
	assert.Empty(t, diags, "the table reports a key no object defines; the census keeps it and says nothing")

	got, diags = lowering.Beside[soa.Parameter](c, params[0], "/paths/~1a/get/parameters/0", false)
	assert.Contains(t, got, "openapi:reference/0/summary", "a use with no docs to hold it keeps its summary")
	require.Len(t, diags, 1)
	assert.Equal(t, "openapi/unhomed-construct", diags[0].Code)
	assert.Equal(t, "/paths/~1a/get/parameters/0/summary", string(diags[0].Provenance.Pointer))

	got, _ = lowering.Beside[soa.Parameter](c, params[3], "/paths/~1a/get/parameters/3", true)
	assert.Equal(t, []string{"openapi:reference/0/x-use", "openapi:reference/1/x-alias"}, sortedKeys(got),
		"the use and the alias each write their own entry")
	assert.Equal(t, "/components/parameters/Alias/x-alias",
		string(got["openapi:reference/1/x-alias"].Provenance.Pointer))
}

// TestBeside_ABareReferenceCostsNothing holds a $ref that writes nothing beside
// it, and an inline entry, to no map, no finding and no allocation.
func TestBeside_ABareReferenceCostsNothing(t *testing.T) {
	c, params := besideParams(t)
	for name, i := range map[string]int{"a bare $ref": 1, "an inline entry": 2} {
		got, diags := lowering.Beside[soa.Parameter](c, params[i], "/paths/~1a/get/parameters/x", true)
		assert.Nil(t, got, name)
		assert.Empty(t, diags, name)
		assert.Zero(t, testing.AllocsPerRun(10, func() {
			_, _ = lowering.Beside[soa.Parameter](c, params[i], "/paths/~1a/get/parameters/x", true)
		}), name)
	}
}
