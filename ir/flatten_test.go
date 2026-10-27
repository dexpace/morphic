package ir_test

import (
	"fmt"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// wired is a property named id on the wire as wire.
func wired(id ir.PropID, wire string) ir.Property { return ir.Property{ID: id, WireName: wire} }

// mandatory is wired, Required.
func mandatory(id ir.PropID, wire string) ir.Property {
	p := wired(id, wire)
	p.Required = true
	return p
}

// owning builds the model id declaring props, shaped further by mutate.
func owning(id ir.TypeID, mutate func(*ir.Model), props ...ir.Property) *ir.Model {
	return irtest.ModelOf(id, func(m *ir.Model) {
		m.Properties = props
		if mutate != nil {
			mutate(m)
		}
	})
}

// based sets a model's Base to target.
func based(target ir.TypeID) func(*ir.Model) {
	return func(m *ir.Model) { m.Base = &ir.TypeRef{Target: target} }
}

// mixing sets a model's Mixins to targets.
func mixing(targets ...ir.TypeID) func(*ir.Model) {
	return func(m *ir.Model) { m.Mixins = irtest.RefsTo(targets...) }
}

// TestFlattenedProperties_ReadsEveryCompositionField holds the composition
// entries the flattening follows, Base, then Implements, then Mixins, to the
// fields of ir.Model that hold a TypeRef, read off its declaration. A field
// added beside them is an entry FlattenedProperties, ExposedProps, Supertypes
// and the irfacts indexes must read too, or is none: the failure asks which.
func TestFlattenedProperties_ReadsEveryCompositionField(t *testing.T) {
	t.Parallel()
	model, err := irDecls(t).Named("Model")
	require.NoError(t, err)
	st, ok := model.Underlying().(*types.Struct)
	require.True(t, ok, "ir.Model is a struct")
	var refs []string
	for field := range st.Fields() {
		if holdsTypeRef(field.Type()) {
			refs = append(refs, field.Name())
		}
	}
	assert.Equal(t, []string{"Base", "Implements", "Mixins"}, refs,
		"a Model field holding a TypeRef is a composition entry every composition walk reads, or it is not one")
}

// holdsTypeRef reports whether typ is ir.TypeRef, a pointer to one or a slice
// of them.
func holdsTypeRef(typ types.Type) bool {
	switch u := typ.(type) {
	case *types.Pointer:
		typ = u.Elem()
	case *types.Slice:
		typ = u.Elem()
	default:
		// A TypeRef held directly, or a type holding none.
	}
	named, ok := types.Unalias(typ).(*types.Named)
	return ok && named.Obj().Name() == "TypeRef" && named.Obj().Pkg().Path() == "github.com/dexpace/morphic/ir"
}

func TestFlattenedProperties_LayoutOrder(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		owning("m", func(m *ir.Model) {
			m.Base = &ir.TypeRef{Target: "b"}
			m.Implements = irtest.RefsTo("i")
			m.Mixins = irtest.RefsTo("x", "y")
		}, wired("m.1", "m1"), wired("m.2", "m2")),
		owning("b", based("bb"), wired("b.1", "b1")),
		owning("bb", nil, wired("bb.1", "bb1")),
		owning("i", nil, wired("i.1", "i1")),
		owning("x", nil, wired("x.1", "x1")),
		owning("y", nil, wired("y.1", "y1")),
	)
	assert.Equal(t, []string{"bb.1@bb", "b.1@b", "i.1@i", "x.1@x", "y.1@y", "m.1@m", "m.2@m"},
		flatLines(ir.FlattenedProperties(doc.Types, "m")),
		"what a model composes comes first, depth first, Base then Implements then Mixins, its own last")
}

