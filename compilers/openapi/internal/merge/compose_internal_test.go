// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package merge

import (
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// composeRegistry holds the types the composition tests declare fields as. A
// component sits under no declaration, so it reads as written by a $ref, and a
// node hoisted at or under declaredAt(n) reads as written inline by the nth
// declaration. Each model declares a property of its own, so no two are copies
// of one another (typeJudge.models).
func composeRegistry() map[ir.TypeID]ir.TypeDef {
	own := func(id ir.TypeID) []ir.Property { return []ir.Property{{WireName: string(id)}} }
	inline := func(id ir.TypeID, pointer jsontext.Pointer) *ir.Model {
		return &ir.Model{ID: id, Anonymous: true, Provenance: ptrAt(pointer), Properties: own(id)}
	}
	return map[ir.TypeID]ir.TypeDef{
		"type/A":      &ir.Model{ID: "type/A", Provenance: ptrAt("/components/schemas/A"), Properties: own("type/A")},
		"type/B":      &ir.Model{ID: "type/B", Provenance: ptrAt("/components/schemas/B"), Properties: own("type/B")},
		"type/sub":    &ir.Model{ID: "type/sub", Provenance: ptrAt("/components/schemas/Sub"), Base: &ir.TypeRef{Target: "type/A"}},
		"type/bare":   &ir.Model{ID: "type/bare", Anonymous: true, Provenance: ptrAt(declaredAt(0))},
		"type/bare1":  &ir.Model{ID: "type/bare1", Anonymous: true, Provenance: ptrAt(declaredAt(1))},
		"type/Pet":    &ir.Model{ID: "type/Pet", Provenance: ptrAt("/components/schemas/Pet"), Properties: own("type/Pet"), Discriminator: &ir.Discriminator{}},
		"type/Dog":    &ir.Model{ID: "type/Dog", Provenance: ptrAt("/components/schemas/Dog"), Properties: own("type/Dog"), Discriminator: &ir.Discriminator{}},
		"type/Mid":    &ir.Model{ID: "type/Mid", Provenance: ptrAt("/components/schemas/Mid"), Properties: own("type/Mid"), Base: &ir.TypeRef{Target: "type/Pet"}},
		"type/midAka": &ir.Scalar{ID: "type/midAka", Base: &ir.TypeRef{Target: "type/Mid"}},
		"type/Low":    &ir.Model{ID: "type/Low", Provenance: ptrAt("/components/schemas/Low"), Properties: own("type/Low"), Base: &ir.TypeRef{Target: "type/midAka"}},
		"type/loopA":  &ir.Model{ID: "type/loopA", Provenance: ptrAt("/components/schemas/LoopA"), Properties: own("type/loopA"), Base: &ir.TypeRef{Target: "type/loopB"}},
		"type/loopB":  &ir.Model{ID: "type/loopB", Provenance: ptrAt("/components/schemas/LoopB"), Properties: own("type/loopB"), Base: &ir.TypeRef{Target: "type/loopA"}},
		"type/alias":  &ir.Scalar{ID: "type/alias", Base: &ir.TypeRef{Target: "type/B"}},
		"type/tagged": &ir.Union{ID: "type/tagged", Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/A"}}}, Discriminator: &ir.Discriminator{}},
		"type/byTag":  &ir.Model{ID: "type/byTag", Provenance: ptrAt("/components/schemas/ByTag"), Properties: own("type/byTag"), Base: &ir.TypeRef{Target: "type/tagged"}},
		"type/guessed": &ir.Union{ID: "type/guessed", Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/B"}}},
			Discriminator: &ir.Discriminator{Inferred: true}},
		"type/in0":    inline("type/in0", declaredAt(0)),
		"type/in1":    inline("type/in1", declaredAt(1)),
		"type/in2":    inline("type/in2", declaredAt(2)),
		"type/nested": inline("type/nested", declaredAt(1)+"/properties/x"),
		"type/str":    &ir.Primitive{ID: "type/str", PrimitiveKind: ir.PrimitiveString},
		"type/top":    &ir.Primitive{ID: "type/top", PrimitiveKind: ir.PrimitiveAny},
		"type/any":    &ir.Any{ID: "type/any"},
		"type/map":    &ir.MapT{ID: "type/map"},
		"type/union":  &ir.Union{ID: "type/union", Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/A"}}, {Type: ir.TypeRef{Target: "type/B"}}}},
		"type/apart":  &ir.Union{ID: "type/apart", Variants: []ir.Variant{{Type: ir.TypeRef{Target: "type/B"}}, {Type: ir.TypeRef{Target: "type/map"}}}},
		"type/list":   &ir.List{ID: "type/list", ElementType: ir.TypeRef{Target: "type/str"}},
		"type/topDoc": &ir.Scalar{Base: &ir.TypeRef{Target: "type/top"}, ID: "type/topDoc", Docs: ir.Docs{Description: "d"}},
		"type/topEx":  &ir.Scalar{Base: &ir.TypeRef{Target: "type/top"}, ID: "type/topEx", Examples: []ir.Example{{}}},
	}
}

// declaredAt is where the nth declaration of a field is written, each in an
// allOf branch of one schema, so the second names the field (fieldSpan).
func declaredAt(n int) jsontext.Pointer {
	return jsontext.Pointer(fmt.Sprintf("/s/allOf/%d/properties/data", n))
}

// nthDecl is the nth declaration of a field, of target.
func nthDecl(n int, target ir.TypeID) declaration {
	return declaration{
		property: ir.Property{WireName: "data", Type: ir.TypeRef{Target: target}, Provenance: ptrAt(declaredAt(n))},
		source:   written(`{}`),
	}
}

// declsOf is one declaration of each target, in order.
func declsOf(targets ...ir.TypeID) []declaration {
	out := make([]declaration, len(targets))
	for i, typeID := range targets {
		out[i] = nthDecl(i, typeID)
	}
	return out
}

// TestFold_RedeclaredModelsCompose pins what a field declared as several models,
// or as a model beside a map or a union, takes: a model the fold mints,
// composing each once as a written allOf of them does, its Base the first taking
// part in a discriminator hierarchy at any depth. Nothing is lost, so
// nothing is kept and nothing reported, and a bare `{}` beside the models adds
// no conjunct.
func TestFold_RedeclaredModelsCompose(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		targets []ir.TypeID
		base    ir.TypeID
		mixins  []ir.TypeID
	}{
		{name: "a component, then an inline object", targets: []ir.TypeID{"type/A", "type/in1"}, base: "type/A", mixins: []ir.TypeID{"type/in1"}},
		{name: "an inline object, then a component", targets: []ir.TypeID{"type/in0", "type/B"}, base: "type/B", mixins: []ir.TypeID{"type/in0"}},
		{name: "two inline objects stay mixins", targets: []ir.TypeID{"type/in0", "type/in1"}, mixins: []ir.TypeID{"type/in0", "type/in1"}},
		{name: "two components stay mixins, in order", targets: []ir.TypeID{"type/B", "type/A"}, mixins: []ir.TypeID{"type/B", "type/A"}},
		{name: "a discriminated component is the base", targets: []ir.TypeID{"type/A", "type/Pet"}, base: "type/Pet", mixins: []ir.TypeID{"type/A"}},
		{name: "the first discriminated component is", targets: []ir.TypeID{"type/Dog", "type/A", "type/Pet"}, base: "type/Dog", mixins: []ir.TypeID{"type/A", "type/Pet"}},
		{name: "a subtype of a discriminated component is", targets: []ir.TypeID{"type/A", "type/Mid"}, base: "type/Mid", mixins: []ir.TypeID{"type/A"}},
		{name: "a deeper one, through an alias", targets: []ir.TypeID{"type/A", "type/Low"}, base: "type/Low", mixins: []ir.TypeID{"type/A"}},
		{name: "a cycle of subtypes declaring none is not", targets: []ir.TypeID{"type/A", "type/loopA"}, mixins: []ir.TypeID{"type/A", "type/loopA"}},
		{name: "a union holding a declared discriminator is", targets: []ir.TypeID{"type/A", "type/tagged"}, base: "type/tagged", mixins: []ir.TypeID{"type/A"}},
		{name: "a model under one is", targets: []ir.TypeID{"type/A", "type/byTag"}, base: "type/byTag", mixins: []ir.TypeID{"type/A"}},
		{name: "a union holding an inferred one is not", targets: []ir.TypeID{"type/A", "type/guessed"}, mixins: []ir.TypeID{"type/A", "type/guessed"}},
		{name: "one type declared twice composes once, where the later is", targets: []ir.TypeID{"type/A", "type/B", "type/A"}, mixins: []ir.TypeID{"type/B", "type/A"}},
		{name: "`{}` adds no conjunct", targets: []ir.TypeID{"type/top", "type/A", "type/in2"}, base: "type/A", mixins: []ir.TypeID{"type/in2"}},
		{name: "an alias of `{}` saying nothing of its values adds none", targets: []ir.TypeID{"type/A", "type/B", "type/topDoc"}, mixins: []ir.TypeID{"type/A", "type/B"}},
		{name: "a bare object adds no conjunct", targets: []ir.TypeID{"type/bare", "type/A", "type/in2"}, base: "type/A", mixins: []ir.TypeID{"type/in2"}},
		{name: "a type what composes it holds adds no conjunct", targets: []ir.TypeID{"type/A", "type/B", "type/sub"}, mixins: []ir.TypeID{"type/B", "type/sub"}},
		{name: "an alias composes as written", targets: []ir.TypeID{"type/alias", "type/in1"}, base: "type/alias", mixins: []ir.TypeID{"type/in1"}},
		{name: "a node under its declaration is inline", targets: []ir.TypeID{"type/in0", "type/nested"}, mixins: []ir.TypeID{"type/in0", "type/nested"}},
		{name: "a model and a map", targets: []ir.TypeID{"type/A", "type/map"}, mixins: []ir.TypeID{"type/A", "type/map"}},
		{name: "a map and an inline model", targets: []ir.TypeID{"type/map", "type/in1"}, base: "type/map", mixins: []ir.TypeID{"type/in1"}},
		{name: "a model and a union no variant of which holds it", targets: []ir.TypeID{"type/apart", "type/A"}, mixins: []ir.TypeID{"type/apart", "type/A"}},
		{name: "a map beside two models", targets: []ir.TypeID{"type/A", "type/B", "type/map"}, mixins: []ir.TypeID{"type/A", "type/B", "type/map"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := composeRegistry()
			stubbed, recorded := stubMerger(reg)

			got := stubbed.fold(declsOf(tc.targets...))

			require.Equal(t, composedAt(declaredAt(1)), got.Type.Target, "the field takes the minted model")
			model, ok := reg[got.Type.Target].(*ir.Model)
			require.True(t, ok, "the minted model is registered")
			assert.Empty(t, cmp.Diff(tc.mixins, targetsOf(model.Mixins)), "mixins")
			if tc.base == "" {
				assert.Nil(t, model.Base)
			} else {
				require.NotNil(t, model.Base)
				assert.Equal(t, tc.base, model.Base.Target)
			}
			assert.Empty(t, model.Properties, "the composition stays in source form")
			assert.Empty(t, *recorded, "nothing is lost, so nothing is reported")
			assert.Empty(t, got.Unmodeled, "and nothing is kept")
		})
	}
}

// targetsOf lists what refs name.
func targetsOf(refs []ir.TypeRef) []ir.TypeID {
	out := make([]ir.TypeID, 0, len(refs))
	for _, typeRef := range refs {
		out = append(out, typeRef.Target)
	}
	return out
}

// TestFold_OnlyTwoOrMoreModelsCompose pins where the type stays the winner's:
// one type holds every other, a declaration that remains is no model, map or
// union, or none is a model, so the fold keeps the type it would keep without
// composing, of a tie the later's, and mints nothing.
func TestFold_OnlyTwoOrMoreModelsCompose(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		targets []ir.TypeID
		want    ir.TypeID
		dropped []jsontext.Pointer
	}{
		{name: "one model twice", targets: []ir.TypeID{"type/A", "type/A"}, want: "type/A"},
		{name: "a model and what composes it", targets: []ir.TypeID{"type/A", "type/sub"}, want: "type/sub"},
		{name: "what composes a model, then the model", targets: []ir.TypeID{"type/sub", "type/A"}, want: "type/sub"},
		{name: "a model and an alias of it", targets: []ir.TypeID{"type/B", "type/alias"}, want: "type/alias"},
		{name: "a bare object and a model", targets: []ir.TypeID{"type/bare", "type/A"}, want: "type/A"},
		{name: "a model and a bare object", targets: []ir.TypeID{"type/A", "type/bare1"}, want: "type/A"},
		{name: "two bare objects written inline", targets: []ir.TypeID{"type/bare", "type/bare1"}, want: "type/bare"},
		{name: "`{}` loses to one model", targets: []ir.TypeID{"type/top", "type/A"}, want: "type/A"},
		{name: "a kind beside two models", targets: []ir.TypeID{"type/A", "type/B", "type/str"}, want: "type/str", dropped: []jsontext.Pointer{declaredAt(0), declaredAt(1)}},
		{name: "a degraded node beside two models", targets: []ir.TypeID{"type/any", "type/A", "type/B"}, want: "type/B", dropped: []jsontext.Pointer{declaredAt(0), declaredAt(1)}},
		{name: "a list beside two models", targets: []ir.TypeID{"type/A", "type/B", "type/list"}, want: "type/list", dropped: []jsontext.Pointer{declaredAt(0), declaredAt(1)}},
		{name: "a map and a union with no model", targets: []ir.TypeID{"type/map", "type/union"}, want: "type/union", dropped: []jsontext.Pointer{declaredAt(0)}},
		{name: "a union a variant of which holds a model, then the model", targets: []ir.TypeID{"type/union", "type/A"}, want: "type/A"},
		{name: "an alias of `{}` holding examples beside two models", targets: []ir.TypeID{"type/A", "type/B", "type/topEx"}, want: "type/topEx", dropped: []jsontext.Pointer{declaredAt(0), declaredAt(1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := composeRegistry()
			count := len(reg)
			stubbed, _ := stubMerger(reg)

			got := stubbed.fold(declsOf(tc.targets...))

			assert.Equal(t, tc.want, got.Type.Target)
			assert.Len(t, reg, count, "nothing is minted")
			want := make([]string, 0, len(tc.dropped))
			for _, pointer := range tc.dropped {
				want = append(want, losingDeclarationKey+string(pointer))
			}
			assert.ElementsMatch(t, want, slices.Collect(maps.Keys(got.Unmodeled)), "each drop is kept")
		})
	}
}

// TestFold_AComposedFieldIntersectsNull pins that the field's reference says
// what null it admits, under the rule every fold follows, and that the
// conjuncts carry no bit of their own: a conjunct names a type, not a usage.
func TestFold_AComposedFieldIntersectsNull(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		a, b bool
		want bool
	}{
		{"both admit null", true, true, true},
		{"only the first does", true, false, false},
		{"only the second does", false, true, false},
		{"neither does", false, false, false},
	} {
		reg := composeRegistry()
		stubbed, _ := stubMerger(reg)
		decls := declsOf("type/A", "type/in1")
		decls[0].property.Type.Nullable, decls[1].property.Type.Nullable = tc.a, tc.b

		got := stubbed.fold(decls)

		assert.Equal(t, tc.want, got.Type.Nullable, tc.name)
		model, ok := reg[got.Type.Target].(*ir.Model)
		require.True(t, ok, tc.name)
		require.NotNil(t, model.Base, tc.name)
		assert.False(t, model.Base.Nullable, "%s: the base carries no bit", tc.name)
		assert.False(t, model.Mixins[0].Nullable, "%s: nor does a mixin", tc.name)
	}
}

// TestFold_AComposedFieldKeepsEveryDeclarationsDetail pins that no declaration
// is dropped: each is in the cone, so a constraint any of them writes reaches
// the field, and one contradicting another is reported as for any other fold.
func TestFold_AComposedFieldKeepsEveryDeclarationsDetail(t *testing.T) {
	t.Parallel()
	stubbed, recorded := stubMerger(composeRegistry())
	decls := declsOf("type/A", "type/B", "type/in1")
	decls[0].property.Constraints = &ir.Constraints{MinProperties: new(int64(1)), Pattern: "^a$"}
	decls[1].property.Constraints = &ir.Constraints{MaxProperties: new(int64(4))}
	decls[2].property.Constraints = &ir.Constraints{MinProperties: new(int64(2)), Pattern: "^b$"}

	got := stubbed.fold(decls)

	require.NotNil(t, got.Constraints)
	assert.Equal(t, int64(2), *got.Constraints.MinProperties, "a bound takes its tightest value")
	assert.Equal(t, int64(4), *got.Constraints.MaxProperties, "a later declaration's keyword reaches the field")
	assert.Equal(t, "^b$", got.Constraints.Pattern, "the last to set a keyword with no order keeps it")
	require.Len(t, *recorded, 1)
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(0)))
}

