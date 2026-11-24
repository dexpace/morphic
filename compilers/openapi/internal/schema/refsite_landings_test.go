// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"fmt"
	"strings"
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

// TestReferenceLandings_ListsSchemaRefsAndKeptDiscriminators pins that
// ReferenceLandings returns resolved schema $refs and kept discriminator
// routes keyed by their writing LandingSite.
func TestReferenceLandings_ListsSchemaRefsAndKeptDiscriminators(t *testing.T) {
	t.Parallel()
	lowerer, _ := schema.LoweredFor(t, openapitest.ComponentSpecVer("3.2.0", `    Kept:
      type: object
      $defs:
        Sub: {type: object, properties: {kept: {type: string}}}
      not: {$ref: '#/components/schemas/Kept/$defs/Sub'}
    KeptDisc:
      type: string
      $defs:
        Mapped: {type: object, properties: {m: {type: string}}}
        Fallback: {type: object, properties: {f: {type: string}}}
      discriminator:
        propertyName: kind
        mapping:
          sub: '#/components/schemas/KeptDisc/$defs/Mapped'
        defaultMapping: '#/components/schemas/KeptDisc/$defs/Fallback'
    Bad:
      type: object
      not: {$ref: '#/components/schemas/Missing'}
    NoDiscRoutes:
      type: string
      discriminator:
        propertyName: kind
`))
	lowerer.Diags.AppendAll(schema.LowerComponentSchemas(t.Context(), lowerer.Ctx, lowerer.Types, lowerer.Schemas()))
	schema.ReserveNodesAtReferenceTargets(lowerer.Ctx, lowerer.Types)
	_ = lowerer.Document(t)

	landings := schema.ReferenceLandings(lowerer.Ctx)
	assert.Equal(t, []schema.Landing{{ID: "type/anonymous/components/schemas/Kept/$defs/Sub", Path: "/$ref", Ref: "#/components/schemas/Kept/$defs/Sub"}},
		landings[schema.LandingSite{Source: 0, Pointer: "/components/schemas/Kept/not"}])
	assert.Equal(t, []schema.Landing{
		{ID: "type/anonymous/components/schemas/KeptDisc/$defs/Mapped", Path: "/mapping/sub", Ref: "#/components/schemas/KeptDisc/$defs/Mapped", Discriminator: true},
		{ID: "type/anonymous/components/schemas/KeptDisc/$defs/Fallback", Path: "/defaultMapping", Ref: "#/components/schemas/KeptDisc/$defs/Fallback", Discriminator: true},
	}, landings[schema.LandingSite{Source: 0, Pointer: "/components/schemas/KeptDisc/discriminator"}])
}