func TestFlattenedProperties_Override(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		doc  *ir.Document
		want []string
	}{
		{
			name: "an own declaration overrides an inherited one in its place",
			doc: irtest.DocOf(
				owning("m", based("b"), wired("m.name", "name")),
				owning("b", nil, wired("b.id", "id"), wired("b.name", "name"), wired("b.at", "at")),
			),
			want: []string{"b.id@b", "m.name@m < b.name@b", "b.at@b"},
		},
		{
			name: "a nearer ancestor overrides a farther one",
			doc: irtest.DocOf(
				owning("m", based("b")),
				owning("b", based("a"), wired("b.x", "x")),
				owning("a", nil, wired("a.x", "x")),
			),
			want: []string{"b.x@b < a.x@a"},
		},
		{
			name: "a later entry naming an ancestor of an earlier one overrides nothing",
			doc: irtest.DocOf(
				owning("m", mixing("b", "a")),
				owning("b", based("a"), wired("b.x", "x")),
				owning("a", nil, wired("a.x", "x")),
			),
			want: []string{"b.x@b < a.x@a"},
		},
		{
			name: "a mixin composing the base overrides the base",
			doc: irtest.DocOf(
				owning("m", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "a"}; m.Mixins = irtest.RefsTo("b") }),
				owning("b", based("a"), wired("b.x", "x")),
				owning("a", nil, wired("a.x", "x")),
			),
			want: []string{"b.x@b < a.x@a"},
		},
		{
			name: "of two unrelated declarations the later conjunct wins",
			doc: irtest.DocOf(
				owning("m", func(m *ir.Model) {
					m.Base = &ir.TypeRef{Target: "a"}
					m.Implements = irtest.RefsTo("i")
					m.Mixins = irtest.RefsTo("b", "c")
				}),
				owning("a", nil, wired("a.x", "x")),
				owning("i", nil, wired("i.x", "x")),
				owning("b", nil, wired("b.x", "x")),
				owning("c", nil, wired("c.x", "x")),
			),
			want: []string{"c.x@c < a.x@a < i.x@i < b.x@b"},
		},
		{
			name: "a mixin overrides what the base inherits from farther up",
			doc: irtest.DocOf(
				owning("m", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "a"}; m.Mixins = irtest.RefsTo("b") }),
				owning("a", based("z"), wired("a.x", "x")),
				owning("z", nil, wired("z.x", "x"), wired("z.y", "y")),
				owning("b", nil, wired("b.y", "y")),
			),
			want: []string{"a.x@a < z.x@z", "b.y@b < z.y@z"},
		},
		{
			name: "a wire name is the source name where no WireName is set",
			doc: irtest.DocOf(
				owning("m", based("b"), ir.Property{ID: "m.id", Name: ir.Naming{Source: "id"}}),
				owning("b", nil, wired("b.id", "id"), ir.Property{ID: "b.ident", Name: ir.Naming{Source: "id"}, WireName: "ident"}),
			),
			want: []string{"m.id@m < b.id@b", "b.ident@b"},
		},
		{
			name: "a per-format name or an alias neither joins members nor splits one",
			doc: irtest.DocOf(
				owning("m", based("b"),
					ir.Property{ID: "m.x", WireName: "x", WireNameByFormat: map[string]string{"application/xml": "Ex"}},
					wired("m.y", "y")),
				owning("b", nil, ir.Property{
					ID: "b.x", WireName: "x", Name: ir.Naming{Aliases: []string{"y"}},
					WireNameByFormat: map[string]string{"application/xml": "X"},
				}),
			),
			want: []string{"m.x@m < b.x@b", "m.y@m"},
		},
		{
			name: "the empty wire name is a name like any other",
			doc: irtest.DocOf(
				owning("m", based("b"), wired("m.empty", "")),
				owning("b", nil, wired("b.empty", "")),
			),
			want: []string{"m.empty@m < b.empty@b"},
		},
		{
			name: "own declarations sharing a wire name are one member",
			doc:  irtest.DocOf(owning("m", nil, wired("m.a", "x"), wired("m.b", "y"), wired("m.c", "x"))),
			want: []string{"m.c@m < m.a@m", "m.b@m"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, flatLines(ir.FlattenedProperties(tc.doc.Types, "m")))
		})
	}
}

func TestFlattenedProperties_APropertySeveralModelsDeclareIsOneDeclaration(t *testing.T) {
	t.Parallel()
	shared := wired("p/shared", "x")
	doc := irtest.DocOf(
		owning("m", mixing("a", "b", "c")),
		owning("a", nil, shared),
		owning("b", nil, wired("b.x", "x")),
		owning("c", nil, shared),
		owning("n", mixing("a", "c"), shared),
		owning("o", mixing("a", "c", "b")),
	)
	assert.Equal(t, []string{"p/shared@c < b.x@b"}, flatLines(ir.FlattenedProperties(doc.Types, "m")),
		"the declaration read last takes the member, and one it shares an ID with is not overridden by it")
	assert.Equal(t, []string{"p/shared@n"}, flatLines(ir.FlattenedProperties(doc.Types, "n")),
		"three readings of one property are one declaration")
	assert.Equal(t, []string{"b.x@b < p/shared@a"}, flatLines(ir.FlattenedProperties(doc.Types, "o")),
		"an overridden declaration read twice is listed once, at its first reading")
}

