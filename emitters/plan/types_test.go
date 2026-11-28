// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package plan_test

import (
	"maps"
	"slices"
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

// build plans doc under policy and requires that it reported nothing.
func build(t *testing.T, doc *ir.Document, policy plan.Policy) *plan.Plan {
	t.Helper()
	built, diags := plan.Build(doc, irfacts.New(doc), policy)
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
// flattening to one model that composes a base, a mixin and a list, requires a
// mixed-in member and an undeclared name, and redeclares two members of its
// base, stating none of what the base says of them: one required and
// deprecated there, one secret.
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
			property(m.ID, "token", nil),
		}
		m.Requires = []string{"note", "extra"}
	})
	tags := &ir.List{ID: "type/tags", ElementType: ir.TypeRef{Target: "type/string"}}
	doc := irtest.DocOf(base, mixin, widget, tags, &ir.Primitive{
		ID: "type/string", PrimitiveKind: ir.PrimitiveString,
	})

	built := build(t, doc, plan.Policy{})

	want := plan.TypePlan{
		Flattened:   []ir.PropertyID{"type/widget/id", "type/widget/token", "type/mixin/note", "type/widget/name"},
		Required:    []ir.PropertyID{"type/widget/id", "type/mixin/note", "type/widget/name"},
		Undeclared:  []string{"extra"},
		Redacted:    []ir.PropertyID{"type/widget/token"},
		Deprecated:  map[ir.PropertyID]*ir.Deprecation{"type/widget/id": useUID},
		Unflattened: []ir.TypeID{"type/tags"},
	}
	assert.Empty(t, cmp.Diff(want, built.Types[widget.ID]), "(-want +got)")
	assert.Same(t, useUID, built.Types[widget.ID].Deprecated["type/widget/id"],
		"the plan points into the document and copies none of it")
	assert.Empty(t, cmp.Diff(plan.TypePlan{Flattened: []ir.PropertyID{"type/mixin/note"}}, built.Types[mixin.ID]),
		"(-want +got)")
	assert.Empty(t, cmp.Diff(plan.TypePlan{}, built.Types[tags.ID]), "a list composes no member (-want +got)")
}

func TestBuild_ClassifiesAUnionsWireShape(t *testing.T) {
	t.Parallel()
	discriminator := &ir.Discriminator{PropertyName: "kind"}
	// routed is what the plan reads off discriminator over no variant.
	routed := &plan.DiscrimFacts{Locator: plan.DiscrimPropertyName, WireKey: "kind"}
	tests := []struct {
		name        string
		union       ir.Union
		want        plan.UnionShape
		wantDiscrim *plan.DiscrimFacts
		wantStrict  bool
	}{
		{"exactly one variant holds a value", ir.Union{Exclusive: true}, plan.UnionOneOf, nil, true},
		{"one or more variants hold a value", ir.Union{}, plan.UnionAnyOf, nil, false},
		{"a tag the discriminator locates", ir.Union{WireTagged: true, Discriminator: discriminator},
			plan.UnionDiscriminated, routed, false},
		{"a tag on an exclusive union", ir.Union{WireTagged: true, Exclusive: true, Discriminator: discriminator},
			plan.UnionDiscriminated, routed, false},
		{"the one key of an object", ir.Union{WireTagged: true}, plan.UnionKeyTagged, nil, false},
		{"an exclusive union tagged by its key", ir.Union{WireTagged: true, Exclusive: true},
			plan.UnionKeyTagged, nil, false},
		{"a discriminator on a payload naming no variant", ir.Union{Discriminator: discriminator},
			plan.UnionAnyOf, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			union := tt.union
			union.ID = "type/union"
			union.Closed = true
			doc := irtest.DocOf(&union)

			lenient := build(t, doc, plan.Policy{}).Types[union.ID]
			assert.Empty(t, cmp.Diff(plan.TypePlan{Union: tt.want, Discrim: tt.wantDiscrim}, lenient),
				"with StrictExclusive off no union is strict (-want +got)")

			strict := build(t, doc, plan.Policy{StrictExclusive: true}).Types[union.ID]
			assert.Empty(t, cmp.Diff(plan.TypePlan{Union: tt.want, Discrim: tt.wantDiscrim, Strict: tt.wantStrict},
				strict), "(-want +got)")
		})
	}
}

// TestBuild_EveryKindHasAPlan is the dispatch's switch-completeness test
// (T-1): derives says what Build reads off each kind, and a kind ir declares
// that it does not list fails here until the dispatch and this table say what
// the plan derives of it. The zero union and enum are open, Closed being
// false.
func TestBuild_EveryKindHasAPlan(t *testing.T) {
	t.Parallel()
	derives := map[ir.TypeKind]plan.TypePlan{
		ir.KindPrimitive: {},
		ir.KindScalar:    {},
		ir.KindModel:     {Flattened: []ir.PropertyID{"type/model/id"}},
		ir.KindUnion:     {Union: plan.UnionAnyOf, Open: true},
		ir.KindEnum:      {Open: true},
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
			assert.Empty(t, cmp.Diff(want, got), "(-want +got)")
		})
	}
}

