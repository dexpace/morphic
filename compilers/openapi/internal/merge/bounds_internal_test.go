// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// inOrders calls check with decls in every order. The declarations keep their
// own pointers, so a result that depends on where one sits fails an order.
func inOrders(t *testing.T, decls []declaration, check func(t *testing.T, ordered []declaration)) {
	t.Helper()
	for _, order := range permutations(len(decls)) {
		ordered := make([]declaration, 0, len(decls))
		for _, i := range order {
			ordered = append(ordered, decls[i])
		}
		check(t, ordered)
	}
}

// propertyBounds is one declaration of a string field per constraint set, each
// at a pointer of its own, whose source fails the test if it is kept.
func propertyBounds(t *testing.T, sets ...*ir.Constraints) []declaration {
	t.Helper()
	decls := make([]declaration, len(sets))
	for i, c := range sets {
		decls[i] = nthDecl(i, primRef(ir.PrimString).Target)
		decls[i].prop.Constraints = c
		decls[i].source = unread(t)
	}
	return decls
}

// TestFold_ABoundTakesItsTightestValueInEveryOrder pins the intersection of a
// keyword several declarations set. The field takes the value holding them all,
// and the others say nothing it does not, so nothing is reported or kept (the
// declarations' sources fail the test if rendered). Three declarations fold in
// every order, with the loosest and tightest in every position: a fold reading
// pairs in order would answer differently for some of them.
func TestFold_ABoundTakesItsTightestValueInEveryOrder(t *testing.T) {
	t.Parallel()
	big := func(v string) *ir.BigVal { return new(ir.BigVal(v)) }
	count := func(n int64) *int64 { return new(n) }
	for _, tc := range []struct {
		name string
		sets [3]*ir.Constraints
		want *ir.Constraints
	}{
		{"minimum", [3]*ir.Constraints{{Min: big("1")}, {Min: big("5")}, {Min: big("3")}}, &ir.Constraints{Min: big("5")}},
		{"exclusiveMinimum", [3]*ir.Constraints{{ExclusiveMin: big("1")}, {ExclusiveMin: big("5")}, {ExclusiveMin: big("3")}}, &ir.Constraints{ExclusiveMin: big("5")}},
		{"maximum", [3]*ir.Constraints{{Max: big("9")}, {Max: big("2")}, {Max: big("5")}}, &ir.Constraints{Max: big("2")}},
		{"exclusiveMaximum", [3]*ir.Constraints{{ExclusiveMax: big("9")}, {ExclusiveMax: big("2")}, {ExclusiveMax: big("5")}}, &ir.Constraints{ExclusiveMax: big("2")}},
		{"minLength", [3]*ir.Constraints{{MinLength: count(1)}, {MinLength: count(5)}, {MinLength: count(3)}}, &ir.Constraints{MinLength: count(5)}},
		{"maxLength", [3]*ir.Constraints{{MaxLength: count(9)}, {MaxLength: count(2)}, {MaxLength: count(5)}}, &ir.Constraints{MaxLength: count(2)}},
		{"minItems", [3]*ir.Constraints{{MinItems: count(1)}, {MinItems: count(5)}, {MinItems: count(3)}}, &ir.Constraints{MinItems: count(5)}},
		{"maxItems", [3]*ir.Constraints{{MaxItems: count(9)}, {MaxItems: count(2)}, {MaxItems: count(5)}}, &ir.Constraints{MaxItems: count(2)}},
		{"minProperties", [3]*ir.Constraints{{MinProps: count(1)}, {MinProps: count(5)}, {MinProps: count(3)}}, &ir.Constraints{MinProps: count(5)}},
		{"maxProperties", [3]*ir.Constraints{{MaxProps: count(9)}, {MaxProps: count(2)}, {MaxProps: count(5)}}, &ir.Constraints{MaxProps: count(2)}},
		{"multipleOf, each a multiple of the last", [3]*ir.Constraints{{MultipleOf: big("2")}, {MultipleOf: big("12")}, {MultipleOf: big("4")}}, &ir.Constraints{MultipleOf: big("12")}},
		{"multipleOf, no two dividing", [3]*ir.Constraints{{MultipleOf: big("4")}, {MultipleOf: big("6")}, {MultipleOf: big("10")}}, &ir.Constraints{MultipleOf: big("60")}},
		{"uniqueItems", [3]*ir.Constraints{{}, {UniqueItems: true}, {}}, &ir.Constraints{UniqueItems: true}},
		{"a bound beside its exclusive form", [3]*ir.Constraints{{Min: big("1")}, {ExclusiveMin: big("5")}, {Min: big("3")}}, &ir.Constraints{Min: big("3"), ExclusiveMin: big("5")}},
		{"one keyword from each", [3]*ir.Constraints{{MinLength: count(1)}, {MaxLength: count(5)}, {Pattern: "^a$"}}, &ir.Constraints{MinLength: count(1), MaxLength: count(5), Pattern: "^a$"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inOrders(t, propertyBounds(t, tc.sets[:]...), func(t *testing.T, ordered []declaration) {
				t.Helper()
				g, recorded := stubMerger(narrowingRegistry())
				got := g.fold(ordered)

				assert.Equal(t, tc.want, got.Constraints)
				assert.Empty(t, *recorded, "a bound the field implies is no disagreement")
				assert.Empty(t, got.Unmodeled, "and nothing of it is lost")
			})
		})
	}
}

