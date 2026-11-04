// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering_test

import (
	"encoding/json/jsontext"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/overlay"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/ir"
)

// raw reads a tree as raw YAML, where no position holds a schema.
var raw = bind.Reading{}

// declaration is a tree weighing 14 read as raw YAML: a mapping (1), the keys
// a and b (2 each), the scalar xyz (4), a sequence (1) and its items 1 and 2
// (2 each).
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

// reading returns how the grammar reads the source position pointer, which
// holds an object a $ref can name.
func reading(t *testing.T, pointer string) bind.Reading {
	t.Helper()
	table := bind.Bind(t.Context(), openapitest.YAMLDocument(t, "openapi: 3.1.0\n"), bind.Config{Self: "spec.yaml"})
	r := table.ReadingAt(bind.Position{Pointer: jsontext.Pointer(pointer)})
	require.True(t, r.Referable())
	return r
}

// parameter returns how the grammar reads a component parameter.
func parameter(t *testing.T) bind.Reading {
	t.Helper()
	return reading(t, "/components/parameters/P")
}

func TestWeightOf_CountsEachNodeAndEachByteOfAScalar(t *testing.T) {
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
		{"a schema key, which raw YAML holds no schema under", ynode.Map(ynode.Scalar("schema"), pair), 13},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, lowering.WeightOf(tc.root, raw, budget.Unbounded()))
		})
	}
}

// TestWeightOf_CountsASchemaAsOneNode pins what a copy of a parameter is
// charged for its schema: one node whatever the schema holds, written inline,
// as a scalar or behind an alias, since it lowers once, to a type every copy
// refers to. A schema key in an example's value is data the copy holds, so it
// is weighed in full.
func TestWeightOf_CountsASchemaAsOneNode(t *testing.T) {
	t.Parallel()
	big := ynode.Map(ynode.Scalar("type"), ynode.Scalar("object"), ynode.Scalar("properties"), declaration())
	withSchema := func(schema *yaml.Node) *yaml.Node {
		return ynode.Map(ynode.Scalar("name"), ynode.Scalar("q"), ynode.Scalar("schema"), schema)
	}
	// A mapping (1), name (5), q (2), schema (7) and the schema (1).
	const asOne = 16
	inExample := ynode.Map(ynode.Scalar("examples"), ynode.Map(ynode.Scalar("e"),
		ynode.Map(ynode.Scalar("value"), ynode.Map(ynode.Scalar("schema"), ynode.Scalar("abc")))))
	param, item := parameter(t), reading(t, "/components/pathItems/I")
	tests := []struct {
		name string
		root *yaml.Node
		at   bind.Reading
		want int
	}{
		{"an inline schema", withSchema(big), param, asOne},
		{"a schema written as a scalar", withSchema(ynode.Scalar("true")), param, asOne},
		{"a schema behind an alias", withSchema(ynode.Alias(big)), param, asOne},
		// A mapping (1), parameters (11), a sequence (1) and the parameter.
		{"a schema of a path item's parameter", ynode.Map(ynode.Scalar("parameters"), ynode.Seq(withSchema(big))),
			item, 13 + asOne},
		// A mapping (1), examples (9), a mapping (1), e (2), a mapping (1),
		// value (6), a mapping (1), schema (7) and abc (4).
		{"a schema key in an example's value", inExample, param, 32},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, lowering.WeightOf(tc.root, tc.at, budget.Unbounded()))
		})
	}
	assert.Greater(t, lowering.WeightOf(withSchema(big), raw, budget.Unbounded()), asOne,
		"read as raw YAML, the same tree holds no schema")
}

// TestWeightOf_CostsTheBudgetNotTheDeclaration pins the bound: weighing a
// declaration the model builds as 2^22 nodes visits a node past what is left,
// and says no more of it than that it weighs past it.
func TestWeightOf_CostsTheBudgetNotTheDeclaration(t *testing.T) {
	t.Parallel()
	const within = 1000
	got := lowering.WeightOf(doubling(22), raw, budget.AtMost(within))
	assert.Greater(t, got, within, "a declaration past what is left weighs past it")
	assert.LessOrEqual(t, got, 2*(within+1), "no more than one node past it is visited, each weighing 2 at most")
}

