// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package ir_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// unionOf is the untagged union id of the variants targets.
func unionOf(id ir.TypeID, targets ...ir.TypeID) *ir.Union {
	u := &ir.Union{ID: id}
	for _, target := range targets {
		u.Variants = append(u.Variants, ir.Variant{Type: ir.TypeRef{Target: target}})
	}
	return u
}

// shapeLines renders each shape of n as its Over, then its flattening's
// members as flatLines renders them, its undeclared names and its unflattened
// entries.
func shapeLines(n ir.Narrowing) []string {
	out := make([]string, 0, len(n.Shapes))
	for _, s := range n.Shapes {
		out = append(out, fmt.Sprintf("over %v: %v undeclared %v unflattened %v",
			s.Over, flatLines(s.Flattening), s.Undeclared, s.Unflattened))
	}
	return out
}

// narrowed returns NarrowedShapes over types and root with no limit.
func narrowed(t *testing.T, types ir.TypeRegistry, root ir.TypeID) ir.Narrowing {
	t.Helper()
	n, _, ok := ir.NarrowedShapes(types, root, math.MaxInt)
	require.True(t, ok, "an unlimited walk always finishes")
	return n
}

// ruleDoc is docs/ir/models.md's example: Stored composes the union Rule of
// Block and Log, and declares id.
func ruleDoc() *ir.Document {
	return irtest.DocOf(
		unionOf("rule", "block", "log"),
		owning("block", nil, mandatory("block.action", "action"), wired("block.status", "status")),
		owning("log", nil, mandatory("log.action", "action")),
		owning("stored", func(m *ir.Model) {
			m.Base = &ir.TypeRef{Target: "rule"}
			m.Mixins = irtest.RefsTo("own")
		}),
		owning("own", nil, mandatory("own.id", "id")),
	)
}

func TestNarrowedShapes_ReadsAVariantInTheUnionsPlace(t *testing.T) {
	t.Parallel()
	doc := ruleDoc()
	require.Equal(t, []ir.TypeID{"rule"}, ir.FlattenedProperties(doc.Types, "stored").Unflattened,
		"the model's own flattening holds the union, none of its members")

	n := narrowed(t, doc.Types, "stored")
	assert.Equal(t, ir.TypeID("rule"), n.Union)
	assert.Empty(t, n.Undistributed)
	assert.Equal(t, []string{
		"over [block]: [block.action@block required block.status@block own.id@own required] undeclared [] unflattened []",
		"over [log]: [log.action@log required own.id@own required] undeclared [] unflattened []",
	}, shapeLines(n), "each variant is read where the union stands, laid out as the conjunct it replaces")
	assert.Empty(t, cmp.Diff(n, narrowed(t, doc.Types, "stored")), "a second call agrees")
}

func TestNarrowedShapes_AVariantIsAConjunctLikeAnyOther(t *testing.T) {
	t.Parallel()
	deprecated := wired("v.tag", "tag")
	deprecated.Deprecation = &ir.Deprecation{Message: "old"}
	doc := irtest.DocOf(
		unionOf("u", "v", "w"),
		owning("v", func(m *ir.Model) { m.Requires = []string{"note"} },
			mandatory("v.id", "id"), redacted("v.token", "token"), deprecated),
		owning("w", nil, wired("w.id", "id")),
		owning("m", based("u"), wired("m.id", "id"), wired("m.token", "token"), wired("m.tag", "tag"),
			wired("m.note", "note")),
	)
	n := narrowed(t, doc.Types, "m")
	require.Len(t, n.Shapes, 2)
	assert.Equal(t, []string{"m.id@m < v.id@v required", "m.token@m < v.token@v secret",
		"m.tag@m < v.tag@v deprecated old", "m.note@m required"}, flatLines(n.Shapes[0].Flattening),
		"the model's declarations override the variant's, which still requires, redacts, deprecates and lists Requires")
	assert.Same(t, deprecated.Deprecation, n.Shapes[0].Properties[2].Deprecation, "the deprecation is the variant's own")
	assert.Equal(t, []string{"m.id@m < w.id@w", "m.token@m", "m.tag@m", "m.note@m"},
		flatLines(n.Shapes[1].Flattening), "a member only one variant redacts is redacted over it alone")
	assert.Nil(t, n.Shapes[1].Properties[2].Deprecation)
}

