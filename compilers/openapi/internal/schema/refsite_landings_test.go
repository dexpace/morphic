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
// them, and returns the two documents.
func lowerBoth(t *testing.T, src string) (plain, promised *ir.Document) {
	t.Helper()
	for _, promise := range []bool{false, true} {
		lowerer, _ := schema.LoweredFor(t, src)
		lowerer.Diags.AppendAll(schema.LowerComponentSchemas(t.Context(), lowerer.Ctx, lowerer.Types, lowerer.Schemas()))
		if promise {
			schema.ReserveNodesAtReferenceTargets(lowerer.Ctx, lowerer.Types)
			promised = lowerer.Document(t)
			continue
		}
		plain = lowerer.Document(t)
	}
	return plain, promised
}

// TestReserveNodesAtReferenceTargets_BuildsNothingTwice pins that a document
// whose every $ref a lowering has already resolved gains no node and loses none
// to promising its landings: a position lowered once is the one node, not a
// second.
func TestReserveNodesAtReferenceTargets_BuildsNothingTwice(t *testing.T) {
	t.Parallel()
	plain, promised := lowerBoth(t, openapitest.ComponentSpecVer("3.1.0", `    Owner: {type: object, properties: {name: {type: string}}}
    Alias: {$ref: '#/components/schemas/Owner', description: an owner}
    Pet:
      type: object
      $defs: {Tag: {type: string}}
      properties:
        owner: {$ref: '#/components/schemas/Alias'}
        tag: {$ref: '#/components/schemas/Pet/$defs/Tag'}
        pets: {type: array, items: {$ref: '#/components/schemas/Pet'}}
`))

	assert.Empty(t, cmp.Diff(plain.Types, promised.Types), "the registry is the one the lowerings built")
	assert.NotEmpty(t, plain.Types)
}

// TestReserveNodesAtReferenceTargets_LowersWhatNothingElseReached pins the
// positions only a kept keyword's $ref names, and a kept discriminator's
// mapping value: the lowerings alone leave them out, and promising their
// landings adds each as the one node, declared at its own pointer.
func TestReserveNodesAtReferenceTargets_LowersWhatNothingElseReached(t *testing.T) {
	t.Parallel()
	plain, promised := lowerBoth(t, openapitest.ComponentSpecVer("3.1.0", `    Kept:
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

	for _, id := range []ir.TypeID{
		"type/anonymous/components/schemas/Kept/$defs/Sub",
		"type/anonymous/components/schemas/Through/$defs/Mid",
		"type/anonymous/components/schemas/Through/$defs/End",
	} {
		assert.NotContains(t, plain.Types, id, "lowerings alone do not reach %s", id)
		require.Contains(t, promised.Types, id, "promising reaches %s", id)
		assert.Equal(t, string(id[len("type/anonymous"):]), string(promised.Types[id].Common().Provenance.Pointer))
	}
	assert.Equal(t, "mid", promised.Types["type/anonymous/components/schemas/Through/$defs/Mid"].Common().Docs.Description)
}
