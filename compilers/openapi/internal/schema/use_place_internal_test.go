// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"testing"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// TestUseDefault_PlacesEachSchemaOfTheChain pins where defaultAt places the
// default each schema of a use's chain writes: the site's own at the carrier, a
// folded entry's at the folded entry, a referent's where the bindings place
// it, and one they place nowhere at the carrier, which reads it.
func TestUseDefault_PlacesEachSchemaOfTheChain(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    T: {type: string}\n"))
	loweringContext := lowerer.Ctx
	ref := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: new(references.Reference("#/x"))})
	unknown := placeOf(loweringContext, Standalone(ref))
	assert.False(t, unknown.known, "a schema the bindings do not know is placed nowhere")
	assert.False(t, unknown.beneath("/allOf/0").known, "nor is an entry beneath it")

	use := Use{FoldedAllOfEntries: []annotation.FoldedAllOfEntry{{At: "/v"}}}
	use.appendFallbackReferent(StandaloneSchema(&oas3.Schema{}), referentPlace{doc: loweringContext.Place(), pointer: "/r", known: true})
	use.appendFallbackReferent(StandaloneSchema(&oas3.Schema{}), unknown)
	const carrier = jsontext.Pointer("/p")
	for i, want := range []jsontext.Pointer{"/p/default", "/v/default", "/r/default", "/p"} {
		assert.Equal(t, want, use.defaultAt(loweringContext, i, carrier).Pointer, "schema %d of the chain", i)
	}
}

// TestKeysOf_ANilSchemaFallsBackToNothing pins keysOf's guard: a nil schema
// writes no key the policy reads, whatever the policy reads.
func TestKeysOf_ANilSchemaFallsBackToNothing(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    T: {type: string}\n"))
	require.NotEmpty(t, slices.Collect(lowerer.Ctx.Promoter().Keys()), "the default policy reads keys")
	assert.Equal(t, promotion.Referent{}, keysOf(lowerer.Ctx, nil))
}

// TestUseOf_ARunTheBindingsDoNotKnowFallsBackToWhatItWrites pins
// appendFallbackChainRun on a $ref the bindings read no hop for: the schema
// writing it is lent, and the run ends there, on no schema.
func TestUseOf_ARunTheBindingsDoNotKnowFallsBackToWhatItWrites(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    T: {type: string}\n"))
	ref := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: new(references.Reference("#/x"))})
	var use Use
	schema, ok := use.appendFallbackChainRun(lowerer.Ctx, lowerer.Ctx.Statements().Of(ref))
	assert.False(t, ok)
	assert.Nil(t, schema)
	require.Len(t, use.Referents, 1)
	assert.Same(t, ref.GetSchema(), statement.Parsed(use.Referents[0]))
}

// TestUseOf_ARunEndingUnresolvedFallsBackToNothing pins appendFallbackChainRun
// on a $ref whose chain the bindings read as ending nowhere: no schema of it is
// lent, and the run ends on none.
func TestUseOf_ARunEndingUnresolvedFallsBackToNothing(t *testing.T) {
	t.Parallel()
	lowerer, _ := LoweredFor(t, openapitest.ComponentSpec("    T: {$ref: '#/components/schemas/U'}\n"+
		"    U: {$ref: '#/components/schemas/Missing'}\n"))
	st := lowerer.Ctx.Statements().At(reftable.DocumentPointer{Doc: reftable.Source, Pointer: "/components/schemas/T"})
	require.NotNil(t, st.Ref)
	var use Use
	schema, ok := use.appendFallbackChainRun(lowerer.Ctx, st)
	assert.False(t, ok)
	assert.Nil(t, schema)
	assert.Empty(t, use.Referents)
}
