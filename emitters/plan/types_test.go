// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/plan"
	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irfacts"
	"github.com/dexpace/morphic/ir/irtest"
)

// build plans doc under pp and requires that it reported nothing.
func build(t *testing.T, doc *ir.Document, pp plan.Policy) *plan.Plan {
	t.Helper()
	built, diags := plan.Build(doc, irfacts.New(doc), pp)
	require.Empty(t, diags)
	require.NotNil(t, built)
	return built
}

// property declares the member name of owner, shaped by mutate when it is not
// nil.
func property(owner ir.TypeID, name string, mutate func(*ir.Property)) ir.Property {
	declared := ir.Property{
		ID:   ir.PropertyID(string(owner) + "/" + name),
		Name: ir.NamingFor(name),
		Type: ir.TypeRef{Target: "type/string"},
	}
	if mutate != nil {
		mutate(&declared)
	}
	return declared
}

// TestBuild_FlattensAModelAcrossItsComposition holds each field read off the
// flattening to one model that composes a base, a mixin and a list, redeclares
// a member of its base, and requires a mixed-in member and an undeclared name.
func TestBuild_FlattensAModelAcrossItsComposition(t *testing.T) {
	t.Parallel()
	useUID := &ir.Deprecation{Message: "use uid"}
	base := irtest.ModelOf("type/base", func(m *ir.Model) {
		m.Properties = []ir.Property{
			property(m.ID, "id", func(p *ir.Property) { p.Required, p.Deprecation = true, useUID }),
			property(m.ID, "token", func(p *ir.Property) { p.Secret = true }),
		}
	})
	mixin := irtest.ModelOf("type/mixin", func(m *ir.Model) {
		m.Properties = []ir.Property{property(m.ID, "note", nil)}
	})
	widget := irtest.ModelOf("type/widget", func(m *ir.Model) {
		m.Base = &ir.TypeRef{Target: base.ID}
		m.Mixins = irtest.RefsTo(mixin.ID, "type/tags")
		m.Properties = []ir.Property{
			property(m.ID, "name", func(p *ir.Property) { p.Required = true }),
			property(m.ID, "id", nil),
		}
		m.Requires = []string{"note", "extra"}
	})
	tags := &ir.List{ID: "type/tags", ElementType: ir.TypeRef{Target: "type/string"}}
	doc := irtest.DocOf(base, mixin, widget, tags, &ir.Primitive{
		ID: "type/string", PrimitiveKind: ir.PrimitiveString,
	})

	built := build(t, doc, plan.Policy{})

	want := plan.TypePlan{
		Flattened:   []ir.PropertyID{"type/widget/id", "type/base/token", "type/mixin/note", "type/widget/name"},
		Required:    []ir.PropertyID{"type/widget/id", "type/mixin/note", "type/widget/name"},
		Undeclared:  []string{"extra"},
		Redacted:    []ir.PropertyID{"type/base/token"},
		Deprecated:  map[ir.PropertyID]*ir.Deprecation{"type/widget/id": useUID},
		Unflattened: []ir.TypeID{"type/tags"},
	}
	assert.Empty(t, cmp.Diff(want, built.Types[widget.ID]))
	assert.Same(t, useUID, built.Types[widget.ID].Deprecated["type/widget/id"],
		"the plan points into the document and copies none of it")
	assert.Empty(t, cmp.Diff(plan.TypePlan{Flattened: []ir.PropertyID{"type/mixin/note"}}, built.Types[mixin.ID]))
	assert.Empty(t, cmp.Diff(plan.TypePlan{}, built.Types[tags.ID]), "a list composes no member")
}

