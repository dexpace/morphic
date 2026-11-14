// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// compileAtLimit compiles src under a reference-expansion budget of limit, -1
// for none, and returns the document, nil where it was refused, and the
// findings, each as "severity code pointer", sorted.
func compileAtLimit(t *testing.T, src []byte, limit int) (*ir.Document, []string) {
	t.Helper()
	doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{{Path: "spec.yaml", Data: src}},
		compilers.Options{FormatOptions: openapi.Options{Limits: openapi.Limits{MaxReferenceExpansion: limit}}})
	require.NoError(t, err)
	return doc, findingsAt(diags)
}

// holderProperty returns the property of wire name wire on the component
// model Holder of doc.
func holderProperty(t *testing.T, doc *ir.Document, wire string) ir.Property {
	t.Helper()
	m, ok := doc.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok)
	p, ok := propByWire(m, wire)
	require.True(t, ok, "no property %s", wire)
	return p
}

// assertOneReadingInEitherOrder compiles head followed by the blocks, then by
// the blocks in reverse, under limit, and requires one document and one set of
// findings: the order of the blocks decides which declaration a walk meets
// first, and nothing a consumer reads, the order each slice holds included.
func assertOneReadingInEitherOrder(t *testing.T, head string, blocks []string, limit int) (*ir.Document, []string) {
	t.Helper()
	forward, forwardFound := compileAtLimit(t, []byte(head+strings.Join(blocks, "")), limit)
	backward, backwardFound := compileAtLimit(t, []byte(head+strings.Join(reversedBlocks(blocks), "")), limit)
	assert.Empty(t, cmp.Diff(forward, backward, cmpopts.IgnoreFields(ir.SourceInfo{}, "Hash"),
		cmpopts.IgnoreFields(ir.Provenance{}, "Position"), cmpopts.IgnoreFields(ir.Document{}, "Diagnostics")),
		"one document in either declaration order")
	assert.Equal(t, forwardFound, backwardFound, "and one set of findings")
	return forward, forwardFound
}

// reversedBlocks returns a copy of blocks in reverse order.
func reversedBlocks(blocks []string) []string {
	out := slices.Clone(blocks)
	slices.Reverse(out)
	return out
}

// TestUseSites_AnAliasChainLendsAlikeInEitherOrder pins the annotations a use
// takes from its chain, nearest hop first, with a default no ir.Value holds
// reported at its own position: the aliases, the use and the schema they end
// on declared in either order lower to one document and one set of findings.
func TestUseSites_AnAliasChainLendsAlikeInEitherOrder(t *testing.T) {
	t.Parallel()
	doc, found := assertOneReadingInEitherOrder(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"+
		"components:\n  schemas:\n", []string{
		"    Leaf: {type: number, description: from-leaf, default: 1}\n",
		"    Fine: {$ref: '#/components/schemas/Leaf', description: from-fine, default: 2}\n",
		"    Bad: {$ref: '#/components/schemas/Fine', default: .nan}\n",
		"    Bare: {$ref: '#/components/schemas/Fine'}\n",
		"    Holder:\n      type: object\n      properties:\n" +
			"        b: {$ref: '#/components/schemas/Bare'}\n" +
			"        bad: {$ref: '#/components/schemas/Bad'}\n" +
			"        own: {$ref: '#/components/schemas/Bare', description: from-use}\n",
	}, -1)
	assert.Contains(t, found, "warning openapi/unlowered-construct /components/schemas/Bad/default")
	b := holderProperty(t, doc, "b")
	require.NotNil(t, b.Default)
	assert.Equal(t, ir.Value{Kind: ir.ValueNumber, Num: "2"}, *b.Default)
	assert.Equal(t, "from-fine", b.Docs.Description)
	assert.Nil(t, holderProperty(t, doc, "bad").Default)
	assert.Equal(t, "from-use", holderProperty(t, doc, "own").Docs.Description)
}