// TestFold_AComposedFieldHoldsTheBoundsOfEachDeclarationsOwnNode pins the
// bounds of a field the fold composes: a declaration owning its node leaves its
// bounds there, its property holding none, so the field takes those of each node
// hoisted at a declaration (ownBounds), met with those a property holds. A
// component's bounds stay its own, which the composition reaches through its
// reference, and the composed model holds none.
func TestFold_AComposedFieldHoldsTheBoundsOfEachDeclarationsOwnNode(t *testing.T) {
	t.Parallel()
	reg := composeRegistry()
	reg["type/in0"].(*ir.Model).Constraints = &ir.Constraints{MinProperties: new(int64(1)), MaxProperties: new(int64(9))}
	reg["type/in1"].(*ir.Model).Constraints = &ir.Constraints{MaxProperties: new(int64(5))}
	reg["type/A"].(*ir.Model).Constraints = &ir.Constraints{MinProperties: new(int64(3))}
	stubbed, recorded := stubMerger(reg)
	decls := declsOf("type/in0", "type/in1", "type/A")
	decls[2].property.Constraints = &ir.Constraints{MaxProperties: new(int64(7))}

	got := stubbed.fold(decls)

	assert.Equal(t, &ir.Constraints{MinProperties: new(int64(1)), MaxProperties: new(int64(5))}, got.Constraints)
	composed, ok := reg[got.Type.Target].(*ir.Model)
	require.True(t, ok, "the field's type is the composition")
	assert.Nil(t, composed.Constraints)
	assert.Empty(t, *recorded)
}