func TestExpansion_ChargesEveryCopy(t *testing.T) {
	t.Parallel()
	root := declaration()
	e := lowering.NewExpansion(budget.AtMost(28))
	assert.True(t, e.Admit(raw, root))
	assert.True(t, e.Admit(raw, root), "a second copy reaching the budget is within it")
	_, refused := e.Refusal(0)
	assert.False(t, refused)

	assert.False(t, e.Admit(raw, root), "a third crosses it")
	_, refused = e.Refusal(0)
	assert.True(t, refused)
	assert.False(t, e.Admit(raw, ynode.Map()), "once crossed, nothing is admitted")
	assert.Equal(t, 42, e.Spent(), "every copy is charged in full, the first among them")
}

// TestExpansion_ChargesEachDeclarationOfAChain pins a copy lowered from
// several declarations, a chain's end and each alias it passes through: it is
// charged their sum, and one crossing the budget part way admits none of it.
func TestExpansion_ChargesEachDeclarationOfAChain(t *testing.T) {
	t.Parallel()
	alias := ynode.Map(ynode.Scalar("$ref"), ynode.Scalar("#/x"))
	e := lowering.NewExpansion(budget.AtMost(2 * (14 + 10)))
	assert.True(t, e.Admit(raw, declaration(), alias))
	assert.Equal(t, 14+10, e.Spent(), "the end (14) and the alias, a mapping (1), $ref (5) and #/x (4)")
	assert.True(t, e.Admit(raw, declaration(), alias))

	assert.False(t, e.Admit(raw, ynode.Scalar(""), declaration()), "a chain crossing at its second declaration")
	_, refused := e.Refusal(0)
	assert.True(t, refused)
}

// TestExpansion_WeighsADeclarationOnce pins the memo: a declaration grown
// after it was weighed is charged its first weight, and an alias naming it is
// the same declaration, while a declaration read another way is weighed again,
// since the reading decides what in it is a schema.
func TestExpansion_WeighsADeclarationOnce(t *testing.T) {
	t.Parallel()
	root := declaration()
	e := lowering.NewExpansion(budget.AtMost(100))
	assert.True(t, e.Admit(raw, root))
	root.Content = append(root.Content, ynode.Scalar("c"), ynode.Scalar("d"))
	assert.True(t, e.Admit(raw, root))
	assert.True(t, e.Admit(raw, ynode.Alias(root)))
	assert.Equal(t, 42, e.Spent())
	assert.Equal(t, 1, e.Weighed())

	assert.True(t, e.Admit(parameter(t), root))
	assert.Equal(t, 42+18, e.Spent(), "read as a parameter, the grown tree, holding no schema, weighs 18")
	assert.Equal(t, 2, e.Weighed())
}

func TestExpansion_UnboundedAdmitsWithoutWeighing(t *testing.T) {
	t.Parallel()
	e := lowering.NewExpansion(budget.Unbounded())
	for range 3 {
		assert.True(t, e.Admit(raw, declaration()))
	}
	assert.Zero(t, e.Spent())
	assert.Zero(t, e.Weighed())
	_, refused := e.Refusal(0)
	assert.False(t, refused)

	var none *lowering.Expansion
	assert.True(t, none.Admit(raw, declaration()), "a zero context's expansion bounds nothing")
	_, refused = none.Refusal(0)
	assert.False(t, refused)
}

// TestExpansion_RefusalNamesTheBudgetAndNoPosition pins the refusal: an error
// at the source and at no pointer in it, since which $ref crossed the budget
// follows the walk's order.
func TestExpansion_RefusalNamesTheBudgetAndNoPosition(t *testing.T) {
	t.Parallel()
	e := lowering.NewExpansion(budget.AtMost(5))
	assert.False(t, e.Admit(raw, declaration()))
	got, refused := e.Refusal(3)
	assert.True(t, refused)
	assert.Equal(t, ir.Diagnostic{Severity: ir.SeverityError, Code: diag.BudgetExceeded,
		Provenance: ir.Provenance{Source: 3},
		Message: "the copies its $refs and path items make weigh more than the 5 budgeted for them: each $ref " +
			"to a declaration other than a schema or a link lowers its own copy of it, and each " +
			"additionalOperations entry its own copy of what its path item declares"}, got)
}

