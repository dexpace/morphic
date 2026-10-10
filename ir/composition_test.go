package ir_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// compDoc builds a registry from definitions, keyed by each one's own ID.
func compDoc(defs ...ir.TypeDef) *ir.Document {
	types := ir.TypeRegistry{}
	for _, d := range defs {
		types[d.Common().ID] = d
	}
	return &ir.Document{Types: types}
}

func compModel(id ir.TypeID, mutate func(*ir.Model)) *ir.Model {
	m := &ir.Model{TypeCommon: ir.TypeCommon{ID: id}}
	if mutate != nil {
		mutate(m)
	}
	return m
}

func compScalar(id, base ir.TypeID) *ir.Scalar {
	s := &ir.Scalar{TypeCommon: ir.TypeCommon{ID: id}}
	if base != "" {
		s.Base = &ir.TypeRef{Target: base}
	}
	return s
}

func refs(ids ...ir.TypeID) []ir.TypeRef {
	out := make([]ir.TypeRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, ir.TypeRef{Target: id})
	}
	return out
}

func TestSupertypes_BaseThenImplementsThroughAliases(t *testing.T) {
	t.Parallel()
	doc := compDoc(
		compModel("m", func(m *ir.Model) {
			m.Base = &ir.TypeRef{Target: "alias"}
			m.Implements = refs("i1", "i2")
			m.Mixins = refs("mix")
		}),
		compScalar("alias", "b"),
		compModel("b", nil), compModel("i1", nil), compModel("i2", nil), compModel("mix", nil),
	)
	assert.Equal(t, []ir.TypeID{"b", "i1", "i2"}, ir.Supertypes(doc, "m"))
}

func TestSupertypes_NamesNoLiveModel(t *testing.T) {
	t.Parallel()
	var nilModel *ir.Model
	doc := compDoc(compScalar("s", ""))
	doc.Types["nil"] = nilModel
	assert.Nil(t, ir.Supertypes(nil, "m"))
	assert.Nil(t, ir.Supertypes(doc, "missing"))
	assert.Nil(t, ir.Supertypes(doc, "s"))
	assert.Nil(t, ir.Supertypes(doc, "nil"))
}

func TestSupertypes_TypedNilAliasEndsTheChain(t *testing.T) {
	t.Parallel()
	var nilScalar *ir.Scalar
	doc := compDoc(
		compModel("m", func(m *ir.Model) { m.Implements = refs("a") }),
		compScalar("a", "z"),
	)
	doc.Types["z"] = nilScalar
	assert.Equal(t, []ir.TypeID{"z"}, ir.Supertypes(doc, "m"))
}

func TestIsSubtype_Transitive(t *testing.T) {
	t.Parallel()
	doc := compDoc(
		compModel("root", nil),
		compModel("mid", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "root"} }),
		compModel("leaf", func(m *ir.Model) { m.Implements = refs("mid") }),
	)
	assert.True(t, ir.IsSubtype(doc, "leaf", "root"))
	assert.True(t, ir.IsSubtype(doc, "leaf", "mid"))
	assert.False(t, ir.IsSubtype(doc, "root", "leaf"))
	assert.False(t, ir.IsSubtype(doc, "root", "root"), "the relation is not reflexive")
	assert.False(t, ir.IsSubtype(nil, "leaf", "root"))
}

func TestIsSubtype_ExcludesMixins(t *testing.T) {
	t.Parallel()
	doc := compDoc(
		compModel("x", nil),
		compModel("m", func(m *ir.Model) { m.Mixins = refs("x") }),
		compModel("viaAlias", func(m *ir.Model) { m.Implements = refs("alias") }),
		compScalar("alias", "x"),
	)
	assert.False(t, ir.IsSubtype(doc, "m", "x"), "a mixin makes no subtype")
	assert.True(t, ir.IsSubtype(doc, "viaAlias", "x"), "an alias scalar is read through")
}

func TestIsSubtype_CyclicAliasChainTerminates(t *testing.T) {
	t.Parallel()
	doc := compDoc(
		compScalar("a", "b"), compScalar("b", "a"),
		compModel("m", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "a"} }),
	)
	assert.False(t, ir.IsSubtype(doc, "m", "elsewhere"))
}

func TestIsSubtype_CyclicCompositionTerminates(t *testing.T) {
	t.Parallel()
	doc := compDoc(
		compModel("a", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "b"} }),
		compModel("b", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: "a"} }),
	)
	assert.True(t, ir.IsSubtype(doc, "a", "a"), "a cycle leads back to the start")
	assert.False(t, ir.IsSubtype(doc, "a", "elsewhere"))
}

func prop(id ir.PropID) ir.Property { return ir.Property{ID: id} }

func TestExposedProps_IncludesMixins(t *testing.T) {
	t.Parallel()
	doc := compDoc(
		compModel("m", func(m *ir.Model) {
			m.Properties = []ir.Property{prop("m.own")}
			m.Base = &ir.TypeRef{Target: "base"}
			m.Implements = refs("iface")
			m.Mixins = refs("mix", "viaAlias")
		}),
		compModel("base", func(m *ir.Model) { m.Properties = []ir.Property{prop("base.p")} }),
		compModel("iface", func(m *ir.Model) { m.Properties = []ir.Property{prop("iface.p")} }),
		compModel("mix", func(m *ir.Model) { m.Properties = []ir.Property{prop("mix.p")} }),
		compScalar("viaAlias", "aliased"),
		compModel("aliased", func(m *ir.Model) { m.Properties = []ir.Property{prop("aliased.p")} }),
		compScalar("opaque", ""),
		compModel("unrelated", func(m *ir.Model) { m.Properties = []ir.Property{prop("unrelated.p")} }),
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
	doc := compDoc(
		compModel("a", func(m *ir.Model) { m.Properties = []ir.Property{prop("a.p")}; m.Mixins = refs("b") }),
		compModel("b", func(m *ir.Model) { m.Properties = []ir.Property{prop("b.p")}; m.Mixins = refs("a") }),
	)
	assert.Equal(t, map[ir.PropID]bool{"a.p": true, "b.p": true}, ir.ExposedProps(doc, "a"))
}

func TestExposedProps_SkipsTypedNil(t *testing.T) {
	t.Parallel()
	var nilModel *ir.Model
	doc := compDoc(compModel("m", func(m *ir.Model) { m.Mixins = refs("n") }))
	doc.Types["n"] = nilModel
	assert.Empty(t, ir.ExposedProps(doc, "m"))
}

func TestExposedProps_OtherKindsExposeNothing(t *testing.T) {
	t.Parallel()
	doc := compDoc(&ir.Enum{TypeCommon: ir.TypeCommon{ID: "e"}})
	assert.Empty(t, ir.ExposedProps(doc, "e"))
}
