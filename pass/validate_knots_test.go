// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

const (
	cycleCode  = "pass/composition-cycle"
	finiteCode = "pass/no-finite-value"
)

// ref is a reference to the type id, not nullable.
func ref(id string) ir.TypeRef { return ir.TypeRef{Target: ir.TypeID(id)} }

// maybe is a reference to the type id that admits null as well.
func maybe(id string) ir.TypeRef { return ir.TypeRef{Target: ir.TypeID(id), Nullable: true} }

// composed is the model id with the base and the properties given.
func composed(id string, base *ir.TypeRef, props ...ir.Property) *ir.Model {
	return &ir.Model{ID: ir.TypeID(id), Base: base, Properties: props, Provenance: provAt(id)}
}

// needs is the property of the ID id of the type typ, required.
func needs(id string, typ ir.TypeRef) ir.Property {
	return prop(id, typ, func(p *ir.Property) { p.Required = true })
}

// knotDoc is valueDoc holding defs.
func knotDoc(defs ...ir.TypeDef) *ir.Document {
	doc := valueDoc()
	for _, td := range defs {
		doc.Types[td.Common().ID] = td
	}
	return doc
}

// TestValidate_ACompositionCycleIsReportedAtEachTypeOfIt holds a cycle of bases
// to a warning at each type in it, naming the base that leads back, and a type
// that only leads into one to none: the IR permits the cycle and a target
// reads it to its fixpoint, so it is a SHOULD NOT, never an error. A type with
// a cycle in its composition is no type with no finite value for that: a cycle
// of bases draws the one code.
func TestValidate_ACompositionCycleIsReportedAtEachTypeOfIt(t *testing.T) {
	t.Parallel()
	cyclic := func(id string) *ir.TypeRef { r := ref(id); return &r }
	doc := knotDoc(
		composed("t/a", cyclic("t/b")), composed("t/b", cyclic("t/a")), composed("t/c", cyclic("t/c")), composed("t/d", cyclic("t/a")),
	)

	assert.Equal(t, []finding{
		{"t/a", "composition never ends: base t/b leads back into a cycle of bases and mixins"},
		{"t/b", "composition never ends: base t/a leads back into a cycle of bases and mixins"},
		{"t/c", "composition never ends: base t/c leads back into a cycle of bases and mixins"},
	}, found(t, doc, cycleCode))
	assert.Empty(t, found(t, doc, finiteCode), "a cycle of bases is told once, as a cycle")

	chain := knotDoc(composed("t/a", cyclic("t/b")), composed("t/b", nil), composed("t/d", cyclic("t/a")))
	assert.Empty(t, found(t, chain, cycleCode), "a chain of bases that ends")
}

// TestValidate_ACycleThroughAMixinNamesTheMixin holds the entry a finding names
// to the first of the base and the mixins whose target is in the cycle.
func TestValidate_ACycleThroughAMixinNamesTheMixin(t *testing.T) {
	t.Parallel()
	a := composed("t/a", nil)
	a.Mixins = []ir.TypeRef{ref("t/ok"), ref("t/b")}
	b := composed("t/b", nil)
	b.Mixins = []ir.TypeRef{ref("t/a")}

	got := found(t, knotDoc(a, b, composed("t/ok", nil)), cycleCode)

	assert.Equal(t, []finding{
		{"t/a", "composition never ends: mixin 1 t/b leads back into a cycle of bases and mixins"},
		{"t/b", "composition never ends: mixin 0 t/a leads back into a cycle of bases and mixins"},
	}, got)
}