func TestNarrowedShapes_ReadsTheUnionWhereverItStands(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		doc  *ir.Document
		want []string
	}{
		{
			name: "through an alias scalar, a variant named through one too",
			doc: irtest.DocOf(
				irtest.ScalarOf("alias", "u"), unionOf("u", "va"), irtest.ScalarOf("va", "v"),
				owning("v", nil, wired("v.a", "a")),
				owning("m", based("alias"), wired("m.b", "b")),
			),
			want: []string{"over [va]: [v.a@v m.b@m] undeclared [] unflattened []"},
		},
		{
			name: "through a model the root composes",
			doc: irtest.DocOf(
				unionOf("u", "v"), owning("v", nil, wired("v.a", "a")),
				owning("x", based("u"), wired("x.b", "b")),
				owning("m", based("x"), wired("m.c", "c")),
			),
			want: []string{"over [v]: [v.a@v x.b@x m.c@m] undeclared [] unflattened []"},
		},
		{
			name: "at each entry naming it, the variant read once",
			doc: irtest.DocOf(
				unionOf("u", "v"), owning("v", nil, wired("v.a", "a")),
				owning("x", mixing("u"), wired("x.b", "b")),
				owning("m", mixing("u", "x"), wired("m.a", "a")),
			),
			want: []string{"over [v]: [m.a@m < v.a@v x.b@x] undeclared [] unflattened []"},
		},
		{
			name: "inside a variant composing it, which reads itself there",
			doc: irtest.DocOf(
				unionOf("u", "v", "w"),
				owning("v", mixing("u"), wired("v.a", "a")),
				owning("w", nil, wired("w.b", "b")),
				owning("m", mixing("u"), wired("m.c", "c")),
			),
			want: []string{
				"over [v]: [v.a@v m.c@m] undeclared [] unflattened []",
				"over [w]: [w.b@w m.c@m] undeclared [] unflattened []",
			},
		},
		{
			name: "a variant naming no model listed as the union writes it, an unset one read in no shape",
			doc: irtest.DocOf(
				unionOf("u", "uuid", "", "v"), irtest.ScalarOf("uuid", "string"),
				&ir.Primitive{ID: "string", Prim: ir.PrimString},
				owning("v", mixing("u"), wired("v.a", "a")),
				owning("m", mixing("u", "enum"), wired("m.c", "c")),
				&ir.Enum{ID: "enum"},
			),
			want: []string{
				"over [uuid]: [m.c@m] undeclared [] unflattened [uuid enum]",
				"over [v]: [v.a@v m.c@m] undeclared [] unflattened [enum]",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := narrowed(t, tc.doc.Types, "m")
			assert.Empty(t, n.Undistributed)
			assert.Equal(t, tc.want, shapeLines(n))
		})
	}
}

func TestNarrowedShapes_UnionsInTurn(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		unionOf("outer", "inner", "narrower", "leaf"),
		unionOf("inner", "i1", "i2"),
		owning("narrower", mixing("deep"), wired("n.a", "a")),
		unionOf("deep", "d1", "d2"),
		owning("i1", nil, wired("i1.a", "a")), owning("i2", nil, wired("i2.a", "a")),
		owning("d1", nil, wired("d1.b", "b")), owning("d2", nil, wired("d2.b", "b")),
		owning("leaf", nil, wired("leaf.a", "a")),
		owning("m", based("outer"), wired("m.c", "c")),
	)
	n := narrowed(t, doc.Types, "m")
	assert.Equal(t, ir.TypeID("outer"), n.Union)
	assert.Equal(t, []string{
		"over [inner i1]: [i1.a@i1 m.c@m] undeclared [] unflattened []",
		"over [inner i2]: [i2.a@i2 m.c@m] undeclared [] unflattened []",
		"over [narrower d1]: [d1.b@d1 n.a@narrower m.c@m] undeclared [] unflattened []",
		"over [narrower d2]: [d2.b@d2 n.a@narrower m.c@m] undeclared [] unflattened []",
		"over [leaf]: [leaf.a@leaf m.c@m] undeclared [] unflattened []",
	}, shapeLines(n), "a variant that is a union or composes one gives way to each of that union's variants, "+
		"in source order and depth first")
}