// loaded returns the context of src, loaded as a source, with its $refs'
// copies held to limit.
func loaded(t *testing.T, src string, limit budget.Limit) lowering.Ctx {
	t.Helper()
	doc, _, err := load.Load(t.Context(), 0, compilers.Source{Path: "spec.yaml", Data: []byte(src)}, load.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return lowering.New(0, doc.Doc, doc.Source, "", lowering.Limits{MaxReferenceExpansion: limit},
		lowering.StreamingMedia{}, lowering.BinaryMedia{}, lowering.ExtensionPromotions{}, lowering.NullableWidening{},
		overlay.Origin{}).WithLoaded(doc)
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
	c := loaded(t, src, budget.AtMost(17))
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

// TestDeclared_ChargesEachDeclarationOfAChain pins what a $ref through two
// aliases is charged: each alias, whose description a use lowers, and the
// end, whose schema counts as one node, read as the parameter the use site
// writes.
func TestDeclared_ChargesEachDeclarationOfAChain(t *testing.T) {
	t.Parallel()
	// A and C weigh 47 each: a mapping (1), $ref (5), its 25-byte value (26),
	// description (12) and dd (3). B weighs 25: a mapping (1), name (5), q (2),
	// in (3), query (6), schema (7) and the schema (1).
	const src = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      parameters: [{$ref: '#/components/parameters/A'}]
      responses: {"200": {description: ok}}
components:
  parameters:
    A: {$ref: '#/components/parameters/C', description: dd}
    C: {$ref: '#/components/parameters/B', description: dd}
    B: {name: q, in: query, schema: {type: string, maxLength: 3}}
`
	const usePtr = "/paths/~1a/get/parameters/0"
	for limit, admitted := range map[int]bool{2*47 + 25: true, 2*47 + 25 - 1: false} {
		c := loaded(t, src, budget.AtMost(limit))
		a, ok := c.Doc.Paths.Get("/a")
		require.True(t, ok)
		p, _, _ := lowering.Declared[soa.Parameter](c, a.GetObject().Get().GetParameters()[0], usePtr)
		assert.Equal(t, admitted, p != nil, "under %d", limit)
		_, refused := c.ExpansionRefusal()
		assert.Equal(t, !admitted, refused, "under %d", limit)
	}
}

// TestReadingOf_ReadsTheObjectTheUseSiteWrites pins the reading a copy is
// weighed under: the object read at the $ref's position, and raw YAML where
// that is no object a $ref names, or where the context holds no table.
func TestReadingOf_ReadsTheObjectTheUseSiteWrites(t *testing.T) {
	t.Parallel()
	c := loaded(t, "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n", budget.Unbounded())
	param := lowering.ReadingOf(c, "/paths/~1a/get/parameters/0")
	assert.True(t, param.Referable())
	assert.True(t, param.Below("schema").Schema())
	assert.Equal(t, bind.Reading{}, lowering.ReadingOf(c, "/paths/~1a/get/parameters/0/schema"),
		"a schema is no object a $ref of this kind names")
	assert.Equal(t, bind.Reading{}, lowering.ReadingOf(lowering.Ctx{}, "/paths/~1a/get/parameters/0"))
	assert.Equal(t, bind.Reading{}, lowering.ReadingOf(c.InScope(resolve.Scope{Foreign: true}), "/paths/~1a/get/parameters/0"),
		"what another document holds, read at the source's coordinates, has no position of its own")
}

// TestRecopied_ChargesTheRunsExpansion pins the two charges a copy made with
// no $ref adds to the budget: a declaration, weighed as the grammar reads its
// position, here a path item's parameter weighing 25 as paramP's does, and raw
// bytes beside it. Past the budget the run is refused, and a zero context
// bounds nothing.
func TestRecopied_ChargesTheRunsExpansion(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.2.0
info: {title: T, version: "1"}
paths:
  /a:
    parameters: [{name: q, in: query, schema: {type: string}}]
    get: {responses: {"200": {description: ok}}}
`
	c := loaded(t, src, budget.AtMost(25+3))
	a, ok := c.Doc.Paths.Get("/a")
	require.True(t, ok)
	param := a.GetObject().Parameters[0].GetObject().GetRootNode()

	assert.True(t, lowering.RecopiedAt(c, "/paths/~1a/parameters/0", param), "a copy weighing 25 is within 28")
	assert.True(t, lowering.Recopied(c, 3), "and three raw bytes beside it reach the budget")
	_, refused := c.ExpansionRefusal()
	assert.False(t, refused)
	assert.False(t, lowering.Recopied(c, 1), "one more crosses it")
	_, refused = c.ExpansionRefusal()
	assert.True(t, refused)
	assert.False(t, lowering.RecopiedAt(c, "/paths/~1a/parameters/0", param), "once crossed, nothing is admitted")

	assert.True(t, lowering.Recopied(lowering.Ctx{}, 1<<40), "a zero context's expansion bounds nothing")
}