// TestFold_ABoundLeavesTheDeclarationsItFoldsAlone pins that a fold reads the
// constraints it meets: a declaration's are shared with whatever else holds it,
// so one tightened in place would change a field the fold never saw.
func TestFold_ABoundLeavesTheDeclarationsItFoldsAlone(t *testing.T) {
	t.Parallel()
	loose, tight := &ir.Constraints{MaxLength: new(int64(9))}, &ir.Constraints{MaxLength: new(int64(2))}
	g, _ := stubMerger(narrowingRegistry())

	got := g.fold(propertyBounds(t, loose, tight))

	require.NotNil(t, got.Constraints)
	assert.Equal(t, int64(2), *got.Constraints.MaxLength)
	assert.Equal(t, int64(9), *loose.MaxLength, "the declaration read is as it was written")
	assert.Equal(t, &ir.Constraints{MaxLength: new(int64(9))}, loose)
}

// TestBoundsRelation_JudgesWhichSaysAllTheOtherDoes pins the verdict on two nodes
// alike but for their bounds, a verdict the fold rests on: narrower is adopted
// silently, so a pair called narrower that is not would lose a bound with no
// report. Each row reads in both directions.
func TestBoundsRelation_JudgesWhichSaysAllTheOtherDoes(t *testing.T) {
	t.Parallel()
	big := func(v string) *ir.BigVal { return new(ir.BigVal(v)) }
	count := func(n int64) *int64 { return new(n) }
	for _, tc := range []struct {
		name string
		a, b *ir.Constraints
		want relation
	}{
		{"neither says anything", nil, nil, relSame},
		{"nothing against an empty set", nil, &ir.Constraints{}, relSame},
		{"one bound against none", &ir.Constraints{MaxItems: count(5)}, nil, relANarrower},
		{"a tighter upper bound", &ir.Constraints{MaxItems: count(5)}, &ir.Constraints{MaxItems: count(9)}, relANarrower},
		{"a tighter lower bound", &ir.Constraints{MinItems: count(5)}, &ir.Constraints{MinItems: count(1)}, relANarrower},
		{"one number spelled two ways", &ir.Constraints{Min: big("10")}, &ir.Constraints{Min: big("1e1")}, relSame},
		{"a step that is a multiple of the other", &ir.Constraints{MultipleOf: big("6")}, &ir.Constraints{MultipleOf: big("2")}, relANarrower},
		{"two steps neither divides", &ir.Constraints{MultipleOf: big("4")}, &ir.Constraints{MultipleOf: big("6")}, relMeet},
		{"uniqueItems against none", &ir.Constraints{UniqueItems: true}, nil, relANarrower},
		{"each adds a bound", &ir.Constraints{MinItems: count(1)}, &ir.Constraints{MaxItems: count(5)}, relMeet},
		{"one tighter on each keyword", &ir.Constraints{MinItems: count(2), MaxItems: count(9)}, &ir.Constraints{MinItems: count(1), MaxItems: count(5)}, relMeet},
		{"a bound beside an exclusive one", &ir.Constraints{Min: big("5")}, &ir.Constraints{ExclusiveMin: big("5")}, relMeet},
		{"one more keyword and the same bound", &ir.Constraints{Pattern: "^a$", MaxItems: count(5)}, &ir.Constraints{MaxItems: count(5)}, relANarrower},
		{"one pattern against none", &ir.Constraints{Pattern: "^a$"}, nil, relANarrower},
		{"two patterns", &ir.Constraints{Pattern: "^a$"}, &ir.Constraints{Pattern: "^b$"}, relMeet},
		{"two precisions", &ir.Constraints{Precision: count(4)}, &ir.Constraints{Precision: count(5)}, relMeet},
		{"a tighter bound against another pattern", &ir.Constraints{Pattern: "^a$", MaxLength: count(1)}, &ir.Constraints{Pattern: "^b$", MaxLength: count(9)}, relMeet},
		{"two literals with no order", &ir.Constraints{Min: big("nan")}, &ir.Constraints{Min: big("other")}, relMeet},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, boundsRelation(tc.a, tc.b))
			assert.Equal(t, tc.want.flipped(), boundsRelation(tc.b, tc.a), "from the other side")
		})
	}
}

