package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

func TestSupertypes_BaseThenImplementsThroughAliases(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ModelOf("m", func(m *ir.Model) {
			m.Base = &ir.TypeRef{Target: "alias"}
			m.Implements = irtest.RefsTo("i1", "i2")
			m.Mixins = irtest.RefsTo("mix")
		}),
		irtest.ScalarOf("alias", "alias2"), irtest.ScalarOf("alias2", "b"),
		irtest.ModelOf("b", nil), irtest.ModelOf("i1", nil), irtest.ModelOf("i2", nil), irtest.ModelOf("mix", nil),
	)
	assert.Equal(t, []ir.TypeID{"b", "i1", "i2"}, ir.Supertypes(doc, "m"))
}

func TestSupertypes_NamesNoLiveModel(t *testing.T) {
	t.Parallel()
	var nilModel *ir.Model
	doc := irtest.DocOf(irtest.ScalarOf("s", ""))
	doc.Types["nil"] = nilModel
	assert.Nil(t, ir.Supertypes(nil, "m"))
	assert.Nil(t, ir.Supertypes(doc, "missing"))
	assert.Nil(t, ir.Supertypes(doc, "s"))
	assert.Nil(t, ir.Supertypes(doc, "nil"))
}

func TestSupertypes_TypedNilAliasEndsTheChain(t *testing.T) {
	t.Parallel()
	var nilScalar *ir.Scalar
	doc := irtest.DocOf(
		irtest.ModelOf("m", func(m *ir.Model) { m.Implements = irtest.RefsTo("a") }),
		irtest.ScalarOf("a", "z"),
	)
	doc.Types["z"] = nilScalar
	assert.Equal(t, []ir.TypeID{"z"}, ir.Supertypes(doc, "m"))
}

func TestSupertypes_AnOpaqueScalarStandsForItself(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ModelOf("m", func(m *ir.Model) { m.Implements = irtest.RefsTo("opaque") }),
		irtest.ScalarOf("opaque", ""),
	)
	assert.Equal(t, []ir.TypeID{"opaque"}, ir.Supertypes(doc, "m"))
}

func TestSupertypes_SkipsAReferenceNamingNoType(t *testing.T) {
	t.Parallel()
	holey := &ir.Scalar{ID: "holey", Base: &ir.TypeRef{}}
	doc := irtest.DocOf(
		irtest.ModelOf("m", func(m *ir.Model) {
			m.Base = &ir.TypeRef{}
			m.Implements = []ir.TypeRef{{}, {Target: "holey"}, {Target: "i"}}
		}),
		holey, irtest.ModelOf("i", nil),
	)
	assert.Equal(t, []ir.TypeID{"i"}, ir.Supertypes(doc, "m"))
	assert.False(t, ir.IsSubtype(doc, "m", ""), "an unset reference makes no supertype to match")
}

func TestIsSubtype_Transitive(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ModelOf("root", nil),
		irtest.ModelOf("mid", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "root"} }),
		irtest.ModelOf("leaf", func(m *ir.Model) { m.Implements = irtest.RefsTo("mid") }),
	)
	assert.True(t, ir.IsSubtype(doc, "leaf", "root"))
	assert.True(t, ir.IsSubtype(doc, "leaf", "mid"))
	assert.False(t, ir.IsSubtype(doc, "root", "leaf"))
	assert.False(t, ir.IsSubtype(doc, "root", "root"), "the relation is not reflexive")
	assert.False(t, ir.IsSubtype(nil, "leaf", "root"))
}

func TestIsSubtype_ExcludesMixins(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ModelOf("x", nil),
		irtest.ModelOf("m", func(m *ir.Model) { m.Mixins = irtest.RefsTo("x") }),
		irtest.ModelOf("viaAlias", func(m *ir.Model) { m.Implements = irtest.RefsTo("alias") }),
		irtest.ScalarOf("alias", "x"),
	)
	assert.False(t, ir.IsSubtype(doc, "m", "x"), "a mixin makes no subtype")
	assert.True(t, ir.IsSubtype(doc, "viaAlias", "x"), "an alias scalar is read through")
}

func TestIsSubtype_CyclicAliasChainTerminates(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ScalarOf("a", "b"), irtest.ScalarOf("b", "a"),
		irtest.ModelOf("m", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "a"} }),
	)
	assert.False(t, ir.IsSubtype(doc, "m", "elsewhere"))
}

func TestIsSubtype_CyclicCompositionTerminates(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ModelOf("a", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "b"} }),
		irtest.ModelOf("b", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "a"} }),
	)
	assert.True(t, ir.IsSubtype(doc, "a", "a"), "a cycle leads back to the start")
	assert.False(t, ir.IsSubtype(doc, "a", "elsewhere"))
}

func prop(id ir.PropID) ir.Property { return ir.Property{ID: id} }

func TestExposedProps_IncludesMixins(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ModelOf("m", func(m *ir.Model) {
			m.Properties = []ir.Property{prop("m.own")}
			m.Base = &ir.TypeRef{Target: "base"}
			m.Implements = irtest.RefsTo("iface")
			m.Mixins = irtest.RefsTo("mix", "viaAlias")
		}),
		irtest.ModelOf("base", func(m *ir.Model) { m.Properties = []ir.Property{prop("base.p")} }),
		irtest.ModelOf("iface", func(m *ir.Model) { m.Properties = []ir.Property{prop("iface.p")} }),
		irtest.ModelOf("mix", func(m *ir.Model) { m.Properties = []ir.Property{prop("mix.p")} }),
		irtest.ScalarOf("viaAlias", "aliased"),
		irtest.ModelOf("aliased", func(m *ir.Model) { m.Properties = []ir.Property{prop("aliased.p")} }),
		irtest.ScalarOf("opaque", ""),
		irtest.ModelOf("unrelated", func(m *ir.Model) { m.Properties = []ir.Property{prop("unrelated.p")} }),
	)
	want := map[ir.PropID]bool{
		"m.own": true, "base.p": true, "iface.p": true, "mix.p": true, "aliased.p": true,
	}
	assert.Equal(t, want, ir.ExposedProps(doc, "m"))
	assert.Empty(t, ir.ExposedProps(doc, "opaque"))
	assert.Empty(t, ir.ExposedProps(doc, "missing"))
	assert.Empty(t, ir.ExposedProps(nil, "m"))
}

func TestExposedProps_CyclicMixinsTerminate(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(
		irtest.ModelOf("a", func(m *ir.Model) { m.Properties = []ir.Property{prop("a.p")}; m.Mixins = irtest.RefsTo("b") }),
		irtest.ModelOf("b", func(m *ir.Model) { m.Properties = []ir.Property{prop("b.p")}; m.Mixins = irtest.RefsTo("a") }),
	)
	assert.Equal(t, map[ir.PropID]bool{"a.p": true, "b.p": true}, ir.ExposedProps(doc, "a"))
}

func TestExposedProps_SkipsTypedNil(t *testing.T) {
	t.Parallel()
	var nilModel *ir.Model
	doc := irtest.DocOf(irtest.ModelOf("m", func(m *ir.Model) { m.Mixins = irtest.RefsTo("n") }))
	doc.Types["n"] = nilModel
	assert.Empty(t, ir.ExposedProps(doc, "m"))
}

func TestExposedProps_OtherKindsExposeNothing(t *testing.T) {
	t.Parallel()
	doc := irtest.DocOf(&ir.Enum{ID: "e"})
	assert.Empty(t, ir.ExposedProps(doc, "e"))
}
