// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestRefBranchTarget_InlineBranchNamesNothing pins that an inline branch has
// no target, which is the answer a caller that did not filter to $refs gets.
func TestRefBranchTarget_InlineBranchNamesNothing(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    M:
      allOf:
        - {type: object}
`))
	decl, ok := l.Doc.Components.Schemas.Get("M")
	require.True(t, ok)
	assert.Nil(t, refBranchTarget(l.Ctx, l.Ctx.Statements().Of(decl.GetSchema().GetAllOf()[0])))
}

// TestRefKeyword_NamesTheKeywordOfAReferenceSite pins the keyword each entry is
// a reference site under, as the bindings read it: `$ref`, a `$dynamicRef`
// read as the $ref it spells, and none for an inline entry.
func TestRefKeyword_NamesTheKeywordOfAReferenceSite(t *testing.T) {
	t.Parallel()
	l, _ := LoweredFor(t, openapitest.ComponentSpec(`    T: {type: string}
    M:
      allOf:
        - {$ref: '#/components/schemas/T'}
        - {$dynamicRef: '#/components/schemas/T'}
        - {type: object}
`))
	entries := StatedIn(l, componentSchema(t, l, "M")).AllOf
	got := make([]string, 0, len(entries))
	for _, b := range entries {
		got = append(got, refKeyword(l.Ctx.Statements().Subschema(b)))
	}
	assert.Equal(t, []string{"$ref", "$dynamicRef", ""}, got)
}