// TestOwnBounds_ReadsTheNodeADeclarationOwns pins which bounds a declaration's
// own node gives a composed field: a Model's, a MapT's, a Scalar's or a Union's
// hoisted at the declaration, and none of a List, which no property held, of a
// node written elsewhere, or of a type the registry lacks.
func TestOwnBounds_ReadsTheNodeADeclarationOwns(t *testing.T) {
	t.Parallel()
	bound := &ir.Constraints{MinProperties: new(int64(1))}
	length := &ir.Constraints{MinLength: new(int64(1))}
	common := func(id ir.TypeID) ir.TypeCommon {
		return ir.TypeCommon{ID: id, Anonymous: true, Provenance: ptrAt(declaredAt(0))}
	}
	stringType := ir.TypeRef{Target: "type/str"}
	reg := map[ir.TypeID]ir.TypeDef{
		"type/model":  &ir.Model{TypeCommon: common("type/model"), Constraints: bound},
		"type/map":    &ir.MapT{TypeCommon: common("type/map"), Value: stringType, Constraints: bound},
		"type/scalar": &ir.Scalar{TypeCommon: common("type/scalar"), Base: &stringType, Constraints: length},
		"type/union":  &ir.Union{TypeCommon: common("type/union"), Variants: []ir.Variant{{Type: stringType}}, Constraints: length},
		"type/list":   &ir.List{TypeCommon: common("type/list"), ElementType: stringType, Constraints: &ir.Constraints{MinItems: new(int64(1))}},
		"type/named":  &ir.Model{ID: "type/named", Provenance: ptrAt("/components/schemas/N"), Constraints: bound},
	}
	stubbed, _ := stubMerger(reg)
	for target, want := range map[ir.TypeID]*ir.Constraints{
		"type/model": bound, "type/map": bound, "type/scalar": length, "type/union": length,
		"type/list": nil, "type/named": nil, "type/missing": nil,
	} {
		foldingValue := &folding{g: &stubbed.Merger, decls: declsOf(target)}
		assert.Equal(t, want, foldingValue.ownBounds(0), target)
	}
}