// TestBuild_OpensWhatThePolicyOpens holds Open on every kind it reads, under
// each pair of OpenUnions and OpenEnums (T-22, T-36): what the IR declares
// open is open under any policy, each switch opens only its own kinds, and
// neither opens what a request alone carries, an enum declaring a fallback or
// one with no member.
func TestBuild_OpensWhatThePolicyOpens(t *testing.T) {
	t.Parallel()
	policies := [...]plan.Policy{{}, {OpenUnions: true}, {OpenEnums: true}, {OpenUnions: true, OpenEnums: true}}
	member := []ir.EnumMember{{ID: "enum-member/subject/small", Name: ir.NamingFor("small")}}
	tests := []struct {
		name string
		td   ir.TypeDef
		// want is Open under each of policies, in order.
		want [len(policies)]bool
	}{
		{"a closed union", &ir.Union{Closed: true}, [...]bool{false, true, false, true}},
		{"an open union", &ir.Union{}, [...]bool{true, true, true, true}},
		{"a closed union only a request carries", &ir.Union{Closed: true, InputOnly: true},
			[...]bool{false, false, false, false}},
		{"an open union only a request carries", &ir.Union{InputOnly: true}, [...]bool{true, true, true, true}},
		{"a model holding a discriminator", irtest.ModelOf("", func(m *ir.Model) {
			m.Discriminator = &ir.Discriminator{PropertyName: "kind"}
		}), [...]bool{false, true, false, true}},
		{"an abstract model", irtest.ModelOf("", func(m *ir.Model) { m.Abstract = true }),
			[...]bool{false, true, false, true}},
		{"a base only a request carries", irtest.ModelOf("", func(m *ir.Model) {
			m.Abstract, m.InputOnly = true, true
			m.Discriminator = &ir.Discriminator{PropertyName: "kind"}
		}), [...]bool{false, false, false, false}},
		{"a model that is no base", irtest.ModelOf("", nil), [...]bool{false, false, false, false}},
		{"a closed enum", &ir.Enum{Closed: true, Members: member}, [...]bool{false, false, true, true}},
		{"an open enum", &ir.Enum{Members: member}, [...]bool{true, true, true, true}},
		{"a closed enum declaring a fallback", &ir.Enum{Closed: true, Members: member, FallbackMember: "small"},
			[...]bool{false, false, false, false}},
		{"a closed enum with no member", &ir.Enum{Closed: true}, [...]bool{false, false, false, false}},
		{"a closed flags enum with no member", &ir.Enum{Closed: true, Flags: true},
			[...]bool{false, false, false, false}},
		{"an open enum with no member", &ir.Enum{}, [...]bool{true, true, true, true}},
		{"a scalar", irtest.ScalarOf("", "type/string"), [...]bool{false, false, false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.td.Common().ID = "type/subject"
			doc := irtest.DocOf(tt.td, &ir.Primitive{ID: "type/string", PrimitiveKind: ir.PrimitiveString})

			var got [len(policies)]bool
			for i, policy := range policies {
				got[i] = build(t, doc, policy).Types["type/subject"].Open
			}

			assert.Equal(t, tt.want, got, "Open under %+v", policies)
		})
	}
}

func TestBuild_ANilEntryPlansAsTheZeroTypePlan(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Types: ir.TypeRegistry{
		"type/untyped": nil,
		"type/model":   (*ir.Model)(nil),
		"type/union":   (*ir.Union)(nil),
		"type/enum":    (*ir.Enum)(nil),
		"type/scalar":  (*ir.Scalar)(nil),
	}}

	built := build(t, doc, plan.Policy{OpenUnions: true, OpenEnums: true, StrictExclusive: true})

	want := map[ir.TypeID]plan.TypePlan{
		"type/untyped": {}, "type/model": {}, "type/union": {}, "type/enum": {}, "type/scalar": {},
	}
	assert.Empty(t, cmp.Diff(want, built.Types), "(-want +got)")
}

// TestBuild_ReadsSensitiveAlongTheBaseChain plans, in one registry, a type of
// every relation to a Sensitive one that a Base chain gives, and of the two a
// composition gives that are no Base. It then holds the plan to the IR's own
// reading: a member of each type is redacted in ir.FlattenedProperties exactly
// where the plan calls its type sensitive, so the two walks cannot part.
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
		irtest.ModelOf("type/mixes-sensitive-list", func(m *ir.Model) { m.Mixins = irtest.RefsTo(list.ID) }),
		irtest.ModelOf("type/implements-secret", func(m *ir.Model) { m.Implements = irtest.RefsTo(secret.ID) }),
		irtest.ModelOf("type/holds-secret", func(m *ir.Model) {
			m.Properties = []ir.Property{property(m.ID, "key", func(p *ir.Property) { p.Type.Target = secret.ID })}
		}),
	)
	// One member of each type above, the missing one among them, named by its
	// type.
	holder := irtest.ModelOf("type/holder", nil)
	for _, id := range append(slices.Sorted(maps.Keys(doc.Types)), "type/missing") {
		holder.Properties = append(holder.Properties,
			property(holder.ID, string(id), func(p *ir.Property) { p.Type.Target = id }))
	}
	doc.Types[holder.ID] = holder

	built := build(t, doc, plan.Policy{})

	want := map[ir.TypeID]bool{
		"type/string":               false,
		"type/secret":               true,
		"type/list":                 true,
		"type/over-secret":          true,
		"type/twice-over-secret":    true,
		"type/over-sensitive-list":  true,
		"type/plain":                false,
		"type/opaque":               false,
		"type/dangling":             false,
		"type/loop-a":               false,
		"type/loop-b":               false,
		"type/mixes-sensitive-list": false,
		"type/implements-secret":    false,
		"type/holds-secret":         false,
		"type/holder":               false,
	}
	got := map[ir.TypeID]bool{}
	for id, typePlan := range built.Types {
		got[id] = typePlan.Sensitive
	}
	assert.Empty(t, cmp.Diff(want, got), "(-want +got)")
	members := ir.FlattenedProperties(doc.Types, holder.ID).Properties
	require.Len(t, members, len(holder.Properties))
	for _, member := range members {
		memberType := member.Property.Type.Target
		assert.Equal(t, member.Secret, built.Types[memberType].Sensitive,
			"the plan and the flattening disagree on whether %s is sensitive", memberType)
	}
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
	assert.Empty(t, cmp.Diff(want, got), "(-want +got)")
}
