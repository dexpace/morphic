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
	return prop(id, typ, func(property *ir.Property) { property.Required = true })
}

// noFiniteValueDoc is valueDoc holding defs.
func noFiniteValueDoc(defs ...ir.TypeDef) *ir.Document {
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
	cyclic := func(id string) *ir.TypeRef { typeRef := ref(id); return &typeRef }
	doc := noFiniteValueDoc(
		composed("type/a", cyclic("type/b")), composed("type/b", cyclic("type/a")), composed("type/c", cyclic("type/c")), composed("type/d", cyclic("type/a")),
	)

	assert.Equal(t, []finding{
		{"type/a", "composition never ends: base type/b leads back into a cycle of bases and mixins"},
		{"type/b", "composition never ends: base type/a leads back into a cycle of bases and mixins"},
		{"type/c", "composition never ends: base type/c leads back into a cycle of bases and mixins"},
	}, found(t, doc, cycleCode))
	assert.Empty(t, found(t, doc, finiteCode), "a cycle of bases is told once, as a cycle")

	chain := noFiniteValueDoc(composed("type/a", cyclic("type/b")), composed("type/b", nil), composed("type/d", cyclic("type/a")))
	assert.Empty(t, found(t, chain, cycleCode), "a chain of bases that ends")
}

// TestValidate_ACycleThroughAMixinNamesTheMixin holds the entry a finding names
// to the first of the base and the mixins whose target is in the cycle.
func TestValidate_ACycleThroughAMixinNamesTheMixin(t *testing.T) {
	t.Parallel()
	a := composed("type/a", nil)
	a.Mixins = []ir.TypeRef{ref("type/ok"), ref("type/b")}
	b := composed("type/b", nil)
	b.Mixins = []ir.TypeRef{ref("type/a")}

	got := found(t, noFiniteValueDoc(a, b, composed("type/ok", nil)), cycleCode)

	assert.Equal(t, []finding{
		{"type/a", "composition never ends: mixin 1 type/b leads back into a cycle of bases and mixins"},
		{"type/b", "composition never ends: mixin 0 type/a leads back into a cycle of bases and mixins"},
	}, got)
}

