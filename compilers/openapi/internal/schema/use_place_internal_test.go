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
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// TestUseDefault_PlacesEachSchemaOfTheChain pins where defaultAt places the
// default each schema of a use's chain writes: the site's own at the carrier,
// a folded branch's at the folded branch, a referent's where the bindings place it, and one
// they place nowhere at the carrier, which reads it.
func TestUseDefault_PlacesEachSchemaOfTheChain(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    T: {type: string}\n"))
	c := l.Ctx
	ref := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: new(references.Reference("#/x"))})
	unknown := placeOf(c, Standalone(ref))
	assert.False(t, unknown.known, "a schema the bindings do not know is placed nowhere")
	assert.False(t, unknown.beneath("/allOf/0").known, "nor is a branch beneath it")

	u := Use{FoldedAllOfBranches: []annotation.FoldedAllOfBranch{{At: "/v"}}}
	u.appendFallbackReferent(StandaloneSchema(&oas3.Schema{}), referentPlace{doc: c.Place(), pointer: "/r", known: true})
	u.appendFallbackReferent(StandaloneSchema(&oas3.Schema{}), unknown)
	const carrier = jsontext.Pointer("/p")
	for i, want := range []jsontext.Pointer{"/p/default", "/v/default", "/r/default", "/p"} {
		assert.Equal(t, want, u.defaultAt(c, i, carrier).Pointer, "schema %d of the chain", i)
	}
}

// TestKeysOf_ANilSchemaFallsBackToNothing pins keysOf's guard: a nil schema writes
// no key the policy reads, whatever the policy reads.
func TestKeysOf_ANilSchemaFallsBackToNothing(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    T: {type: string}\n"))
	require.NotEmpty(t, slices.Collect(l.Ctx.Promoter().Keys()), "the default policy reads keys")
	assert.Equal(t, promotion.Referent{}, keysOf(l.Ctx, nil))
}

// TestUseOf_ARunTheBindingsDoNotKnowFallsBackToWhatItWrites pins appendFallbackChainRun on a $ref
// the bindings read no hop for: the schema writing it is lent, and the run
// ends there, on no schema.
func TestUseOf_ARunTheBindingsDoNotKnowFallsBackToWhatItWrites(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    T: {type: string}\n"))
	ref := oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{Ref: new(references.Reference("#/x"))})
	var u Use
	s, ok := u.appendFallbackChainRun(l.Ctx, l.Ctx.Statements().Of(ref))
	assert.False(t, ok)
	assert.Nil(t, s)
	require.Len(t, u.Referents, 1)
	assert.Same(t, ref.GetSchema(), statement.Parsed(u.Referents[0]))
}

// TestUseOf_ARunEndingUnresolvedFallsBackToNothing pins appendFallbackChainRun on a $ref whose
// chain the bindings read as ending nowhere: no schema of it is lent, and the
// run ends on none.
func TestUseOf_ARunEndingUnresolvedFallsBackToNothing(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec("    T: {$ref: '#/components/schemas/U'}\n"+
		"    U: {$ref: '#/components/schemas/Missing'}\n"))
	st := l.Ctx.Statements().At(bind.DocumentPointer{Doc: bind.Source, Pointer: "/components/schemas/T"})
	require.NotNil(t, st.Ref)
	var u Use
	s, ok := u.appendFallbackChainRun(l.Ctx, st)
	assert.False(t, ok)
	assert.Nil(t, s)
	assert.Empty(t, u.Referents)
}