func TestFlattenedProperties_Required(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		doc        *ir.Document
		want       []string
		undeclared []string
	}{
		{
			name: "a member is required where its declaration says so",
			doc:  irtest.DocOf(owning("m", nil, mandatory("m.a", "a"), wired("m.b", "b"))),
			want: []string{"m.a@m required", "m.b@m"},
		},
		{
			name: "a requirement an overridden declaration states still holds",
			doc: irtest.DocOf(
				owning("m", based("b"), wired("m.id", "id")),
				owning("b", nil, mandatory("b.id", "id")),
			),
			want: []string{"m.id@m < b.id@b required"},
		},
		{
			name: "a Requires names an inherited member, or a sibling's",
			doc: irtest.DocOf(
				owning("m", func(m *ir.Model) { m.Mixins = irtest.RefsTo("a", "b"); m.Requires = []string{"a"} }),
				owning("a", func(m *ir.Model) { m.Requires = []string{"b"} }, wired("a.a", "a")),
				owning("b", nil, wired("b.b", "b"), wired("b.c", "c")),
			),
			want: []string{"a.a@a required", "b.b@b required", "b.c@b"},
		},
		{
			name: "a Requires naming no member is undeclared, once, in layout order",
			doc: irtest.DocOf(
				owning("m", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "b"}; m.Requires = []string{"z", "y", "k"} }),
				owning("b", func(m *ir.Model) { m.Requires = []string{"y", "k", "y"} }, wired("b.k", "k")),
			),
			want:       []string{"b.k@b required"},
			undeclared: []string{"y", "z"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := ir.FlattenedProperties(tc.doc.Types, "m")
			assert.Equal(t, tc.want, flatLines(f))
			assert.Equal(t, tc.undeclared, f.Undeclared)
		})
	}
}

func TestFlattenedProperties_ReadsThroughAliasScalars(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ScalarOf("root", "m"),
		owning("m", based("alias1"), wired("m.a", "a")),
		irtest.ScalarOf("alias1", "alias2"),
		irtest.ScalarOf("alias2", "b"),
		owning("b", mixing("alias2"), wired("b.a", "a"), wired("b.b", "b")),
	)
	f := ir.FlattenedProperties(doc.Types, "root")
	assert.Equal(t, []string{"m.a@m < b.a@b", "b.b@b"}, flatLines(f),
		"a root, a Base and a Mixin naming an alias scalar each read the model it stands for")
	assert.Empty(t, f.Unflattened, "an alias read a second time still stands for its model")
}

func TestFlattenedProperties_Unflattened(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		owning("m", func(m *ir.Model) {
			m.Base = &ir.TypeRef{Target: "uuid"}
			m.Implements = []ir.TypeRef{{}, {Target: "u"}}
			m.Mixins = irtest.RefsTo("x", "enum", "ghost", "nil", "cycle1", "holey", "uuid", "e2")
		}, wired("m.a", "a")),
		irtest.ScalarOf("uuid", "string"),
		&ir.Primitive{ID: "string", Prim: ir.PrimString},
		&ir.Union{ID: "u"},
		owning("x", mixing("deep"), wired("x.a", "a")),
		&ir.MapT{ID: "deep"},
		&ir.Enum{ID: "enum"},
		irtest.ScalarOf("cycle1", "cycle2"),
		irtest.ScalarOf("cycle2", "cycle1"),
		&ir.Scalar{ID: "holey", Base: &ir.TypeRef{}},
		irtest.ScalarOf("e2", "enum"),
	)
	doc.Types["nil"] = (*ir.Model)(nil)
	f := ir.FlattenedProperties(doc.Types, "m")
	assert.Equal(t, []ir.TypeID{"uuid", "u", "deep", "enum", "ghost", "nil", "cycle1", "holey", "e2"}, f.Unflattened,
		"each entry naming no model is listed as written, once, as the walk reads it, however deep")
	assert.Equal(t, []string{"m.a@m < x.a@x"}, flatLines(f), "and the models beside them are flattened")
	assert.Empty(t, f.Undeclared)
}