// TestSameBounds_NothingSaysNothingOnEitherSide pins the states that mean no bound
// was written, on both sides, since a node may hold nil or an empty set, and a
// bound on either side tells them from it.
func TestSameBounds_NothingSaysNothingOnEitherSide(t *testing.T) {
	t.Parallel()
	bound := &ir.Constraints{MaxItems: new(int64(5))}

	assert.True(t, sameBounds(nil, nil))
	assert.True(t, sameBounds(nil, &ir.Constraints{}))
	assert.True(t, sameBounds(&ir.Constraints{}, nil))
	assert.False(t, sameBounds(nil, bound))
	assert.False(t, sameBounds(bound, nil))
	assert.False(t, sameBounds(&ir.Constraints{UniqueItems: true}, nil))
}

// TestBoundsBeyond_KeepsWhatTheOtherDoesNotStateAtThatValue pins what counts as a
// bound another declaration brought: not one a declaration states itself at the
// value the field keeps, whichever keyword it is, and a looser value it states
// does not excuse it.
func TestBoundsBeyond_KeepsWhatTheOtherDoesNotStateAtThatValue(t *testing.T) {
	t.Parallel()
	own := &ir.Constraints{MaxLength: new(int64(3)), Pattern: "^a$", UniqueItems: true, Min: new(ir.BigVal("1"))}
	field := &ir.Constraints{MaxLength: new(int64(3)), Pattern: "^b$", UniqueItems: true, Min: new(ir.BigVal("1.0")), MinLength: new(int64(2))}
	want := &ir.Constraints{Pattern: "^b$", MinLength: new(int64(2))}

	assert.Equal(t, want, boundsBeyond(field, own))
	assert.Equal(t, int64(3), *field.MaxLength, "neither set is written to")
	assert.Equal(t, &ir.Constraints{MaxLength: new(int64(9)), UniqueItems: true},
		boundsBeyond(&ir.Constraints{MaxLength: new(int64(9)), UniqueItems: true}, &ir.Constraints{MaxLength: new(int64(10))}))
}

// boundedNodeKinds builds a hoisted node of each kind that holds bounds, written
// at the nth declaration, holding c and a detail a mint must carry over.
func boundedNodeKinds() map[string]func(n int, c *ir.Constraints) ir.TypeDef {
	i32 := primRef(ir.PrimInt32)
	site := func(n int) ir.TypeCommon {
		return ir.TypeCommon{
			ID: ir.TypeID(fmt.Sprintf("t/node/%d", n)), Anonymous: true, Provenance: ptrAt(declaredAt(n)),
			Docs: ir.Docs{Description: "detail"},
		}
	}
	return map[string]func(n int, c *ir.Constraints) ir.TypeDef{
		"a scalar": func(n int, c *ir.Constraints) ir.TypeDef {
			return &ir.Scalar{TypeCommon: site(n), Base: &i32, Encoding: &ir.Encoding{Name: "x"}, Constraints: c}
		},
		"a list": func(n int, c *ir.Constraints) ir.TypeDef {
			return &ir.List{TypeCommon: site(n), Elem: i32, Constraints: c}
		},
		"a map": func(n int, c *ir.Constraints) ir.TypeDef {
			return &ir.MapT{TypeCommon: site(n), Value: i32, Constraints: c}
		},
		"a tuple": func(n int, c *ir.Constraints) ir.TypeDef {
			return &ir.Tuple{TypeCommon: site(n), Elems: []ir.TypeRef{i32}, Constraints: c}
		},
	}
}

// nodeDecls registers one node per constraint set, built by node, in reg and
// returns a declaration of each, whose source fails the test if it is kept.
func nodeDecls(t *testing.T, reg map[ir.TypeID]ir.TypeDef, node func(int, *ir.Constraints) ir.TypeDef, sets ...*ir.Constraints) []declaration {
	t.Helper()
	decls := make([]declaration, len(sets))
	for i, c := range sets {
		td := node(i, c)
		reg[td.Common().ID] = td
		decls[i] = nthDecl(i, td.Common().ID)
		decls[i].source = unread(t)
	}
	return decls
}