// TestValidate_ATypeWithNoFiniteValueIsReportedAtEachTypeOfTheKnot holds a type
// that needs itself, through a required property, an element a list must hold,
// a value a map must hold, a position a tuple must fill, one of a union's
// variants when it has no other, or a base, to a warning at each type of the knot; and
// the nearest type that is founded to none: a property not required, a
// reference that admits null, a list that may be empty, a union with another
// variant, a type that only needs one with no finite value.
func TestValidate_ATypeWithNoFiniteValueIsReportedAtEachTypeOfTheKnot(t *testing.T) {
	t.Parallel()
	const none = ", which has none either"
	list := func(id, elem string, minItems *int64) *ir.List {
		return &ir.List{ID: ir.TypeID(id), Elem: ref(elem), Constraints: &ir.Constraints{MinItems: minItems}, Provenance: provAt(id)}
	}
	tuple := func(id string, minItems *int64, rest *ir.TypeRef, elems ...ir.TypeRef) *ir.Tuple {
		tp := &ir.Tuple{ID: ir.TypeID(id), Elems: elems, Rest: rest, Provenance: provAt(id)}
		if minItems != nil {
			tp.Constraints = &ir.Constraints{MinItems: minItems}
		}
		return tp
	}
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		want []finding
	}{
		{"a required property of its own type, a mutual pair, and a type needing one", knotDoc(
			composed("t/n", nil, needs("p/n/0", ref("t/n"))),
			composed("t/x", nil, needs("p/x/0", ref("t/y"))), composed("t/y", nil, needs("p/y/0", ref("t/x"))),
			composed("t/h", nil, needs("p/h/0", ref("t/n"))),
		), []finding{
			{"t/n", "no finite value: each needs its required property 0, t/n" + none},
			{"t/x", "no finite value: each needs its required property 0, t/y" + none},
			{"t/y", "no finite value: each needs its required property 0, t/x" + none},
		}},
		{"the same properties, not required", knotDoc(
			composed("t/n", nil, prop("p/n/0", ref("t/n"), nil)),
			composed("t/x", nil, prop("p/x/0", ref("t/y"), nil)), composed("t/y", nil, needs("p/y/0", ref("t/x"))),
		), nil},
		{"the same properties, one admitting null", knotDoc(
			composed("t/x", nil, needs("p/x/0", ref("t/y"))), composed("t/y", nil, needs("p/y/0", maybe("t/x"))),
		), nil},
		{"a list that must hold an element that needs the list", knotDoc(
			list("t/k", "t/t", new(int64(1))), composed("t/t", nil, needs("p/t/0", ref("t/k"))),
		), []finding{
			{"t/k", "no finite value: each needs its element, t/t" + none},
			{"t/t", "no finite value: each needs its required property 0, t/k" + none},
		}},
		{"the same list, which may be empty", knotDoc(
			list("t/k", "t/t", nil), composed("t/t", nil, needs("p/t/0", ref("t/k"))),
		), nil},
		{"the same list, with a minimum of none", knotDoc(
			list("t/k", "t/t", new(int64(0))), composed("t/t", nil, needs("p/t/0", ref("t/k"))),
		), nil},
		{"a map that must hold a value of its own type", knotDoc(
			&ir.MapT{ID: "t/mp", Key: new(strT), Value: ref("t/mp"), Constraints: &ir.Constraints{MinProps: new(int64(1))}, Provenance: provAt("t/mp")},
		), []finding{{"t/mp", "no finite value: each needs its value, t/mp" + none}}},
		{"a tuple that must hold itself", knotDoc(
			tuple("t/p", new(int64(2)), nil, ref("t/prim/string"), ref("t/p")),
		), []finding{{"t/p", "no finite value: each needs its element 1, t/p" + none}}},
		{"a tuple that must hold itself past its leading positions", knotDoc(
			tuple("t/p", new(int64(2)), new(ref("t/p")), ref("t/prim/string")),
		), []finding{{"t/p", "no finite value: each needs its element 1, t/p" + none}}},
		{"the same tuple, which need not hold that position", knotDoc(
			tuple("t/p", new(int64(1)), new(ref("t/p")), ref("t/prim/string"), ref("t/p")),
		), nil},
		{"the same tuple, with no minimum", knotDoc(
			tuple("t/p", nil, nil, ref("t/prim/string"), ref("t/p")),
		), nil},
		{"a tuple whose element admits null", knotDoc(
			tuple("t/p", new(int64(1)), nil, maybe("t/p")),
		), nil},
		{"a union whose one variant is the union", knotDoc(
			&ir.Union{ID: "t/u", Variants: []ir.Variant{{Type: ref("t/u")}}, Provenance: provAt("t/u")},
		), []finding{{"t/u", "no finite value: each needs its variant 0, t/u" + none}}},
		{"a union with a second variant that is a string", knotDoc(
			&ir.Union{ID: "t/u", Variants: []ir.Variant{{Type: ref("t/u")}, {Type: strT}}, Provenance: provAt("t/u")},
		), nil},
		{"a union whose variant admits null", knotDoc(
			&ir.Union{ID: "t/u", Variants: []ir.Variant{{Type: maybe("t/u")}}, Provenance: provAt("t/u")},
		), nil},
		{"a base that needs the type", knotDoc(
			composed("t/a", new(ref("t/b"))), composed("t/b", nil, needs("p/b/0", ref("t/a"))),
		), []finding{
			{"t/a", "no finite value: each needs its base, t/b" + none},
			{"t/b", "no finite value: each needs its required property 0, t/a" + none},
		}},
		{"a scalar alias of itself", knotDoc(
			&ir.Scalar{ID: "t/s", Base: new(ref("t/s")), Provenance: provAt("t/s")},
		), []finding{{"t/s", "no finite value: each needs its base, t/s" + none}}},
		{"a scalar alias admitting null of itself", knotDoc(
			&ir.Scalar{ID: "t/s", Base: new(maybe("t/s")), Provenance: provAt("t/s")},
		), nil},
		{"a type holding itself in a property that is not required", knotDoc(
			composed("t/node", nil, prop("p/node/next", ref("t/node"), nil)),
		), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, finiteCode))
		})
	}
}

// TestValidate_ABaseThatNeedsItsDerivedTypeIsAFiniteValueAlone holds a base
// whose target requires the type to be reported as a value that cannot be
// built, and not as a cycle of composition: the base is no cycle, and the
// type of the one reads a base whatever the other needs.
func TestValidate_ABaseThatNeedsItsDerivedTypeIsAFiniteValueAlone(t *testing.T) {
	t.Parallel()
	doc := knotDoc(composed("t/a", new(ref("t/b"))), composed("t/b", nil, needs("p/b/0", ref("t/a"))))

	assert.Empty(t, found(t, doc, cycleCode))
	assert.Len(t, found(t, doc, finiteCode), 2)
}

// TestValidate_TheKnotsAreReportedInIDOrderCompositionFirst holds the findings
// of one document to the compositions' first, then the values', each in ID
// order, the types declared in the reverse.
func TestValidate_TheKnotsAreReportedInIDOrderCompositionFirst(t *testing.T) {
	t.Parallel()
	doc := knotDoc(
		composed("t/z", nil, needs("p/z/0", ref("t/z"))), composed("t/y", new(ref("t/y"))),
		composed("t/b", nil, needs("p/b/0", ref("t/b"))), composed("t/a", new(ref("t/a"))),
	)

	got := onlyKnots(validated(t, doc))

	assert.Equal(t, []string{"t/a", "t/y", "t/b", "t/z"}, pointers(got))
	assert.Equal(t, []string{cycleCode, cycleCode, finiteCode, finiteCode}, irtest.DiagCodes(got))
}

// onlyKnots returns the findings of the two knot codes, in the order drawn.
func onlyKnots(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, d := range diags {
		if d.Code == cycleCode || d.Code == finiteCode {
			out = append(out, d)
		}
	}
	return out
}
