// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestRefinementOf_AnUnrefinedSchemaHoldsNothing pins that a schema whose allOf
// composes, or may not refine, gives its readers no refinement to read.
func TestRefinementOf_AnUnrefinedSchemaHoldsNothing(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    Obj: {type: object, properties: {a: {type: string}}}
    T: {allOf: [{$ref: '#/components/schemas/Obj'}, {properties: {b: {type: string}}}]}
    U: {type: string}
`))
	for _, name := range []string{"T", "U"} {
		r, refined := refinementOf(l.Ctx, StatedIn(l, componentSchema(t, l, name)), jsontext.Pointer("/components/schemas/"+name))
		assert.False(t, refined, name)
		assert.Empty(t, r.comp.branches, "%s: no branch list is built", name)
		assert.Nil(t, r.principal, name)
	}
}

// TestEntryAt_ReadsTheBranchAtAnIndex pins the statement a reader of the
// decision with no pointer takes for an index, and none for -1.
func TestEntryAt_ReadsTheBranchAtAnIndex(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    Port: {type: integer}
    T: {allOf: [{description: d}, {$ref: '#/components/schemas/Port'}]}
`))
	s := StatedIn(l, componentSchema(t, l, "T"))
	assert.Nil(t, entryAt(l.Ctx, s, -1))
	got := entryAt(l.Ctx, s, 1)
	require.NotNil(t, got)
	assert.Same(t, allOfBranches(l.Ctx, s, "").branches[1].schema, got)
	assert.NotNil(t, got.Ref, "the entry at 1 is the $ref")
}
