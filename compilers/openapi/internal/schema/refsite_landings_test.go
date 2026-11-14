// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
)

// lowerBoth lowers the component schemas of src twice, once as the compiler
// does and once with every position a schema $ref lands on promised after
// them, and returns the two documents with what promising reported.
func lowerBoth(t *testing.T, src string) (plain, promised *ir.Document, found []ir.Diagnostic) {
	t.Helper()
	for _, promise := range []bool{false, true} {
		l, _ := schema.LoweredFor(t, src)
		l.Diags.AppendAll(schema.LowerComponentSchemas(t.Context(), l.Ctx, l.Types))
		if promise {
			found = schema.PromiseLandings(l.Ctx, l.Types)
			promised = l.Document(t)
			continue
		}
		plain = l.Document(t)
	}
	return plain, promised, found
}

// TestPromiseLandings_BuildsNothingTwice pins that a document whose every $ref
// a lowering has already resolved gains no node and loses none to promising its
// landings, and that it reports nothing: a position lowered once is the one
// node, not a second.
func TestPromiseLandings_BuildsNothingTwice(t *testing.T) {
	t.Parallel()
	plain, promised, found := lowerBoth(t, openapitest.ComponentSpecVer("3.1.0", `    Owner: {type: object, properties: {name: {type: string}}}
    Alias: {$ref: '#/components/schemas/Owner', description: an owner}
    Pet:
      type: object
      $defs: {Tag: {type: string}}
      properties:
        owner: {$ref: '#/components/schemas/Alias'}
        tag: {$ref: '#/components/schemas/Pet/$defs/Tag'}
        pets: {type: array, items: {$ref: '#/components/schemas/Pet'}}
`))

	assert.Empty(t, found)
	assert.Empty(t, cmp.Diff(plain.Types, promised.Types), "the registry is the one the lowerings built")
	assert.NotEmpty(t, plain.Types)
}

// TestPromiseLandings_LowersWhatNothingElseReached pins the positions only a
// kept keyword's $ref names, and a kept discriminator's mapping value: the
// lowerings alone leave them out, and promising their landings adds each as the
// one node, declared at its own pointer.
func TestPromiseLandings_LowersWhatNothingElseReached(t *testing.T) {
	t.Parallel()
	plain, promised, found := lowerBoth(t, openapitest.ComponentSpecVer("3.1.0", `    Kept:
      type: object
      $defs: {Sub: {type: object, properties: {kept: {type: string}}}}
      not: {$ref: '#/components/schemas/Kept/$defs/Sub'}
    Through:
      type: object
      $defs:
        Mid: {$ref: '#/components/schemas/Through/$defs/End', description: mid}
        End: {type: integer}
      if: {$ref: '#/components/schemas/Through/$defs/Mid'}
`))

	assert.Empty(t, found)
	for _, id := range []ir.TypeID{
		"t/anon/components/schemas/Kept/$defs/Sub",
		"t/anon/components/schemas/Through/$defs/Mid",
		"t/anon/components/schemas/Through/$defs/End",
	} {
		assert.NotContains(t, plain.Types, id, "lowerings alone do not reach %s", id)
		require.Contains(t, promised.Types, id, "promising reaches %s", id)
		assert.Equal(t, string(id[len("t/anon"):]), string(promised.Types[id].Common().Provenance.Pointer))
	}
	assert.Equal(t, "mid", promised.Types["t/anon/components/schemas/Through/$defs/Mid"].Common().Docs.Description)
}