// TestValidate_ATypeWithNoFiniteValueIsReportedAtEachTypeOfTheTypeCycle holds a type
// that needs itself, through a required property, an element a list must hold,
// a value a map must hold, a position a tuple must fill, a union's only
// variant, or a base, to a warning at each type of the type cycle, naming what leads
// on, never a base of a cycle told already; and the nearest founded type to
// none: a property not required, a
// reference that admits null, a list that may be empty, a union with another
// variant, a type that only needs one with no finite value.
func TestValidate_ATypeWithNoFiniteValueIsReportedAtEachTypeOfTheTypeCycle(t *testing.T) {
	t.Parallel()
	const none = ", which has none either"
	list := func(id, elem string, minItems *int64) *ir.List {
		return &ir.List{ID: ir.TypeID(id), ElementType: ref(elem), Constraints: &ir.Constraints{MinItems: minItems}, Provenance: provAt(id)}
	}
	tuple := func(id string, minItems *int64, rest *ir.TypeRef, elems ...ir.TypeRef) *ir.Tuple {
		tp := &ir.Tuple{ID: ir.TypeID(id), ElementTypes: elems, Rest: rest, Provenance: provAt(id)}
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
		{"a required property of its own type, a mutual pair, and a type needing one", noFiniteValueDoc(
			composed("type/n", nil, needs("property/n/0", ref("type/n"))),
			composed("type/x", nil, needs("property/x/0", ref("type/y"))), composed("type/y", nil, needs("property/y/0", ref("type/x"))),
			composed("type/h", nil, needs("property/h/0", ref("type/n"))),
		), []finding{
			{"type/n", "no finite value: each needs its required property 0, type/n" + none},
			{"type/x", "no finite value: each needs its required property 0, type/y" + none},
			{"type/y", "no finite value: each needs its required property 0, type/x" + none},
		}},
		{"the same properties, not required", noFiniteValueDoc(
			composed("type/n", nil, prop("property/n/0", ref("type/n"), nil)),
			composed("type/x", nil, prop("property/x/0", ref("type/y"), nil)), composed("type/y", nil, needs("property/y/0", ref("type/x"))),
		), nil},
		{"the same properties, one admitting null", noFiniteValueDoc(
			composed("type/x", nil, needs("property/x/0", ref("type/y"))), composed("type/y", nil, needs("property/y/0", maybe("type/x"))),
		), nil},
		{"a list that must hold an element that needs the list", noFiniteValueDoc(
			list("type/k", "type/t", new(int64(1))), composed("type/t", nil, needs("property/t/0", ref("type/k"))),
		), []finding{
			{"type/k", "no finite value: each needs its element, type/t" + none},
			{"type/t", "no finite value: each needs its required property 0, type/k" + none},
		}},
		{"the same list, which may be empty", noFiniteValueDoc(
			list("type/k", "type/t", nil), composed("type/t", nil, needs("property/t/0", ref("type/k"))),
		), nil},
		{"the same list, with a minimum of none", noFiniteValueDoc(
			list("type/k", "type/t", new(int64(0))), composed("type/t", nil, needs("property/t/0", ref("type/k"))),
		), nil},
		{"a map that must hold a value of its own type", noFiniteValueDoc(
			&ir.MapT{ID: "type/mp", Key: new(strT), Value: ref("type/mp"), Constraints: &ir.Constraints{MinProperties: new(int64(1))}, Provenance: provAt("type/mp")},
		), []finding{{"type/mp", "no finite value: each needs its value, type/mp" + none}}},
		{"a tuple that must hold itself", noFiniteValueDoc(
			tuple("type/p", new(int64(2)), nil, ref("type/primitive/string"), ref("type/p")),
		), []finding{{"type/p", "no finite value: each needs its element 1, type/p" + none}}},
		{"a tuple that must hold itself past its leading positions", noFiniteValueDoc(
			tuple("type/p", new(int64(2)), new(ref("type/p")), ref("type/primitive/string")),
		), []finding{{"type/p", "no finite value: each needs its element 1, type/p" + none}}},
		{"the same tuple, which need not hold that position", noFiniteValueDoc(
			tuple("type/p", new(int64(1)), new(ref("type/p")), ref("type/primitive/string"), ref("type/p")),
		), nil},
		{"the same tuple, with no minimum", noFiniteValueDoc(
			tuple("type/p", nil, nil, ref("type/primitive/string"), ref("type/p")),
		), nil},
		{"a tuple whose element admits null", noFiniteValueDoc(
			tuple("type/p", new(int64(1)), nil, maybe("type/p")),
		), nil},
		{"a union whose one variant is the union", noFiniteValueDoc(
			&ir.Union{ID: "type/u", Variants: []ir.Variant{{Type: ref("type/u")}}, Provenance: provAt("type/u")},
		), []finding{{"type/u", "no finite value: each needs its variant 0, type/u" + none}}},
		{"a union with a second variant that is a string", noFiniteValueDoc(
			&ir.Union{ID: "type/u", Variants: []ir.Variant{{Type: ref("type/u")}, {Type: strT}}, Provenance: provAt("type/u")},
		), nil},
		{"a union whose variant admits null", noFiniteValueDoc(
			&ir.Union{ID: "type/u", Variants: []ir.Variant{{Type: maybe("type/u")}}, Provenance: provAt("type/u")},
		), nil},
		{"a base that needs the type", noFiniteValueDoc(
			composed("type/a", new(ref("type/b"))), composed("type/b", nil, needs("property/b/0", ref("type/a"))),
		), []finding{
			{"type/a", "no finite value: each needs its base, type/b" + none},
			{"type/b", "no finite value: each needs its required property 0, type/a" + none},
		}},
		{"a cycle of bases whose types each require the other", noFiniteValueDoc(
			composed("type/a", new(ref("type/b")), needs("property/a/0", ref("type/b"))), composed("type/b", new(ref("type/a")), needs("property/b/0", ref("type/a"))),
		), []finding{
			{"type/a", "no finite value: each needs its required property 0, type/b" + none},
			{"type/b", "no finite value: each needs its required property 0, type/a" + none},
		}},
		{"a scalar alias of itself", noFiniteValueDoc(
			&ir.Scalar{ID: "type/s", Base: new(ref("type/s")), Provenance: provAt("type/s")},
		), []finding{{"type/s", "no finite value: each needs its base, type/s" + none}}},
		{"a scalar alias admitting null of itself", noFiniteValueDoc(
			&ir.Scalar{ID: "type/s", Base: new(maybe("type/s")), Provenance: provAt("type/s")},
		), nil},
		{"a type holding itself in a property that is not required", noFiniteValueDoc(
			composed("type/node", nil, prop("property/node/next", ref("type/node"), nil)),
		), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, found(t, tc.doc, finiteCode))
		})
	}
}

// TestValidate_ABaseThatNeedsItsDerivedTypeIsToldAsNoFiniteValueAlone holds a
// base whose target requires the type to be reported as a value that cannot be
// built, and not as a cycle of composition: the base is no cycle, and the
// type of the one reads a base whatever the other needs.
func TestValidate_ABaseThatNeedsItsDerivedTypeIsToldAsNoFiniteValueAlone(t *testing.T) {
	t.Parallel()
	doc := noFiniteValueDoc(composed("type/a", new(ref("type/b"))), composed("type/b", nil, needs("property/b/0", ref("type/a"))))

	assert.Empty(t, found(t, doc, cycleCode))
	assert.Len(t, found(t, doc, finiteCode), 2)
}

// TestValidate_TheTypeCyclesAreReportedInIDOrderCompositionFirst holds the findings
// of one document to the compositions' first, then the values', each in ID
// order, the types declared in the reverse.
func TestValidate_TheTypeCyclesAreReportedInIDOrderCompositionFirst(t *testing.T) {
	t.Parallel()
	doc := noFiniteValueDoc(
		composed("type/z", nil, needs("property/z/0", ref("type/z"))), composed("type/y", new(ref("type/y"))),
		composed("type/b", nil, needs("property/b/0", ref("type/b"))), composed("type/a", new(ref("type/a"))),
	)

	got := onlyNoFiniteValueFindings(validated(t, doc))

	assert.Equal(t, []string{"type/a", "type/y", "type/b", "type/z"}, pointers(got))
	assert.Equal(t, []string{cycleCode, cycleCode, finiteCode, finiteCode}, irtest.DiagCodes(got))
}

// onlyNoFiniteValueFindings returns the findings of the two type cycle codes, in the order drawn.
func onlyNoFiniteValueFindings(diags []ir.Diagnostic) []ir.Diagnostic {
	var out []ir.Diagnostic
	for _, diagnostic := range diags {
		if diagnostic.Code == cycleCode || diagnostic.Code == finiteCode {
			out = append(out, diagnostic)
		}
	}
	return out
}