func TestFlattenedProperties_NoModelHasAnEmptyShape(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ScalarOf("opaque", ""),
		irtest.ScalarOf("toEnum", "enum"),
		&ir.Enum{ID: "enum"},
		irtest.ScalarOf("loop", "loop"),
		&ir.Primitive{ID: "string", Prim: ir.PrimString},
	)
	doc.Types["nil"] = (*ir.Model)(nil)
	for _, root := range []ir.TypeID{"missing", "", "opaque", "toEnum", "enum", "loop", "string", "nil"} {
		assert.Equal(t, ir.Flattening{}, ir.FlattenedProperties(doc.Types, root), "root %q", root)
	}
	assert.Equal(t, ir.Flattening{}, ir.FlattenedProperties(nil, "m"), "no registry declares nothing")
	assert.Equal(t, ir.Flattening{}, ir.FlattenedProperties(irtest.DocOf(owning("m", nil)).Types, "m"),
		"a model declaring nothing and composing nothing has no member")
}

func TestFlattenedProperties_CyclicCompositionIsReadToItsFixpoint(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		owning("a", based("b"), wired("a.p", "p")),
		owning("b", mixing("alias"), wired("b.q", "q"), wired("b.p", "p")),
		irtest.ScalarOf("alias", "a"),
		owning("self", based("self"), wired("self.s", "s")),
	)
	assert.Equal(t, []string{"b.q@b", "a.p@a < b.p@b"}, flatLines(ir.FlattenedProperties(doc.Types, "a")),
		"each model of the cycle holds the other's members, read in the order the walk finished them")
	assert.Equal(t, []string{"b.p@b < a.p@a", "b.q@b"}, flatLines(ir.FlattenedProperties(doc.Types, "b")))
	assert.Equal(t, []string{"self.s@self"}, flatLines(ir.FlattenedProperties(doc.Types, "self")),
		"a model composing itself reads itself once")
}

// TestFlattenedProperties_ACycleIsReadAfterWhatItComposes holds a model on a
// cycle through the root to overriding a model the cycle composes. The walk
// meets c before x, and c's entry back to the root is one it is already on, so
// c finishes first; read in finishing order, x would override c, though c
// composes x through the root and x composes nothing. The cycle may be longer
// than one entry: in s's, a reaches the root only through b, and waits for it
// as b does.
func TestFlattenedProperties_ACycleIsReadAfterWhatItComposes(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		owning("r", func(m *ir.Model) { m.Implements = irtest.RefsTo("c"); m.Mixins = irtest.RefsTo("x") }),
		owning("c", based("r"), wired("c.k", "k")),
		owning("x", nil, wired("x.k", "k"), wired("x.j", "j")),
		owning("s", func(m *ir.Model) { m.Implements = irtest.RefsTo("a"); m.Mixins = irtest.RefsTo("x") }),
		owning("a", based("b"), wired("a.j", "j")),
		owning("b", based("s"), wired("b.k", "k")),
	)
	assert.Equal(t, []string{"c.k@c < x.k@x", "x.j@x"}, flatLines(ir.FlattenedProperties(doc.Types, "r")))
	assert.Equal(t, []string{"b.k@b < x.k@x", "a.j@a < x.j@x"}, flatLines(ir.FlattenedProperties(doc.Types, "s")))
}

// TestFlattenedProperties_AModelReadEarlierHoldsNothingBack holds a model an
// entry names after the walk has read it to being skipped: it composes no
// model still on the walk's way, so the model naming it is read when it
// finishes, before the conjuncts after it.
func TestFlattenedProperties_AModelReadEarlierHoldsNothingBack(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		owning("r", mixing("a", "b", "c")),
		owning("a", nil, wired("a.j", "j")),
		owning("b", based("a"), wired("b.k", "k")),
		owning("c", nil, wired("c.k", "k")),
	)
	assert.Equal(t, []string{"a.j@a", "c.k@c < b.k@b"}, flatLines(ir.FlattenedProperties(doc.Types, "r")),
		"b is read before c, so the later conjunct's declaration wins")
}

func TestFlattenedProperties_PointsIntoTheRegistry(t *testing.T) {
	t.Parallel()
	b := owning("b", nil, wired("b.x", "x"), wired("b.y", "y"))
	m := owning("m", based("b"), wired("m.x", "x"))
	f := ir.FlattenedProperties(irtest.DocOf(m, b).Types, "m")
	require.Len(t, f.Properties, 2)
	assert.Same(t, &m.Properties[0], f.Properties[0].Property, "the member takes the declaration itself, not a copy")
	require.Len(t, f.Properties[0].Overridden, 1)
	assert.Same(t, &b.Properties[0], f.Properties[0].Overridden[0].Property)
	assert.Same(t, &b.Properties[1], f.Properties[1].Property)
}

