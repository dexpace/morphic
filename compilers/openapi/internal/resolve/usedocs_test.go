// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// TestUseDocs_UseSiteSiblingsOverrideTheDeclaration pins the fold along a
// chain: a summary or description written beside a $ref wins field by field
// over what it names, the use site's over an alias's and an alias's over the
// declaration's, and one an entry omits, or writes empty or null, leaves the
// one beneath it standing. One beside a $ref a `<<` merges in counts, as the
// table reads that $ref, and an inline entry, a nil one and one the bindings
// never read fold nothing.
func TestUseDocs_UseSiteSiblingsOverrideTheDeclaration(t *testing.T) {
	t.Parallel()
	doc := loaded(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  parameters:
    P: {name: p, in: query}
    Alias: {$ref: '#/components/parameters/P', summary: alias summary, description: alias description}
    Both: {$ref: '#/components/parameters/Alias', summary: use summary, description: use description}
    Described: {$ref: '#/components/parameters/Alias', description: use description, summary: ''}
    Nulled: {$ref: '#/components/parameters/Alias', description: null, summary: ~}
    Merged: {<<: {$ref: '#/components/parameters/P'}, description: merged description}
`)
	declared := ir.Docs{Summary: "declared summary", Description: "declared description"}
	params := doc.Doc.Components.GetParameters()
	for name, want := range map[string]ir.Docs{
		"Both":      {Summary: "use summary", Description: "use description"},
		"Described": {Summary: "alias summary", Description: "use description"},
		"Nulled":    {Summary: "alias summary", Description: "alias description"},
		"Alias":     {Summary: "alias summary", Description: "alias description"},
		"Merged":    {Summary: "declared summary", Description: "merged description"},
		"P":         declared,
	} {
		got := resolve.UseDocs[soa.Parameter](doc.Bindings, params.GetOrZero(name), declared)
		assert.Empty(t, cmp.Diff(want, got), name)
	}
	assert.Equal(t, declared, resolve.UseDocs[soa.Parameter](doc.Bindings, (*soa.ReferencedParameter)(nil), declared))
	assert.Equal(t, declared, resolve.UseDocs[soa.Parameter](doc.Bindings, &soa.ReferencedParameter{}, declared))
}