func TestNarrowedShapes_NarrowsNone(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		unionOf("shape", "circle", "alias"),
		owning("circle", based("shape"), wired("circle.r", "r")),
		irtest.ScalarOf("alias", "square"),
		owning("square", based("shape"), wired("square.s", "s")),
		owning("plain", nil, wired("plain.a", "a")),
		irtest.ScalarOf("toCircle", "circle"),
		&ir.Enum{ID: "enum"},
	)
	doc.Types["nil"] = (*ir.Model)(nil)
	for _, root := range []ir.TypeID{"circle", "square", "toCircle", "plain", "shape", "enum", "nil", "ghost", ""} {
		n, spent, ok := ir.NarrowedShapes(doc.Types, root, math.MaxInt)
		require.True(t, ok)
		assert.Equal(t, ir.Narrowing{}, n, "root %q: a model its union lists is that variant, and nothing else narrows", root)
		assert.Positive(t, spent, "root %q: finding so is a walk", root)
	}
	n, _, ok := ir.NarrowedShapes(nil, "m", math.MaxInt)
	assert.True(t, ok)
	assert.Equal(t, ir.Narrowing{}, n, "no registry narrows nothing")
}

func TestNarrowedShapes_NoVariantGivesNoShape(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(unionOf("u"), owning("m", based("u"), wired("m.a", "a")))
	assert.Equal(t, ir.Narrowing{Union: "u"}, narrowed(t, doc.Types, "m"))
}

func TestNarrowedShapes_Undistributed(t *testing.T) {
	t.Parallel()
	tagged := func(id ir.TypeID, d *ir.Discriminator) *ir.Union {
		u := unionOf(id, "v")
		u.WireTagged, u.Discriminator = true, d
		return u
	}
	index := 0
	cases := []struct {
		name  string
		doc   *ir.Document
		union ir.TypeID
		want  ir.Undistributed
	}{
		{
			name: "two unions the model composes",
			doc:  irtest.DocOf(unionOf("u", "v"), unionOf("x", "v"), owning("v", nil), owning("m", mixing("u", "x"))),
			want: ir.UndistributedSeveral,
		},
		{
			name: "two unions a variant composes",
			doc: irtest.DocOf(unionOf("u", "v"), owning("v", mixing("x", "y")), unionOf("x", "z"), unionOf("y", "z"),
				owning("z", nil), owning("m", based("u"))),
			union: "u",
			want:  ir.UndistributedSeveral,
		},
		{
			name:  "a variant composing the model",
			doc:   irtest.DocOf(unionOf("u", "w", "v"), owning("v", based("m")), owning("w", nil), owning("m", based("u"))),
			union: "u",
			want:  ir.UndistributedUnending,
		},
		{
			name: "a variant composing the model through another",
			doc: irtest.DocOf(unionOf("u", "v"), owning("v", based("n")), owning("n", mixing("m")),
				owning("m", based("u"))),
			union: "u",
			want:  ir.UndistributedUnending,
		},
		{
			name:  "a union that is its own variant",
			doc:   irtest.DocOf(unionOf("u", "u"), owning("m", based("u"))),
			union: "u",
			want:  ir.UndistributedUnending,
		},
		{
			name:  "a union reaching itself through the unions among its variants",
			doc:   irtest.DocOf(unionOf("u", "x"), unionOf("x", "v", "alias"), irtest.ScalarOf("alias", "u"), owning("v", nil), owning("m", based("u"))),
			union: "u",
			want:  ir.UndistributedUnending,
		},
		{
			name:  "a key-tagged union",
			doc:   irtest.DocOf(tagged("u", nil), owning("v", nil), owning("m", based("u"))),
			union: "u",
			want:  ir.UndistributedNotValues,
		},
		{
			name: "an enveloped union",
			doc: irtest.DocOf(tagged("u", &ir.Discriminator{PropertyName: "kind", Envelope: "object"}),
				owning("v", nil), owning("m", based("u"))),
			union: "u",
			want:  ir.UndistributedNotValues,
		},
		{
			name: "a union tagged by a tuple element, met in turn",
			doc: irtest.DocOf(unionOf("u", "w"), owning("w", based("x")),
				tagged("x", &ir.Discriminator{Index: &index}), owning("v", nil), owning("m", based("u"))),
			union: "u",
			want:  ir.UndistributedNotValues,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, ir.Narrowing{Union: tc.union, Undistributed: tc.want}, narrowed(t, tc.doc.Types, "m"))
		})
	}
}

