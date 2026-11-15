// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestBind_AReferenceObjectWaitsForADeclarationOfADocumentRead pins that a
// Reference Object whose path a document declares by `$id`, that document read
// and its declaration not yet reached by the pass, waits for it, as a schema's
// $ref does, and is answered by the declaration: no other hop waits at the
// time, so a pass that did not go on for it would answer it from nothing.
func TestBind_AReferenceObjectWaitsForADeclarationOfADocumentRead(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"/a.yaml": "R: {description: x}\n",
		"/z.yaml": "S: {$id: 'a.yaml', R: {description: y}}\n",
	}
	const src = head + "components:\n  schemas:\n    Z: {$ref: 'z.yaml#/S'}\n  responses:\n    R: {$ref: 'a.yaml#/R'}\n"
	cfg := resolving
	cfg.Open = func(name, _ string) (*yaml.Node, error) { return openapitest.YAMLDocument(t, docs[name]), nil }

	table := bound(t, src, cfg)

	refs := table.References()
	require.Len(t, refs, 1)
	assert.Equal(t, bind.Position{Pointer: "/components/responses/R"}, refs[0].Site)
	require.Equal(t, bind.Resolved, refs[0].Hop.Status, refs[0].Hop.Why())
	assert.Equal(t, bind.Position{Doc: docNamed(t, table, "/z.yaml"), Pointer: "/S/R"}, refs[0].Hop.Target,
		"the declaration answers it, not the file")
}

// TestBind_ABudgetThatRunsOutInAPassWhoseDeclarationsChangedEndsThere pins that
// a pass the budget ran out in is the last though what it reached changed a URI
// it asked for: at each limit from the one that ends the first pass to the one
// below the least that begins a second, the first stands, and one step more
// begins the second. The first pass of this input costs 62 to 66 steps of
// budget; a second begun with none would answer nothing.
func TestBind_ABudgetThatRunsOutInAPassWhoseDeclarationsChangedEndsThere(t *testing.T) {
	t.Parallel()
	const src = head + "components:\n  schemas:\n    Y: {$ref: 'a.yaml'}\n"
	docs := map[string]string{
		"/a.yaml": "type: integer\n$defs: {Z: {$ref: 'z.yaml#/S'}}\n",
		"/z.yaml": "S: {$id: 'a.yaml', type: string}\n",
	}
	passes := func(limit int) int {
		cfg := withDocuments(docs)
		cfg.Self, cfg.Locate, cfg.Limit = resolving.Self, resolving.Locate, budget.AtMost(limit)
		return bound(t, src, cfg).Passes()
	}

	for limit := 62; limit <= 66; limit++ {
		assert.Equal(t, 1, passes(limit), "limit %d: the pass the budget ran out in stands", limit)
	}
	assert.Equal(t, 2, passes(67), "a budget left after the first pass begins a second")
}
