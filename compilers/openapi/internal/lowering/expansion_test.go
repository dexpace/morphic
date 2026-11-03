// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/ir"
)

// declaration is a tree weighing 14: a mapping (1), the keys a and b (2 each),
// the scalar xyz (4), a sequence (1) and its items 1 and 2 (2 each).
func declaration() *yaml.Node {
	return ynode.Map(ynode.Scalar("a"), ynode.Scalar("xyz"),
		ynode.Scalar("b"), ynode.Seq(ynode.Scalar("1"), ynode.Scalar("2")))
}

// doubling returns a sequence of two aliases of a sequence of two aliases, and
// so on levels deep, down to one scalar: a few nodes written that the model
// builds as 2^levels copies of the scalar.
func doubling(levels int) *yaml.Node {
	n := ynode.Scalar("x")
	for range levels {
		n = ynode.Seq(ynode.Alias(n), ynode.Alias(n))
	}
	return n
}

func TestWeigh_CountsEachNodeAndEachByteOfAScalar(t *testing.T) {
	t.Parallel()
	pair := ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))
	tests := []struct {
		name string
		root *yaml.Node
		want int
	}{
		{"nothing", nil, 0},
		{"a scalar", ynode.Scalar("abc"), 4},
		{"an empty mapping", ynode.Map(), 1},
		{"a declaration", declaration(), 14},
		{"an alias, as a copy of what it names", ynode.Seq(ynode.Alias(pair), ynode.Alias(pair)), 11},
		{"an alias root, as what it names", ynode.Alias(pair), 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, lowering.Weigh(tc.root, budget.Unbounded()))
		})
	}
}

// TestWeigh_CostsTheBudgetNotTheDeclaration pins the bound: weighing a
// declaration the model builds as 2^22 nodes visits a node past what is left,
// and says no more of it than that it weighs past it.
func TestWeigh_CostsTheBudgetNotTheDeclaration(t *testing.T) {
	t.Parallel()
	const within = 1000
	got := lowering.Weigh(doubling(22), budget.AtMost(within))
	assert.Greater(t, got, within, "a declaration past what is left weighs past it")
	assert.LessOrEqual(t, got, 2*(within+1), "no more than one node past it is visited, each weighing 2 at most")
}

func TestExpansion_ChargesEveryCopy(t *testing.T) {
	t.Parallel()
	root := declaration()
	e := lowering.NewExpansion(budget.AtMost(28))
	assert.True(t, e.Admit(root))
	assert.True(t, e.Admit(root), "a second copy reaching the budget is within it")
	_, refused := e.Refusal(0)
	assert.False(t, refused)

	assert.False(t, e.Admit(root), "a third crosses it")
	_, refused = e.Refusal(0)
	assert.True(t, refused)
	assert.False(t, e.Admit(ynode.Map()), "once crossed, nothing is admitted")
	assert.Equal(t, 42, e.Spent(), "every copy is charged in full, the first among them")
}

// TestExpansion_WeighsADeclarationOnce pins the memo: a declaration grown
// after it was weighed is charged its first weight, and an alias naming it is
// the same declaration.
func TestExpansion_WeighsADeclarationOnce(t *testing.T) {
	t.Parallel()
	root := declaration()
	e := lowering.NewExpansion(budget.AtMost(100))
	assert.True(t, e.Admit(root))
	root.Content = append(root.Content, ynode.Scalar("c"), ynode.Scalar("d"))
	assert.True(t, e.Admit(root))
	assert.True(t, e.Admit(ynode.Alias(root)))
	assert.Equal(t, 42, e.Spent())
	assert.Equal(t, 1, e.Weighed())
}

func TestExpansion_UnboundedAdmitsWithoutWeighing(t *testing.T) {
	t.Parallel()
	e := lowering.NewExpansion(budget.Unbounded())
	for range 3 {
		assert.True(t, e.Admit(declaration()))
	}
	assert.Zero(t, e.Spent())
	assert.Zero(t, e.Weighed())
	_, refused := e.Refusal(0)
	assert.False(t, refused)

	var none *lowering.Expansion
	assert.True(t, none.Admit(declaration()), "a zero context's expansion bounds nothing")
	_, refused = none.Refusal(0)
	assert.False(t, refused)
}

// TestExpansion_RefusalNamesTheBudgetAndNoPosition pins the refusal: an error
// at the source and at no pointer in it, since which $ref crossed the budget
// follows the walk's order.
func TestExpansion_RefusalNamesTheBudgetAndNoPosition(t *testing.T) {
	t.Parallel()
	e := lowering.NewExpansion(budget.AtMost(5))
	assert.False(t, e.Admit(declaration()))
	got, refused := e.Refusal(3)
	assert.True(t, refused)
	assert.Equal(t, ir.SeverityError, got.Severity)
	assert.Equal(t, diag.BudgetExceeded, got.Code)
	assert.Equal(t, ir.Provenance{Source: 3}, got.Provenance)
	assert.Contains(t, got.Message, "the 5 budgeted")
}

// TestDeclared_ResolvesNoReferencePastTheExpansionBudget pins what bounds a
// refused run's cost: once the copies cross the budget, a $ref resolves to
// nothing, at its use site, so no caller lowers another copy. An inline entry,
// which the source writes once, is not charged and still resolves.
func TestDeclared_ResolvesNoReferencePastTheExpansionBudget(t *testing.T) {
	t.Parallel()
	// P weighs 17: a mapping (1), the keys name (5) and in (3), and q (2) and
	// query (6).
	const src = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters: [{$ref: '#/components/parameters/P'}, {name: r, in: query}]
      responses: {"200": {description: ok}}
components:
  parameters:
    P: {name: q, in: query}
`
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	c := lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{MaxReferenceExpansion: budget.AtMost(17)},
		lowering.StreamingMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{},
		overlay.Origin{}).WithLoaded(doc)
	a, ok := c.Doc.Paths.Get("/a")
	require.True(t, ok)
	params := a.GetObject().Get().GetParameters()
	const usePtr = "/paths/~1a/get/parameters/0"

	p, _, at := lowering.Declared[soa.Parameter](c, params[0], usePtr)
	require.NotNil(t, p, "a copy weighing the budget is within it")
	assert.Equal(t, "/components/parameters/P", string(at))
	_, refused := c.ExpansionRefusal()
	assert.False(t, refused)

	p, _, at = lowering.Declared[soa.Parameter](c, params[0], usePtr)
	assert.Nil(t, p, "a second copy crosses it")
	assert.Equal(t, usePtr, string(at))
	_, refused = c.ExpansionRefusal()
	assert.True(t, refused)

	inline, _, _ := lowering.Declared[soa.Parameter](c, params[1], "/paths/~1a/get/parameters/1")
	require.NotNil(t, inline)
	assert.Equal(t, "r", inline.GetName())
}