// TestNarrowedShapes_DistributesWhatACycleLeavesOut holds the cycles no
// distribution needs ended to their shapes: a cycle of models the variant
// takes no part in, a variant reaching its own union again through a model of
// its own, and one composing a model the root composes, which narrows the
// union first.
func TestNarrowedShapes_DistributesWhatACycleLeavesOut(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		unionOf("u", "v"),
		owning("v", based("y"), wired("v.a", "a")),
		owning("y", based("u"), wired("y.b", "b")),
		owning("m", mixing("u", "n"), wired("m.c", "c")),
		owning("n", based("m"), wired("n.d", "d")),
		unionOf("w", "z"),
		owning("z", based("x"), wired("z.a", "a")),
		owning("x", based("w"), wired("x.b", "b")),
		owning("r", based("x"), wired("r.c", "c")),
	)
	assert.Equal(t, []string{"over [v]: [y.b@y v.a@v n.d@n m.c@m] undeclared [] unflattened []"},
		shapeLines(narrowed(t, doc.Types, "m")))
	assert.Equal(t, []string{"over [z]: [z.a@z x.b@x r.c@r] undeclared [] unflattened []"},
		shapeLines(narrowed(t, doc.Types, "r")))
	assert.Equal(t, ir.Narrowing{Union: "w", Undistributed: ir.UndistributedUnending}, narrowed(t, doc.Types, "x"),
		"the model whose entry the union fills is the one its variant composes")
}

// TestNarrowedShapes_StopsPastItsLimit holds the limit to the steps the call
// spends: given them all it answers and spends them, and given one fewer it
// stops without an answer, wherever that step falls, in the walk finding the
// union, in a shape's or on a shape itself.
func TestNarrowedShapes_StopsPastItsLimit(t *testing.T) {
	t.Parallel()
	doc := ruleDoc()
	plain := irtest.DocOf(owning("m", nil, wired("m.a", "a")))
	for name, types := range map[string]ir.TypeRegistry{"narrowing": doc.Types, "plain": plain.Types} {
		root := ir.TypeID("stored")
		if name == "plain" {
			root = "m"
		}
		want, need, ok := ir.NarrowedShapes(types, root, math.MaxInt)
		require.True(t, ok)
		got, spent, ok := ir.NarrowedShapes(types, root, need)
		require.True(t, ok, "%s: the steps it needs are enough", name)
		assert.Equal(t, need, spent)
		assert.Empty(t, cmp.Diff(want, got))
		for limit := range need {
			got, spent, ok = ir.NarrowedShapes(types, root, limit)
			assert.False(t, ok, "%s: %d steps of %d are too few", name, limit, need)
			assert.Greater(t, spent, limit, "%s: it spent past the limit when it stopped", name)
			assert.Equal(t, ir.Narrowing{}, got, "%s: and answers nothing", name)
		}
	}
}