// TestFold_NodesThatMeetStandOnAMintedNode pins what a field takes where each of
// two nodes adds a bound the other lacks: no declared node holds both, so the
// merge mints one. It holds both bounds in either order, keeps the detail the two
// share, names the field, and leaves the declared nodes as they were written.
// Neither declaration lost anything, so nothing is reported or kept.
func TestFold_NodesThatMeetStandOnAMintedNode(t *testing.T) {
	t.Parallel()
	lower, upper := &ir.Constraints{MinItems: new(int64(1))}, &ir.Constraints{MaxItems: new(int64(5))}
	want := &ir.Constraints{MinItems: new(int64(1)), MaxItems: new(int64(5))}
	for kind, node := range boundedNodeKinds() {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			reg := narrowingRegistry()
			inOrders(t, nodeDecls(t, reg, node, lower, upper), func(t *testing.T, ordered []declaration) {
				t.Helper()
				g, recorded := stubMerger(reg)
				_, field := fieldSpan(ordered)

				got := g.fold(ordered)

				require.Equal(t, composedAt(field), got.Type.Target, "the node takes the composed namespace")
				minted := reg[got.Type.Target]
				assert.Equal(t, node(0, nil).Kind(), minted.Kind())
				assert.Equal(t, want, boundsOf(minted))
				assert.Equal(t, "detail", minted.Common().Docs.Description, "what the two share stays")
				assert.Equal(t, lower, boundsOf(reg["t/node/0"]), "the declared node is as it was written")
				assert.Equal(t, upper, boundsOf(reg["t/node/1"]), "and so is the other")
				assert.Empty(t, *recorded)
				assert.Empty(t, got.Unmodeled)
			})
		})
	}
}

// TestFold_TheBoundsOfEveryDeclarationMeetInEveryOrder pins the set fold over
// nodes. A winner picked by how many declarations it holds can be any of those
// that meet, so what the minted node holds must not follow which: a chain folding
// pairs would keep the third declaration's bound or lose it by its position.
func TestFold_TheBoundsOfEveryDeclarationMeetInEveryOrder(t *testing.T) {
	t.Parallel()
	one, two, five := new(int64(1)), new(int64(2)), new(int64(5))
	for _, tc := range []struct {
		name string
		sets []*ir.Constraints
		want *ir.Constraints
	}{
		{"one bound tightening another", []*ir.Constraints{{MinItems: one}, {MaxItems: five}, {MinItems: two}}, &ir.Constraints{MinItems: two, MaxItems: five}},
		{"a bare node beside two that meet", []*ir.Constraints{nil, {MinItems: one}, {MaxItems: five}}, &ir.Constraints{MinItems: one, MaxItems: five}},
		{"three that each add one", []*ir.Constraints{{MinItems: one}, {MaxItems: five}, {UniqueItems: true}}, &ir.Constraints{MinItems: one, MaxItems: five, UniqueItems: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			node := boundedNodeKinds()["a list"]
			reg := narrowingRegistry()
			inOrders(t, nodeDecls(t, reg, node, tc.sets...), func(t *testing.T, ordered []declaration) {
				t.Helper()
				g, recorded := stubMerger(reg)
				got := g.fold(ordered)

				assert.Equal(t, tc.want, boundsOf(reg[got.Type.Target]))
				assert.Empty(t, *recorded)
				assert.Empty(t, got.Unmodeled)
			})
		})
	}
}

// TestFold_ADroppedDeclarationsBoundsStayOffAMintedNode pins whose bounds a mint
// reads: only the declarations meeting the winner. A list of strings beside lists
// of int32 loses its type, and its bound went with it, since it describes the
// shape that lost.
func TestFold_ADroppedDeclarationsBoundsStayOffAMintedNode(t *testing.T) {
	t.Parallel()
	reg := narrowingRegistry()
	i32 := primRef(ir.PrimInt32)
	decls := nodeDecls(t, reg, func(n int, c *ir.Constraints) ir.TypeDef {
		return &ir.List{ID: ir.TypeID(fmt.Sprintf("t/lists/%d", n)), Anonymous: true, Provenance: ptrAt(declaredAt(n)), Elem: i32, Constraints: c}
	}, &ir.Constraints{MinItems: new(int64(1))}, &ir.Constraints{MaxItems: new(int64(5))})
	reg["t/strings"] = &ir.List{ID: "t/strings", Anonymous: true, Provenance: ptrAt(declaredAt(2)), Elem: primRef(ir.PrimString),
		Constraints: &ir.Constraints{MaxItems: new(int64(2))}}
	dropped := nthDecl(2, "t/strings")
	inOrders(t, append(decls, dropped), func(t *testing.T, ordered []declaration) {
		t.Helper()
		g, recorded := stubMerger(reg)

		got := g.fold(ordered)

		assert.Equal(t, &ir.Constraints{MinItems: new(int64(1)), MaxItems: new(int64(5))}, boundsOf(reg[got.Type.Target]))
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(2)))
		require.Len(t, *recorded, 1, "only the dropped declaration is reported")
		assert.Equal(t, declaredAt(2), (*recorded)[0].Provenance.Pointer)
	})
}