// TestFold_AliasesOfOneModelComposeThoughTheirBoundsMeet pins which node a field
// takes where both could stand. Two aliases of one model each say more than the
// model, so they compose as any two aliases do, and the composition holds both
// bounds on the aliases it names: no node is minted for the bounds beside it.
func TestFold_AliasesOfOneModelComposeThoughTheirBoundsMeet(t *testing.T) {
	t.Parallel()
	reg := composeRegistry()
	reg["type/min"] = &ir.Scalar{ID: "type/min", Base: &ir.TypeRef{Target: "type/B"}, Constraints: &ir.Constraints{MinProperties: new(int64(1))}}
	reg["type/max"] = &ir.Scalar{ID: "type/max", Base: &ir.TypeRef{Target: "type/B"}, Constraints: &ir.Constraints{MaxProperties: new(int64(4))}}
	stubbed, recorded := stubMerger(reg)

	got := stubbed.fold(declsOf("type/min", "type/max"))

	composed, ok := reg[got.Type.Target].(*ir.Model)
	require.True(t, ok, "the field's type is the composition")
	assert.Equal(t, []ir.TypeRef{{Target: "type/min"}, {Target: "type/max"}}, composed.Mixins)
	assert.Equal(t, 1, stubbed.walk.minted, "one pointer takes one node, which Register would otherwise refuse")
	assert.Empty(t, *recorded)
}