// TestNarrowedShapes_StepsCountWhatEachWalkReads holds the steps to what the
// walks read beside a flattening, FlattenSteps counting each entry and each
// type read through alias scalars: each variant read in a union's place, each
// time the walk meets the union, and each variant of a union a walk tells
// whether it lists the model.
func TestNarrowedShapes_StepsCountWhatEachWalkReads(t *testing.T) {
	t.Parallel()
	spent := func(doc *ir.Document) int {
		_, s, ok := ir.NarrowedShapes(doc.Types, "m", math.MaxInt)
		require.True(t, ok)
		return s
	}
	once := irtest.DocOf(unionOf("u", "v"), owning("v", nil), owning("m", mixing("u", "x")), owning("x", nil))
	twice := irtest.DocOf(unionOf("u", "v"), owning("v", nil), owning("m", mixing("u", "x")), owning("x", mixing("u")))
	assert.Equal(t, spent(once)+2+3, spent(twice),
		"meeting the union again is an entry and the union read, in the walk finding it, "+
			"and those and the variant read in the shape's, resolved at the first meeting alone")

	narrow := irtest.DocOf(unionOf("u", "v"), owning("v", nil), owning("m", based("u")))
	copied := irtest.DocOf(owning("v", nil), owning("m", based("v")))
	listing := 2 // a variant read and resolved to tell whether the union lists the model
	inPlace := 2 // the variant read in the union's place, and resolved
	assert.Equal(t, ir.FlattenSteps(narrow.Types, "m")+listing+ir.FlattenSteps(copied.Types, "m")+inPlace+1, spent(narrow),
		"the walk finding the union, the shape's walk, which reads the variant where the union stands, and the shape")

	wide := irtest.DocOf(unionOf("u", "v", "w"), owning("v", nil), owning("w", nil), owning("m", based("u")))
	shape := spent(narrow) - ir.FlattenSteps(narrow.Types, "m") - listing
	assert.Equal(t, spent(narrow)+listing+shape, spent(wide),
		"a variant more is one more read to find the union, and one more shape, its walk and itself")

	listsFirst := irtest.DocOf(unionOf("u", "m"), owning("m", based("u")))
	listsFirstOfMore := irtest.DocOf(unionOf("u", "m", "v", "w"), owning("v", nil), owning("w", nil), owning("m", based("u")))
	assert.Equal(t, spent(listsFirst), spent(listsFirstOfMore),
		"a union reads no variant past the one standing for the model")

	selfOnly := irtest.DocOf(unionOf("u", "u"), owning("m", based("u")))
	selfAndMore := irtest.DocOf(unionOf("u", "u", "v", "w"), owning("v", nil), owning("w", nil), owning("m", based("u")))
	assert.Equal(t, spent(selfOnly)+2*listing, spent(selfAndMore),
		"a shape whose chain reaches its union twice ends there, reading the union it ends at no further")
	selfTwice := irtest.DocOf(unionOf("u", "u"), owning("m", mixing("u", "u")))
	selfOnce := irtest.DocOf(unionOf("u", "u"), owning("m", mixing("u")))
	assert.Equal(t, spent(selfOnce)+2+2, spent(selfTwice),
		"once a chain reaches its union twice the walk substitutes nothing more, so meeting the union again "+
			"is an entry and the union read, in the walk finding it and in the shape's")
}

// TestNarrowedShapes_CountsWhatNestedUnionsMultiply holds the steps to what
// the shapes cost where nested unions multiply them: unions nested n deep, each
// beside one leaf, give n shapes whose Over lists hold n(n+1)/2 variants, all
// of them read, so a limit refusing that refuses the input. A union with n
// variants of one member each, and one n models of the composition name, by
// contrast, cost linear steps.
func TestNarrowedShapes_CountsWhatNestedUnionsMultiply(t *testing.T) {
	t.Parallel()
	nested := func(n int) ir.TypeRegistry {
		doc := irtest.DocOf(owning("m", based(unionAt(0)), wired("m.a", "a")))
		for i := range n {
			leaf := ir.TypeID(fmt.Sprintf("leaf%04d", i))
			doc.Types[leaf] = owning(leaf, nil, wired(ir.PropID(leaf), "b"))
			if i == n-1 {
				doc.Types[unionAt(i)] = unionOf(unionAt(i), leaf)
				continue
			}
			doc.Types[unionAt(i)] = unionOf(unionAt(i), leaf, unionAt(i+1))
		}
		return doc.Types
	}
	for _, n := range []int{10, 100} {
		got, spent, ok := ir.NarrowedShapes(nested(n), "m", math.MaxInt)
		require.True(t, ok)
		require.Len(t, got.Shapes, n)
		over := 0
		for _, s := range got.Shapes {
			over += len(s.Over)
		}
		assert.Equal(t, n*(n+1)/2, over)
		assert.Greater(t, spent, over, "n=%d: every variant read is a step", n)
	}
	_, _, ok := ir.NarrowedShapes(nested(1000), "m", 100_000)
	assert.False(t, ok, "unions nested 1,000 deep pass a limit their entries and members alone stay under")

	wide := func(n int) ir.TypeRegistry {
		u := unionOf("u")
		doc := irtest.DocOf(u, owning("m", based("u"), wired("m.a", "a")))
		for i := range n {
			v := ir.TypeID(fmt.Sprintf("v%04d", i))
			doc.Types[v] = owning(v, nil, wired(ir.PropID(v), "b"))
			u.Variants = append(u.Variants, ir.Variant{Type: ir.TypeRef{Target: v}})
		}
		return doc.Types
	}
	fanIn := func(n int) ir.TypeRegistry {
		doc := irtest.DocOf(unionOf("u", "v", "w"), owning("v", nil, wired("v.a", "a")),
			owning("w", nil, wired("w.a", "a")))
		names := make([]ir.TypeID, n)
		for i := range names {
			names[i] = ir.TypeID(fmt.Sprintf("x%04d", i))
			doc.Types[names[i]] = owning(names[i], based("u"), wired(ir.PropID(names[i]), "b"))
		}
		doc.Types["m"] = owning("m", mixing(names...))
		return doc.Types
	}
	for name, build := range map[string]func(int) ir.TypeRegistry{"variants": wide, "fan-in": fanIn} {
		var steps [3]int
		for k, n := range []int{100, 200, 400} {
			_, steps[k], ok = ir.NarrowedShapes(build(n), "m", math.MaxInt)
			require.True(t, ok)
		}
		assert.LessOrEqual(t, steps[2]-steps[1], 2*(steps[1]-steps[0])+8,
			"%s: steps around 100, 200, 400 grow linearly: %v", name, steps)
	}
}