func TestBuild_ClassifiesAUnionsWireShape(t *testing.T) {
	t.Parallel()
	discriminator := &ir.Discriminator{PropertyName: "kind"}
	tests := []struct {
		name       string
		union      ir.Union
		want       plan.UnionShape
		wantStrict bool
	}{
		{"exactly one variant holds a value", ir.Union{Exclusive: true}, plan.UnionOneOf, true},
		{"one or more variants hold a value", ir.Union{}, plan.UnionAnyOf, false},
		{"a tag the discriminator locates", ir.Union{WireTagged: true, Discriminator: discriminator},
			plan.UnionDiscriminated, false},
		{"a tag on an exclusive union", ir.Union{WireTagged: true, Exclusive: true, Discriminator: discriminator},
			plan.UnionDiscriminated, false},
		{"the one key of an object", ir.Union{WireTagged: true}, plan.UnionKeyTagged, false},
		{"an exclusive union tagged by its key", ir.Union{WireTagged: true, Exclusive: true}, plan.UnionKeyTagged, false},
		{"a discriminator on a payload naming no variant", ir.Union{Discriminator: discriminator}, plan.UnionAnyOf, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			union := tt.union
			union.ID = "type/union"
			doc := irtest.DocOf(&union)

			lenient := build(t, doc, plan.Policy{}).Types[union.ID]
			assert.Empty(t, cmp.Diff(plan.TypePlan{Union: tt.want}, lenient),
				"with StrictExclusive off no union is strict")

			strict := build(t, doc, plan.Policy{StrictExclusive: true}).Types[union.ID]
			assert.Empty(t, cmp.Diff(plan.TypePlan{Union: tt.want, Strict: tt.wantStrict}, strict))
		})
	}
}

// TestBuild_EveryKindHasAPlan is the dispatch's switch-completeness test
// (T-1): derives says what Build reads off each kind, and a kind ir declares
// that it does not list fails here until the dispatch and this table say what
// the plan derives of it.
func TestBuild_EveryKindHasAPlan(t *testing.T) {
	t.Parallel()
	derives := map[ir.TypeKind]plan.TypePlan{
		ir.KindPrimitive: {},
		ir.KindScalar:    {},
		ir.KindModel:     {Flattened: []ir.PropertyID{"type/model/id"}},
		ir.KindUnion:     {Union: plan.UnionAnyOf},
		ir.KindEnum:      {},
		ir.KindList:      {},
		ir.KindMap:       {},
		ir.KindTuple:     {},
		ir.KindLiteral:   {},
		ir.KindExternal:  {},
		ir.KindAny:       {},
	}
	pkg, err := decls.Load("github.com/dexpace/morphic/ir")
	require.NoError(t, err)
	kinds, err := pkg.StringConsts("TypeKind")
	require.NoError(t, err)
	require.Len(t, kinds, len(derives), "derives lists a kind ir does not declare, or misses one it does")

	for _, declared := range kinds {
		typeKind := ir.TypeKind(declared.Value)
		t.Run(declared.Value, func(t *testing.T) {
			t.Parallel()
			want, listed := derives[typeKind]
			require.True(t, listed, "derives does not say what the plan reads off a %s", typeKind)
			td, ok := ir.NewTypeDef(typeKind)
			require.True(t, ok)
			td.Common().ID = ir.TypeID("type/" + declared.Value)
			if model, isModel := td.(*ir.Model); isModel {
				model.Properties = []ir.Property{property(model.ID, "id", nil)}
			}

			built := build(t, irtest.DocOf(td), plan.Policy{})

			got, planned := built.Types[td.Common().ID]
			require.True(t, planned, "every registry type has an entry")
			assert.Empty(t, cmp.Diff(want, got))
		})
	}
}

func TestBuild_ANilEntryPlansAsTheZeroTypePlan(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{
		"type/untyped": nil,
		"type/model":   (*ir.Model)(nil),
		"type/union":   (*ir.Union)(nil),
		"type/scalar":  (*ir.Scalar)(nil),
	}}

	built := build(t, doc, plan.Policy{StrictExclusive: true})

	want := map[ir.TypeID]plan.TypePlan{"type/untyped": {}, "type/model": {}, "type/union": {}, "type/scalar": {}}
	assert.Empty(t, cmp.Diff(want, built.Types))
}