// TestReserveNodesAtReferenceTargets_DrainsTransitiveRefsFromHoistedDiscriminator
// pins that schemas reached while fulfilling promised sub-schemas (such as a
// kept discriminator route inside a hoisted $defs entry) also reserve their
// transitive $ref landings.
func TestReserveNodesAtReferenceTargets_DrainsTransitiveRefsFromHoistedDiscriminator(t *testing.T) {
	t.Parallel()
	_, promised := lowerBoth(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      not: {$ref: '#/components/schemas/Holder/$defs/Mid'}
      $defs:
        Mid:
          type: string
          discriminator:
            propertyName: k
            mapping:
              m: '#/components/schemas/Holder/$defs/Mapped'
        Mapped:
          type: object
          not: {$ref: '#/components/schemas/Holder/$defs/Deep'}
        Deep:
          type: object
          properties: {z: {type: string}}
`))
	for _, want := range []ir.TypeID{
		"type/anonymous/components/schemas/Holder/$defs/Mid",
		"type/anonymous/components/schemas/Holder/$defs/Mapped",
		"type/anonymous/components/schemas/Holder/$defs/Deep",
	} {
		assert.Contains(t, promised.Types, want, "transitive target %s must be reserved", want)
	}
}

// TestReserveNodesAtReferenceTargets_SkipsUnreferencedDefs pins that
// ReserveNodesAtReferenceTargets does not reserve targets of $refs inside
// unreferenced $defs tables (including chains into raw extensions or nested
// $defs inside a reached definition), while still reserving chains reached from
// live keywords and properties literally named "$defs".
func TestReserveNodesAtReferenceTargets_SkipsUnreferencedDefs(t *testing.T) {
	t.Parallel()
	_, promised := lowerBoth(t, openapitest.ComponentSpecVer("3.1.0", `    Unused:
      type: object
      $defs:
        A: {$ref: '#/components/schemas/Unused/$defs/B'}
        B: {type: object, properties: {x: {type: string}}}
        ToRaw: {$ref: '#/x-defs/Dead1'}
    Reached:
      type: object
      properties:
        $defs:
          type: object
          not: {$ref: '#/components/schemas/Reached/$defs/FromProp'}
      not: {$ref: '#/components/schemas/Reached/$defs/Mid'}
      $defs:
        FromProp: {type: object, properties: {p: {type: string}}}
        Mid:
          type: object
          not: {$ref: '#/components/schemas/Reached/$defs/End'}
          $defs:
            InnerDead1: {$ref: '#/components/schemas/Reached/$defs/Mid/$defs/InnerDead2'}
            InnerDead2: {type: object, properties: {z: {type: string}}}
        End: {type: object, properties: {e: {type: string}}}
`)+`x-defs:
  Dead1: {$ref: '#/x-defs/Dead2'}
  Dead2: {type: object, properties: {d: {type: string}}}
`)

	for _, want := range []ir.TypeID{
		"type/anonymous/components/schemas/Reached/$defs/FromProp",
		"type/anonymous/components/schemas/Reached/$defs/Mid",
		"type/anonymous/components/schemas/Reached/$defs/End",
	} {
		assert.Contains(t, promised.Types, want, "live target %s must be reserved", want)
	}
	for _, gone := range []ir.TypeID{
		"type/anonymous/components/schemas/Unused/$defs/A",
		"type/anonymous/components/schemas/Unused/$defs/B",
		"type/anonymous/components/schemas/Unused/$defs/ToRaw",
		"type/anonymous/x-defs/Dead1",
		"type/anonymous/x-defs/Dead2",
		"type/anonymous/components/schemas/Reached/$defs/Mid/$defs/InnerDead1",
		"type/anonymous/components/schemas/Reached/$defs/Mid/$defs/InnerDead2",
	} {
		assert.NotContains(t, promised.Types, gone, "unreferenced $defs target %s must not be reserved", gone)
	}

	// Empty reference table fast-path.
	plain, emptyPromised := lowerBoth(t, openapitest.ComponentSpecVer("3.1.0", `    Leaf: {type: string}
`))
	assert.Empty(t, cmp.Diff(plain.Types, emptyPromised.Types))

	// Reverse-ordered $defs chain exercises linear worklist propagation
	// (every hop lands on an earlier pointer in sorted table order).
	const chainLen = 200
	var sb strings.Builder
	fmt.Fprintf(&sb, "    Chain:\n      type: object\n      not: {$ref: '#/components/schemas/Chain/$defs/D%04d'}\n      $defs:\n", chainLen-1)
	sb.WriteString("        D0000: {type: object, properties: {leaf: {type: string}}}\n")
	for i := 1; i < chainLen; i++ {
		fmt.Fprintf(&sb, "        D%04d: {type: object, not: {$ref: '#/components/schemas/Chain/$defs/D%04d'}}\n", i, i-1)
	}
	_, chainPromised := lowerBoth(t, openapitest.ComponentSpecVer("3.1.0", sb.String()))
	assert.Contains(t, chainPromised.Types, ir.TypeID("type/anonymous/components/schemas/Chain/$defs/D0000"))
	assert.Contains(t, chainPromised.Types, ir.TypeID(fmt.Sprintf("type/anonymous/components/schemas/Chain/$defs/D%04d", chainLen-1)))

	// RecordReached activates sites beneath an explicitly recorded container.
	lowerer, _ := schema.LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", `    Holder:
      type: object
      $defs:
        X: {type: object, properties: {a: {type: string}}}
`)+`  parameters:
    Extra:
      name: p
      in: query
      schema: {$ref: '#/components/schemas/Holder/$defs/X'}
`)
	lowerer.Diags.AppendAll(schema.LowerComponentSchemas(t.Context(), lowerer.Ctx, lowerer.Types, lowerer.Schemas()))
	schema.RecordReached(lowerer.Ctx, "/components/parameters/Extra")
	schema.ReserveNodesAtReferenceTargets(lowerer.Ctx, lowerer.Types)
	recordedDoc := lowerer.Document(t)
	assert.Contains(t, recordedDoc.Types, ir.TypeID("type/anonymous/components/schemas/Holder/$defs/X"))

	// RecordDiscarded and RecordDiscardedSite store dropped IDs and provenances.
	schema.RecordDiscarded(lowerer.Ctx, "type/anonymous/dropped")
	prov := ir.Provenance{Source: 0, Pointer: "/components/headers/H/schema/not"}
	schema.RecordDiscardedSite(lowerer.Ctx, prov)
	assert.True(t, schema.DiscardedTypes(lowerer.Ctx)["type/anonymous/dropped"])
	assert.Equal(t, []ir.Provenance{prov}, schema.DiscardedSites(lowerer.Ctx))
	assert.Empty(t, schema.PrunedDiagnostics(lowerer.Ctx))
}

// TestUnpreservableValidationOnlyBranch_RecordsDiscardedSite pins that an
// unpreservable validation-only combinator (kept via
// preserveValidationOnlyKeyword) records its keyword provenance as a discarded
// site for post-settlement pruning.
func TestUnpreservableValidationOnlyBranch_RecordsDiscardedSite(t *testing.T) {
	t.Parallel()
	lowerer, _ := schema.LoweredFor(t, openapitest.ComponentSpecVer("3.1.0", `    BadValOnly:
      type: string
      oneOf:
        - minLength: 1
          x-bad: .inf
        - maxLength: 5
`))
	lowerer.Diags.AppendAll(schema.LowerComponentSchemas(t.Context(), lowerer.Ctx, lowerer.Types, lowerer.Schemas()))
	_ = lowerer.Document(t)
	assert.Contains(t, schema.DiscardedSites(lowerer.Ctx), ir.Provenance{Source: 0, Pointer: "/components/schemas/BadValOnly/oneOf"})
}