// TestFlattenedProperties_WorkGrowsLinearly holds the steps one walk takes
// (FlattenSteps) to linear growth around three sizes on two shapes: an alias
// chain every entry names, which a walk reading the chain per entry makes
// quadratic, and a lattice, which a walk entering a model again per path
// makes exponential.
func TestFlattenedProperties_WorkGrowsLinearly(t *testing.T) {
	t.Parallel()
	shapes := map[string]func(n int) (ir.TypeRegistry, ir.TypeID){
		// n entries each naming the head of one alias chain n long.
		"a chain of aliases named by every entry": func(n int) (ir.TypeRegistry, ir.TypeID) {
			doc := irtest.DocOf(owning("end", nil, wired("end.p", "p")))
			for i := range n {
				doc.Types[ir.TypeID(fmt.Sprintf("s%04d", i))] = irtest.ScalarOf(ir.TypeID(fmt.Sprintf("s%04d", i)),
					ir.TypeID(fmt.Sprintf("s%04d", i+1)))
			}
			doc.Types[ir.TypeID(fmt.Sprintf("s%04d", n))] = irtest.ScalarOf(ir.TypeID(fmt.Sprintf("s%04d", n)), "end")
			heads := make([]ir.TypeID, n)
			for i := range heads {
				heads[i] = "s0000"
			}
			doc.Types["m"] = owning("m", mixing(heads...))
			return doc.Types, "m"
		},
		// n levels of two models, each composing both of the level below.
		"a lattice every path of which reaches the bottom": func(n int) (ir.TypeRegistry, ir.TypeID) {
			doc := irtest.DocOf()
			level := func(i int, side string) ir.TypeID { return ir.TypeID(fmt.Sprintf("l%04d%s", i, side)) }
			for i := range n {
				for _, side := range []string{"a", "b"} {
					doc.Types[level(i, side)] = owning(level(i, side), mixing(level(i+1, "a"), level(i+1, "b")),
						wired(ir.PropID(level(i, side)), "x"))
				}
			}
			doc.Types[level(n, "a")] = owning(level(n, "a"), nil)
			doc.Types[level(n, "b")] = owning(level(n, "b"), nil)
			return doc.Types, level(0, "a")
		},
	}
	for name, build := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var steps [3]int
			for k, n := range []int{100, 200, 400} {
				types, root := build(n)
				steps[k] = ir.FlattenSteps(types, root)
			}
			assert.LessOrEqual(t, steps[2]-steps[1], 2*(steps[1]-steps[0])+8,
				"steps around 100, 200, 400 grow linearly: %v", steps)
		})
	}
}

// ExampleFlattenedProperties lays out an error response composing a success
// envelope and, after it, a failure schema, as an emitter laying out a struct
// reads it: the failure's success overrides the envelope's and stays required,
// the envelope's result is required by the failure's Requires, and a name no
// member declares is left to the catch-all.
func ExampleFlattenedProperties() {
	reg := ir.TypeRegistry{
		"t/envelope": &ir.Model{ID: "t/envelope", Properties: []ir.Property{
			{ID: "p/envelope/success", WireName: "success", Type: ir.TypeRef{Target: "t/true"}, Required: true},
			{ID: "p/envelope/result", WireName: "result", Type: ir.TypeRef{Target: "t/page"}},
		}},
		"t/failure": &ir.Model{ID: "t/failure", Requires: []string{"result", "code"}, Properties: []ir.Property{
			{ID: "p/failure/success", WireName: "success", Type: ir.TypeRef{Target: "t/false"}},
			{ID: "p/failure/errors", WireName: "errors", Type: ir.TypeRef{Target: "t/errors"}},
		}},
		"t/response": &ir.Model{ID: "t/response", Mixins: []ir.TypeRef{{Target: "t/envelope"}, {Target: "t/failure"}}},
	}

	f := ir.FlattenedProperties(reg, "t/response")
	for _, fp := range f.Properties {
		fmt.Printf("%s: %s from %s, required %t, overriding %d\n",
			fp.Property.EffectiveWireName(), fp.Property.Type.Target, fp.Owner, fp.Required, len(fp.Overridden))
	}
	fmt.Println("undeclared:", f.Undeclared)
	// Output:
	// success: t/false from t/failure, required true, overriding 1
	// result: t/page from t/envelope, required true, overriding 0
	// errors: t/errors from t/failure, required false, overriding 0
	// undeclared: [code]
}