// TestNarrowedShapes_ReadsAChainOncePerWalk holds an entry standing for a union
// to the same cost however long the chain of variants read in its place: a
// walk reads a chain once, as it reads an alias chain, so a model at the end
// of a deep chain that names its first union, and one halfway along, many
// times costs no product of the two.
func TestNarrowedShapes_ReadsAChainOncePerWalk(t *testing.T) {
	t.Parallel()
	chained := func(deep, entries int) ir.TypeRegistry {
		names := make([]ir.TypeID, entries)
		for i := range names {
			names[i] = unionAt(i % 2 * deep / 2)
		}
		doc := irtest.DocOf(owning("m", based(unionAt(0)), wired("m.a", "a")),
			owning("v", mixing(names...), wired("v.b", "b")), unionOf(unionAt(deep-1), "v"))
		for i := range deep - 1 {
			doc.Types[unionAt(i)] = unionOf(unionAt(i), unionAt(i+1))
		}
		return doc.Types
	}
	perEntries := func(deep int) int {
		few, spentFew, ok := ir.NarrowedShapes(chained(deep, 100), "m", math.MaxInt)
		require.True(t, ok)
		require.Len(t, few.Shapes, 1, "deep %d: the chain ends at the one model", deep)
		require.Len(t, few.Shapes[0].Over, deep)
		_, spentMore, ok := ir.NarrowedShapes(chained(deep, 200), "m", math.MaxInt)
		require.True(t, ok)
		return spentMore - spentFew
	}
	assert.Equal(t, perEntries(2), perEntries(50),
		"a hundred entries more cost the same at the end of a chain 2 and 50 unions deep")
}

// unionAt is the ID of the i-th union of a nest.
func unionAt(i int) ir.TypeID { return ir.TypeID(fmt.Sprintf("u%04d", i)) }

// ExampleNarrowedShapes lays out the shapes a stored rule takes: the rule
// union of a block and a log, with the stored model's id beside each.
func ExampleNarrowedShapes() {
	reg := ir.TypeRegistry{
		"t/rule": &ir.Union{ID: "t/rule", Variants: []ir.Variant{
			{Type: ir.TypeRef{Target: "t/block"}}, {Type: ir.TypeRef{Target: "t/log"}},
		}},
		"t/block": &ir.Model{ID: "t/block", Properties: []ir.Property{
			{ID: "p/block/action", WireName: "action", Required: true},
			{ID: "p/block/status", WireName: "status"},
		}},
		"t/log": &ir.Model{ID: "t/log", Properties: []ir.Property{{ID: "p/log/action", WireName: "action", Required: true}}},
		"t/stored": &ir.Model{ID: "t/stored", Base: &ir.TypeRef{Target: "t/rule"}, Properties: []ir.Property{
			{ID: "p/stored/id", WireName: "id", Required: true},
		}},
	}

	n, _, _ := ir.NarrowedShapes(reg, "t/stored", 10_000)
	fmt.Println("narrows", n.Union)
	for _, s := range n.Shapes {
		fmt.Print("over ", s.Over, ":")
		for _, fp := range s.Properties {
			fmt.Printf(" %s from %s", fp.Property.EffectiveWireName(), fp.Owner)
		}
		fmt.Println()
	}
	// Output:
	// narrows t/rule
	// over [t/block]: action from t/block status from t/block id from t/stored
	// over [t/log]: action from t/log id from t/stored
}