// TestUseSites_APositionTwoReadersReachLowersAlikeInEitherOrder pins the node at
// a position both a lowered property's $ref and a kept keyword's name, and an
// alias a chain passes: whichever declaration a walk meets first, the node is
// built once, where it is declared, so either order lowers one document.
func TestUseSites_APositionTwoReadersReachLowersAlikeInEitherOrder(t *testing.T) {
	t.Parallel()
	doc, _ := assertOneReadingInEitherOrder(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"+
		"components:\n  schemas:\n", []string{
		"    Kept:\n      type: object\n      not: {$ref: '#/components/schemas/Typed/$defs/Mid'}\n" +
			"      if: {$ref: '#/components/schemas/Typed/$defs/Sub'}\n",
		"    Typed:\n      type: object\n      $defs:\n" +
			"        Mid: {$ref: '#/components/schemas/Typed/$defs/Sub', description: mid}\n" +
			"        Sub: {type: object, properties: {s: {type: string}}}\n" +
			"      properties:\n        a: {$ref: '#/components/schemas/Typed/$defs/Sub'}\n",
	}, -1)
	mid, ok := doc.Types["t/anon/components/schemas/Typed/$defs/Mid"]
	require.True(t, ok, "the alias only the kept keyword reaches is lowered")
	assert.Equal(t, "mid", mid.Common().Docs.Description)
	assert.Contains(t, doc.Types, ir.TypeID("t/anon/components/schemas/Typed/$defs/Sub"))
}

// TestUseSites_APathItemLandingMountsAlikeInEitherOrder pins a path item two
// paths reach through a chain of landings under an x- key, each writing an
// operation beside its $ref: the landings written before the paths or after
// them, and the two in either order, mount one document.
func TestUseSites_APathItemLandingMountsAlikeInEitherOrder(t *testing.T) {
	t.Parallel()
	const paths = "paths:\n  /a: {$ref: '#/x-items/outer'}\n  /c: {$ref: '#/x-items/inner'}\n" +
		"  /b: {get: {operationId: getB, responses: {\"200\": {description: ok}}}}\n"
	for _, items := range [][2]string{
		{"  outer: {$ref: '#/x-items/inner', put: {operationId: putOuter, responses: {\"200\": {description: ok}}}}\n",
			"  inner: {$ref: '#/paths/~1b', post: {operationId: postInner, responses: {\"200\": {description: ok}}}}\n"},
	} {
		for _, landings := range []string{"x-items:\n" + items[0] + items[1], "x-items:\n" + items[1] + items[0]} {
			doc, found := assertOneReadingInEitherOrder(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n",
				[]string{landings, paths}, -1)
			assert.Equal(t, []string{"warning openapi/duplicate-operation-id /paths/~1a/get",
				"warning openapi/duplicate-operation-id /paths/~1c/get",
				"warning openapi/duplicate-operation-id /paths/~1c/post"}, found,
				"two paths mounting one item repeat its operation IDs, and nothing else is reported")
			assert.Len(t, operationIDs(doc), 6, "/a's put, post and get, /c's post and get, and /b's get")
		}
	}
}

// TestUseSites_AUsesChargeHoldsInEitherOrder pins the reference-expansion charge
// of uses writing keys beside their $ref and of an alias between: the verdict at
// the exact weight, and one below it, is the same in either order.
func TestUseSites_AUsesChargeHoldsInEitherOrder(t *testing.T) {
	t.Parallel()
	// P weighs 25 as paramP does; A, an alias of P, weighs 1 + description (12)
	// + dd (3) = 16; each use's mapping weighs 1, and one writing a description
	// beside its $ref 1 + 12 + 3 = 16 too.
	const head = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\n"
	blocks := []string{`paths:
  /a:
    get:
      parameters: [{$ref: '#/components/parameters/A', description: dd}]
      responses: {"200": {description: ok}}
  /b:
    get:
      parameters: [{$ref: '#/components/parameters/P'}]
      responses: {"200": {description: ok}}
`, `components:
  parameters:
    A: {$ref: '#/components/parameters/P', description: dd}
    P: {name: q, in: query, schema: {type: string}}
`}
	const weight = (16 + 16 + 25) + (1 + 25)
	doc, found := assertOneReadingInEitherOrder(t, head, blocks, weight)
	require.NotNil(t, doc, "admitted at the weight: %v", found)
	doc, found = assertOneReadingInEitherOrder(t, head, blocks, weight-1)
	assert.Nil(t, doc)
	assert.Equal(t, []string{"error openapi/budget-exceeded "}, found)
}