// TestFold_AComposedFieldAdoptsOnlyANull pins what a composed field takes of the
// values its declarations write. A null follows the field's null admission. Any
// other value describes one conjunct, and nothing judges it against a model, so
// it is reported and kept rather than adopted as the whole field's, whichever
// declaration wrote it.
func TestFold_AComposedFieldAdoptsOnlyANull(t *testing.T) {
	t.Parallel()
	object := ir.Value{Kind: ir.ValueObject}
	null := ir.Value{Kind: ir.ValueNull}
	for _, tc := range []struct {
		name     string
		at       int
		value    ir.Value
		nullable bool
		adopted  bool
	}{
		{name: "an object the first declaration writes", at: 0, value: object},
		{name: "an object the second declaration writes", at: 1, value: object},
		{name: "a null where the field admits null", at: 1, value: null, nullable: true, adopted: true},
		{name: "a null where it does not", at: 0, value: null},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stubbed, recorded := stubMerger(composeRegistry())
			decls := declsOf("type/A", "type/in1")
			decls[0].property.Type.Nullable, decls[1].property.Type.Nullable = tc.nullable, tc.nullable
			decls[tc.at].property.Default = &tc.value

			got := stubbed.fold(decls)

			if tc.adopted {
				assert.Equal(t, &tc.value, got.Default)
				assert.Empty(t, *recorded)
				return
			}
			assert.Nil(t, got.Default)
			require.Len(t, *recorded, 1)
			assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
			assert.Contains(t, (*recorded)[0].Message, "admits the default the declaration at "+string(declaredAt(tc.at))+" gives")
			assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(tc.at)))
		})
	}
}