// TestFold_ANodeHoldingEveryBoundIsNotMinted pins that a declared node holding
// what they all say is the field's type, whichever declaration wrote it: minting
// beside it would lose nothing and add a node the source never wrote.
func TestFold_ANodeHoldingEveryBoundIsNotMinted(t *testing.T) {
	t.Parallel()
	one, five, nine := new(int64(1)), new(int64(5)), new(int64(9))
	for _, tc := range []struct {
		name string
		sets []*ir.Constraints
		want ir.TypeID
	}{
		{"a bound against a bare node", []*ir.Constraints{nil, {MaxItems: five}}, "t/node/1"},
		{"a tighter bound", []*ir.Constraints{{MaxItems: nine}, {MaxItems: five}}, "t/node/1"},
		{"a node holding what two others add", []*ir.Constraints{{MinItems: one}, {MaxItems: five}, {MinItems: one, MaxItems: five}}, "t/node/2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := narrowingRegistry()
			decls := nodeDecls(t, reg, boundedNodeKinds()["a list"], tc.sets...)
			nodes := len(reg)
			inOrders(t, decls, func(t *testing.T, ordered []declaration) {
				t.Helper()
				g, recorded := stubMerger(reg)
				got := g.fold(ordered)

				assert.Equal(t, tc.want, got.Type.Target)
				assert.Len(t, reg, nodes, "nothing is minted")
				assert.Empty(t, *recorded)
				assert.Empty(t, got.Unmodeled)
			})
		})
	}
}

// TestFold_NodesDifferingInAKeywordNoOneValueStatesKeepTheWinner pins two nodes
// that cannot meet on one keyword. The first's pattern stands and the other's
// declaration is reported and kept, as on a property, and one warning stands for
// a keyword a Scalar's position writes on both its node and its property.
func TestFold_NodesDifferingInAKeywordNoOneValueStatesKeepTheWinner(t *testing.T) {
	t.Parallel()
	node := boundedNodeKinds()["a scalar"]
	mirrored := func(reg map[ir.TypeID]ir.TypeDef, sets ...*ir.Constraints) []declaration {
		decls := nodeDecls(t, reg, node, sets...)
		for i := range decls {
			decls[i].prop.Constraints = sets[i]
			decls[i].source = written(`{"kept":true}`)
		}
		return decls
	}

	t.Run("only the pattern differs", func(t *testing.T) {
		t.Parallel()
		reg := narrowingRegistry()
		decls := mirrored(reg, &ir.Constraints{Pattern: "^a$"}, &ir.Constraints{Pattern: "^b$"})
		nodes := len(reg)
		g, recorded := stubMerger(reg)

		got := g.fold(decls)

		assert.Equal(t, decls[0].prop.Type.Target, got.Type.Target, "the first stands")
		assert.Len(t, reg, nodes, "and nothing is minted for it")
		require.Len(t, *recorded, 1, "the keyword is reported once")
		assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(1)))
	})

	t.Run("one more declaration sets it on the property alone", func(t *testing.T) {
		t.Parallel()
		reg := narrowingRegistry()
		nodes := mirrored(reg, &ir.Constraints{Pattern: "^a$"}, &ir.Constraints{Pattern: "^b$"})
		bare := nthDecl(2, primRef(ir.PrimInt32).Target)
		bare.prop.Constraints = &ir.Constraints{Pattern: "^a$"}
		g, recorded := stubMerger(reg)

		g.fold([]declaration{bare, nodes[0], nodes[1]})

		require.Len(t, *recorded, 1, "the node and the property each meet the one disagreement, whoever each kept it from")
		assert.Equal(t, declaredAt(1), (*recorded)[0].Provenance.Pointer)
	})

	t.Run("a bound meets beside it", func(t *testing.T) {
		t.Parallel()
		reg := narrowingRegistry()
		decls := mirrored(reg,
			&ir.Constraints{Pattern: "^a$", MaxLength: new(int64(9))},
			&ir.Constraints{Pattern: "^b$", MaxLength: new(int64(3))})
		g, recorded := stubMerger(reg)

		got := g.fold(decls)

		assert.Equal(t, &ir.Constraints{Pattern: "^a$", MaxLength: new(int64(3))}, boundsOf(reg[got.Type.Target]))
		assert.Equal(t, &ir.Constraints{Pattern: "^a$", MaxLength: new(int64(3))}, got.Constraints)
		require.Len(t, *recorded, 1, "the keyword is reported once")
		assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(1)))
	})
}