// TestBuild_ReadsSensitiveAlongTheBaseChain plans, in one registry, a type of
// every relation to a Sensitive one that a Base chain gives.
func TestBuild_ReadsSensitiveAlongTheBaseChain(t *testing.T) {
	t.Parallel()
	secret := irtest.ScalarOf("type/secret", "type/string")
	secret.Sensitive = true
	list := &ir.List{ID: "type/list", Sensitive: true, ElementType: ir.TypeRef{Target: "type/string"}}
	doc := irtest.DocOf(
		&ir.Primitive{ID: "type/string", PrimitiveKind: ir.PrimitiveString},
		secret,
		list,
		irtest.ScalarOf("type/over-secret", secret.ID),
		irtest.ScalarOf("type/twice-over-secret", "type/over-secret"),
		irtest.ModelOf("type/over-sensitive-list", func(m *ir.Model) { m.Base = &ir.TypeRef{Target: list.ID} }),
		irtest.ScalarOf("type/plain", "type/string"),
		irtest.ScalarOf("type/opaque", ""),
		irtest.ScalarOf("type/dangling", "type/missing"),
		irtest.ScalarOf("type/loop-a", "type/loop-b"),
		irtest.ScalarOf("type/loop-b", "type/loop-a"),
		irtest.ModelOf("type/holds-secret", func(m *ir.Model) {
			m.Properties = []ir.Property{property(m.ID, "key", func(p *ir.Property) { p.Type.Target = secret.ID })}
		}),
	)

	built := build(t, doc, plan.Policy{})

	want := map[ir.TypeID]bool{
		"type/string":              false,
		"type/secret":              true,
		"type/list":                true,
		"type/over-secret":         true,
		"type/twice-over-secret":   true,
		"type/over-sensitive-list": true,
		"type/plain":               false,
		"type/opaque":              false,
		"type/dangling":            false,
		"type/loop-a":              false,
		"type/loop-b":              false,
		"type/holds-secret":        false,
	}
	got := map[ir.TypeID]bool{}
	for id, typePlan := range built.Types {
		got[id] = typePlan.Sensitive
	}
	assert.Empty(t, cmp.Diff(want, got))
	assert.Equal(t, []ir.PropertyID{"type/holds-secret/key"}, built.Types["type/holds-secret"].Redacted,
		"a member of a sensitive type is redacted, and the model holding it is not sensitive for that")
}

// decorator builds the anonymous scalar id over base, which says nothing its
// base does not.
func decorator(id, base ir.TypeID) *ir.Scalar {
	scalar := irtest.ScalarOf(id, base)
	scalar.Anonymous = true
	scalar.Docs.Description = "a note beside the reference"
	return scalar
}

// TestBuild_ResolvesADecoratorToItsChainsEnd plans, in one registry, a
// decorator of every way a chain ends, and the types that are none.
func TestBuild_ResolvesADecoratorToItsChainsEnd(t *testing.T) {
	t.Parallel()
	modelDecorator := irtest.ModelOf("type/model-decorator", func(m *ir.Model) {
		m.Anonymous = true
		m.Base = &ir.TypeRef{Target: "type/status"}
	})
	named := irtest.ScalarOf("type/named-alias", "type/status")
	doc := irtest.DocOf(
		&ir.Enum{ID: "type/status", ValueType: ir.PrimitiveString, Closed: true},
		decorator("type/once", "type/status"),
		decorator("type/twice", "type/once"),
		decorator("type/thrice", "type/twice"),
		modelDecorator,
		named,
		decorator("type/over-named", named.ID),
		decorator("type/dangling", "type/missing"),
		decorator("type/self", "type/self"),
		decorator("type/loop-a", "type/loop-b"),
		decorator("type/loop-b", "type/loop-a"),
		decorator("type/into-loop", "type/loop-a"),
	)

	built := build(t, doc, plan.Policy{})

	want := map[ir.TypeID]ir.TypeID{
		"type/status":          "",
		"type/once":            "type/status",
		"type/twice":           "type/status",
		"type/thrice":          "type/status",
		"type/model-decorator": "type/status",
		"type/named-alias":     "",
		"type/over-named":      "type/named-alias",
		"type/dangling":        "",
		"type/self":            "",
		"type/loop-a":          "",
		"type/loop-b":          "",
		"type/into-loop":       "",
	}
	got := map[ir.TypeID]ir.TypeID{}
	for id, typePlan := range built.Types {
		got[id] = typePlan.Alias
	}
	assert.Empty(t, cmp.Diff(want, got))
}