// TestFold_ComposesOnlyTheDeclarationsItJudges pins the bound on what composes:
// past maxFoldDeclarations a declaration is kept unjudged, so it is no conjunct.
func TestFold_ComposesOnlyTheDeclarationsItJudges(t *testing.T) {
	t.Parallel()
	reg := composeRegistry()
	stubbed, recorded := stubMerger(reg)
	decls := make([]declaration, 0, maxFoldDeclarations+1)
	for range maxFoldDeclarations {
		decls = append(decls, nthDecl(0, "type/A"))
	}
	decls[1] = nthDecl(1, "type/B")
	decls = append(decls, nthDecl(2, "type/Pet"))

	got := stubbed.fold(decls)

	model, ok := reg[got.Type.Target].(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, cmp.Diff([]ir.TypeID{"type/B", "type/A"}, targetsOf(model.Mixins)),
		"the declaration past the bound composes nothing, and the last of one type stands for it")
	require.Len(t, *recorded, 1)
	assert.Equal(t, diag.BudgetExceeded, (*recorded)[0].Code)
	assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(2)))
}

// TestSelectBase_ChoosesAsTheRuleSays pins the rule both readers take: the only
// $ref, else the first discriminated one in the order the caller ranks them,
// else none.
func TestSelectBase_ChoosesAsTheRuleSays(t *testing.T) {
	t.Parallel()
	never := func(int) bool { return false }
	only := func(want int) func(int) bool { return func(i int) bool { return i == want } }
	for _, tc := range []struct {
		name          string
		refs          []int
		discriminated func(int) bool
		want          int
		ok            bool
	}{
		{"no ref", nil, never, 0, false},
		{"the only ref, undiscriminated", []int{3}, never, 3, true},
		{"several, none discriminated", []int{1, 2}, never, 0, false},
		{"several, one discriminated", []int{1, 2}, only(2), 2, true},
		{"several discriminated, the first as ranked wins", []int{4, 2}, func(int) bool { return true }, 4, true},
	} {
		got, ok := SelectBase(tc.refs, tc.discriminated)
		assert.Equal(t, tc.ok, ok, tc.name)
		assert.Equal(t, tc.want, got, tc.name)
	}
}