// TestFold_AValueIsHeldToTheBoundsOthersBring pins which bounds judge a value a
// declaration wrote. One another declaration brought does, and a value it refuses
// stays off the field, reported and kept at the declaration, in both orders. One
// the declaration states itself at the value the field keeps does not: that
// disagreement is the declaration's own and stays where it stood. A type's kind
// is never asked here, since both declarations have the field's type.
func TestFold_AValueIsHeldToTheBoundsOthersBring(t *testing.T) {
	t.Parallel()
	big := func(v string) *ir.BigVal { return new(ir.BigVal(v)) }
	count := func(n int64) *int64 { return new(n) }
	hello := str("hello")
	for _, tc := range []struct {
		name         string
		kind         ir.PrimKind
		own, another *ir.Constraints
		value        ir.Value
		taken        bool
	}{
		{name: "a string past another's maxLength", kind: ir.PrimString, another: &ir.Constraints{MaxLength: count(3)}, value: hello},
		{name: "a string within another's maxLength", kind: ir.PrimString, another: &ir.Constraints{MaxLength: count(5)}, value: hello, taken: true},
		{name: "a string below another's minLength", kind: ir.PrimString, another: &ir.Constraints{MinLength: count(6)}, value: hello},
		{name: "a string past a bound tighter than its own", kind: ir.PrimString, own: &ir.Constraints{MaxLength: count(9)}, another: &ir.Constraints{MaxLength: count(3)}, value: hello},
		{name: "a string past its own bound alone", kind: ir.PrimString, own: &ir.Constraints{MaxLength: count(3)}, value: hello, taken: true},
		{name: "a string past its own bound, which another restates", kind: ir.PrimString, own: &ir.Constraints{MaxLength: count(3)}, another: &ir.Constraints{MaxLength: count(3)}, value: hello, taken: true},
		{name: "a string past its own bound, another's being looser", kind: ir.PrimString, own: &ir.Constraints{MaxLength: count(3)}, another: &ir.Constraints{MaxLength: count(9)}, value: hello, taken: true},
		{name: "a string under another's pattern, which nothing evaluates", kind: ir.PrimString, another: &ir.Constraints{Pattern: "^h"}, value: hello},
		{name: "a string under its own pattern", kind: ir.PrimString, own: &ir.Constraints{Pattern: "^x"}, value: hello, taken: true},
		{name: "a number below another's minimum", kind: ir.PrimNumber, another: &ir.Constraints{Min: big("5")}, value: num("1")},
		{name: "a number within another's minimum", kind: ir.PrimNumber, another: &ir.Constraints{Min: big("5")}, value: num("9"), taken: true},
		{name: "a number at another's exclusive bound", kind: ir.PrimNumber, another: &ir.Constraints{ExclusiveMax: big("5")}, value: num("5")},
		{name: "a number off the step the two make", kind: ir.PrimNumber, own: &ir.Constraints{MultipleOf: big("4")}, another: &ir.Constraints{MultipleOf: big("6")}, value: num("8")},
		{name: "a number on the step the two make", kind: ir.PrimNumber, own: &ir.Constraints{MultipleOf: big("4")}, another: &ir.Constraints{MultipleOf: big("6")}, value: num("24"), taken: true},
		{name: "a bound for another kind of value", kind: ir.PrimNumber, another: &ir.Constraints{MaxLength: count(1)}, value: num("123"), taken: true},
		{name: "a list repeating under another's uniqueItems", kind: ir.PrimAny, another: &ir.Constraints{UniqueItems: true}, value: ir.Value{Kind: ir.ValueList, List: []ir.Value{hello, hello}}},
		{name: "a list repeating under its own uniqueItems", kind: ir.PrimAny, own: &ir.Constraints{UniqueItems: true}, another: &ir.Constraints{UniqueItems: true}, value: ir.Value{Kind: ir.ValueList, List: []ir.Value{hello, hello}}, taken: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const written0 = `{"written":"value"}`
			withValue, other := nthDecl(0, primRef(tc.kind).Target), nthDecl(1, primRef(tc.kind).Target)
			withValue.prop.Constraints, withValue.prop.Default, withValue.source = tc.own, &tc.value, written(written0)
			other.prop.Constraints = tc.another
			for _, order := range [][]declaration{{withValue, other}, {other, withValue}} {
				g, recorded := stubMerger(narrowingRegistry())

				got := g.fold(order)

				if tc.taken {
					assert.Equal(t, &tc.value, got.Default)
					assert.Empty(t, *recorded)
					assert.Empty(t, got.Unmodeled)
					continue
				}
				assert.Nil(t, got.Default, "the field does not take it")
				require.Len(t, *recorded, 1)
				assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
				assert.Equal(t, withValue.prop.Provenance.Pointer, (*recorded)[0].Provenance.Pointer)
				assert.Equal(t, ir.RawValue(written0), got.Unmodeled[losingDeclarationKey+string(declaredAt(0))].Value)
			}
		})
	}
}

// TestFold_AnExampleIsHeldToTheBoundsOthersBringAsAWhole pins that the examples of
// a declaration are judged together, as a default is judged alone, so one a bound
// refuses keeps the list off the field.
func TestFold_AnExampleIsHeldToTheBoundsOthersBringAsAWhole(t *testing.T) {
	t.Parallel()
	fits, past := str("abc"), str("hello")
	withExamples, bound := nthDecl(0, primRef(ir.PrimString).Target), nthDecl(1, primRef(ir.PrimString).Target)
	withExamples.prop.Examples = []ir.Example{{Value: &fits}, {Value: &past}}
	bound.prop.Constraints = &ir.Constraints{MaxLength: new(int64(3))}
	g, recorded := stubMerger(narrowingRegistry())

	got := g.fold([]declaration{withExamples, bound})

	assert.Empty(t, got.Examples)
	require.Len(t, *recorded, 1)
	assert.Contains(t, (*recorded)[0].Message, "admits the examples")
}

// TestFold_ANodesBoundsJudgeAValueWrittenAgainstItsOwnKind pins what a declaration
// of a node alike the field's answers for. Its type is the field's but for bounds,
// so an encoding, which admits no value, refuses nothing it wrote; a bound another
// declaration brought to the node does, whether the field took a declared node or
// a minted one.
func TestFold_ANodesBoundsJudgeAValueWrittenAgainstItsOwnKind(t *testing.T) {
	t.Parallel()
	node := boundedNodeKinds()["a scalar"]
	short, long := str("abc"), str("abcdefgh")
	maxFive := &ir.Constraints{MaxLength: new(int64(5))}
	for _, tc := range []struct {
		name  string
		own   *ir.Constraints
		value ir.Value
		taken bool
	}{
		{"on a declared node, within its bound", nil, short, true},
		{"on a declared node, past its bound", nil, long, false},
		{"on a minted node, within both bounds", &ir.Constraints{MinLength: new(int64(1))}, short, true},
		{"on a minted node, past the other's bound", &ir.Constraints{MinLength: new(int64(1))}, long, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := narrowingRegistry()
			decls := nodeDecls(t, reg, node, tc.own, maxFive)
			decls[0].prop.Default, decls[0].source = &tc.value, written(`{"written":"value"}`)
			inOrders(t, decls, func(t *testing.T, ordered []declaration) {
				t.Helper()
				g, recorded := stubMerger(reg)

				got := g.fold(ordered)

				if tc.taken {
					assert.Equal(t, &tc.value, got.Default)
					assert.Empty(t, *recorded)
					return
				}
				assert.Nil(t, got.Default)
				require.Len(t, *recorded, 1)
				assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
			})
		})
	}
}

// TestFold_AValueOfABroaderMemberIsJudgedByTheField pins where "the field's type
// but for bounds" stops. A list of integers is no such type beside a list of
// int32, which is narrower by its element, so what it wrote is judged by the field
// and not left to its declaration.
func TestFold_AValueOfABroaderMemberIsJudgedByTheField(t *testing.T) {
	t.Parallel()
	fraction := ir.Value{Kind: ir.ValueList, List: []ir.Value{num("1.5")}}
	broad, narrow := nthDecl(0, "t/list-int"), nthDecl(1, "t/list-i32")
	broad.prop.Default = &fraction
	g, recorded := stubMerger(judgeRegistry())

	got := g.fold([]declaration{broad, narrow})

	assert.Equal(t, ir.TypeID("t/list-i32"), got.Type.Target)
	assert.Nil(t, got.Default)
	require.Len(t, *recorded, 1)
	assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
}

// TestTypeJudge_ATupleOfTuplesCostsItsWidthNotItsWidthToTheDepth pins the bound on
// the one node with a member per position. Each level's positions name one tuple
// of the next, so a judgment reading them all would read width^depth nodes from
// a document as small as width times depth, as $refs to a sibling position write.
// A tuple another type holds is not compared, so this reads a handful.
func TestTypeJudge_ATupleOfTuplesCostsItsWidthNotItsWidthToTheDepth(t *testing.T) {
	t.Parallel()
	const width, depth = 4, maxJudgeDepth
	reg := map[ir.TypeID]ir.TypeDef{}
	for _, side := range []string{"a", "b"} {
		for level := range depth {
			next := ir.TypeRef{Target: ir.TypeID(fmt.Sprintf("t/%s/%d", side, level+1))}
			id := ir.TypeID(fmt.Sprintf("t/%s/%d", side, level))
			reg[id] = &ir.Tuple{ID: id, Anonymous: true, Provenance: ptrAt(jsontext.Pointer("/" + side)), Elems: slices.Repeat([]ir.TypeRef{next}, width)}
		}
		leaf := ir.TypeID(fmt.Sprintf("t/%s/%d", side, depth))
		reg[leaf] = &ir.Primitive{ID: leaf, Prim: ir.PrimString}
	}
	reads := 0
	j := typeJudge{
		Resolve: func(id ir.TypeID) (ir.TypeDef, bool) {
			reads++
			td, ok := reg[id]
			return td, ok
		},
		written: [2]ir.Provenance{ptrAt("/a"), ptrAt("/b")},
	}

	rel := j.relate(ref("t/a/0"), ref("t/b/0"))

	assert.Equal(t, relUnrelated, rel, "a tuple holding tuples is compared by no position")
	assert.LessOrEqual(t, reads, 2*width*depth, "the reads grow with the document, not with its width to the depth")
}

// TestTypeJudge_ATuplesPositionsCompareOnlyAtAFieldsOwnDepth pins the two sides of
// that bound: a field's own tuples relate by their positions, and the same pair
// held by a list does not.
func TestTypeJudge_ATuplesPositionsCompareOnlyAtAFieldsOwnDepth(t *testing.T) {
	t.Parallel()
	reg := judgeRegistry()
	reg["t/held-min"] = &ir.List{ID: "t/held-min", Elem: ref("t/tuple-min")}
	reg["t/held-bare"] = &ir.List{ID: "t/held-bare", Elem: ref("t/tuple")}
	j := typeJudge{Resolve: stubResolve(reg)}

	assert.Equal(t, relANarrower, j.relate(ref("t/tuple-min"), ref("t/tuple")), "as a field's own type")
	assert.Equal(t, relUnrelated, j.relate(ref("t/held-min"), ref("t/held-bare")), "as what a list holds")
}

// TestFold_ATypeTheRegistryDoesNotHoldHoldsNoBounds pins the guard behind a
// dangling reference: it says nothing of its bounds, and a value written against
// it is its declaration's own, as on any type the fold keeps.
func TestFold_ATypeTheRegistryDoesNotHoldHoldsNoBounds(t *testing.T) {
	t.Parallel()
	value := str("hello")
	first, second := nthDecl(0, "t/absent"), nthDecl(1, "t/absent")
	first.prop.Default = &value
	g, recorded := stubMerger(narrowingRegistry())

	got := g.fold([]declaration{first, second})

	assert.Equal(t, &value, got.Default)
	assert.Equal(t, ir.TypeID("t/absent"), got.Type.Target)
	assert.Empty(t, *recorded)
}

// TestWithBounds_ReadsOnlyTheKindsThatHoldThem pins the pair a mint rests on: a
// kind boundsOf does not read has no copy to make, so a caller holding a model
// reads no bounds off it and gets it back unchanged.
func TestWithBounds_ReadsOnlyTheKindsThatHoldThem(t *testing.T) {
	t.Parallel()
	model := &ir.Model{ID: "t/m", Constraints: &ir.Constraints{MinProps: new(int64(1))}}

	assert.Nil(t, boundsOf(model))
	assert.Same(t, model, withBounds(model, &ir.Constraints{}))
}
