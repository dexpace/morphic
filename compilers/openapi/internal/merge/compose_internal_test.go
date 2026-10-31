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
// declaration.
func composeRegistry() map[ir.TypeID]ir.TypeDef {
	own := []ir.Property{{WireName: "own"}}
	inline := func(id ir.TypeID, pointer jsontext.Pointer) *ir.Model {
		return &ir.Model{ID: id, Anonymous: true, Provenance: ptrAt(pointer), Properties: own}
	}
	return map[ir.TypeID]ir.TypeDef{
		"t/A":      &ir.Model{ID: "t/A", Provenance: ptrAt("/components/schemas/A"), Properties: own},
		"t/B":      &ir.Model{ID: "t/B", Provenance: ptrAt("/components/schemas/B"), Properties: own},
		"t/sub":    &ir.Model{ID: "t/sub", Provenance: ptrAt("/components/schemas/Sub"), Base: &ir.TypeRef{Target: "t/A"}},
		"t/bare":   &ir.Model{ID: "t/bare", Anonymous: true, Provenance: ptrAt(declaredAt(0))},
		"t/bare1":  &ir.Model{ID: "t/bare1", Anonymous: true, Provenance: ptrAt(declaredAt(1))},
		"t/Pet":    &ir.Model{ID: "t/Pet", Provenance: ptrAt("/components/schemas/Pet"), Properties: own, Discriminator: &ir.Discriminator{}},
		"t/Dog":    &ir.Model{ID: "t/Dog", Provenance: ptrAt("/components/schemas/Dog"), Properties: own, Discriminator: &ir.Discriminator{}},
		"t/alias":  &ir.Scalar{ID: "t/alias", Base: &ir.TypeRef{Target: "t/B"}},
		"t/in0":    inline("t/in0", declaredAt(0)),
		"t/in1":    inline("t/in1", declaredAt(1)),
		"t/in2":    inline("t/in2", declaredAt(2)),
		"t/nested": inline("t/nested", declaredAt(1)+"/properties/x"),
		"t/str":    &ir.Primitive{ID: "t/str", Prim: ir.PrimString},
		"t/top":    &ir.Primitive{ID: "t/top", Prim: ir.PrimAny},
		"t/any":    &ir.Any{ID: "t/any"},
		"t/map":    &ir.MapT{ID: "t/map"},
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
		prop:   ir.Property{WireName: "data", Type: ir.TypeRef{Target: target}, Provenance: ptrAt(declaredAt(n))},
		source: written(`{}`),
	}
}

// declsOf is one declaration of each target, in order.
func declsOf(targets ...ir.TypeID) []declaration {
	out := make([]declaration, len(targets))
	for i, t := range targets {
		out[i] = nthDecl(i, t)
	}
	return out
}

// TestFold_RedeclaredModelsCompose pins what a field declared as several models
// takes: a model the fold mints, composing each once as a written allOf of them
// does. Nothing is lost, so nothing is kept and nothing reported, and a bare
// `{}` beside the models adds no conjunct.
func TestFold_RedeclaredModelsCompose(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		targets []ir.TypeID
		base    ir.TypeID
		mixins  []ir.TypeID
	}{
		{name: "a component, then an inline object", targets: []ir.TypeID{"t/A", "t/in1"}, base: "t/A", mixins: []ir.TypeID{"t/in1"}},
		{name: "an inline object, then a component", targets: []ir.TypeID{"t/in0", "t/B"}, base: "t/B", mixins: []ir.TypeID{"t/in0"}},
		{name: "two inline objects stay mixins", targets: []ir.TypeID{"t/in0", "t/in1"}, mixins: []ir.TypeID{"t/in0", "t/in1"}},
		{name: "two components stay mixins, in order", targets: []ir.TypeID{"t/B", "t/A"}, mixins: []ir.TypeID{"t/B", "t/A"}},
		{name: "a discriminated component is the base", targets: []ir.TypeID{"t/A", "t/Pet"}, base: "t/Pet", mixins: []ir.TypeID{"t/A"}},
		{name: "the first discriminated component is", targets: []ir.TypeID{"t/Dog", "t/A", "t/Pet"}, base: "t/Dog", mixins: []ir.TypeID{"t/A", "t/Pet"}},
		{name: "one type declared twice composes once", targets: []ir.TypeID{"t/A", "t/B", "t/A"}, mixins: []ir.TypeID{"t/A", "t/B"}},
		{name: "`{}` adds no conjunct", targets: []ir.TypeID{"t/top", "t/A", "t/in2"}, base: "t/A", mixins: []ir.TypeID{"t/in2"}},
		{name: "a bare object adds no conjunct", targets: []ir.TypeID{"t/bare", "t/A", "t/in2"}, base: "t/A", mixins: []ir.TypeID{"t/in2"}},
		{name: "a type what composes it holds adds no conjunct", targets: []ir.TypeID{"t/A", "t/B", "t/sub"}, mixins: []ir.TypeID{"t/B", "t/sub"}},
		{name: "an alias composes as written", targets: []ir.TypeID{"t/alias", "t/in1"}, base: "t/alias", mixins: []ir.TypeID{"t/in1"}},
		{name: "a node under its declaration is inline", targets: []ir.TypeID{"t/in0", "t/nested"}, mixins: []ir.TypeID{"t/in0", "t/nested"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := composeRegistry()
			g, recorded := stubMerger(reg)

			got := g.fold(declsOf(tc.targets...))

			require.Equal(t, composedAt(declaredAt(1)), got.Type.Target, "the field takes the minted model")
			m, ok := reg[got.Type.Target].(*ir.Model)
			require.True(t, ok, "the minted model is registered")
			assert.Empty(t, cmp.Diff(tc.mixins, targetsOf(m.Mixins)), "mixins")
			if tc.base == "" {
				assert.Nil(t, m.Base)
			} else {
				require.NotNil(t, m.Base)
				assert.Equal(t, tc.base, m.Base.Target)
			}
			assert.Empty(t, m.Properties, "the composition stays in source form")
			assert.Empty(t, *recorded, "nothing is lost, so nothing is reported")
			assert.Empty(t, got.Unmodeled, "and nothing is kept")
		})
	}
}

// targetsOf lists what refs name.
func targetsOf(refs []ir.TypeRef) []ir.TypeID {
	out := make([]ir.TypeID, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Target)
	}
	return out
}

// TestFold_OnlyTwoOrMoreModelsCompose pins where the type stays the winner's:
// one type holds every other, or a declaration that remains is no model, so the
// fold keeps what it did and mints nothing.
func TestFold_OnlyTwoOrMoreModelsCompose(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		targets []ir.TypeID
		want    ir.TypeID
		dropped []jsontext.Pointer
	}{
		{name: "one model twice", targets: []ir.TypeID{"t/A", "t/A"}, want: "t/A"},
		{name: "a model and what composes it", targets: []ir.TypeID{"t/A", "t/sub"}, want: "t/sub"},
		{name: "what composes a model, then the model", targets: []ir.TypeID{"t/sub", "t/A"}, want: "t/sub"},
		{name: "a model and an alias of it", targets: []ir.TypeID{"t/B", "t/alias"}, want: "t/alias"},
		{name: "a bare object and a model", targets: []ir.TypeID{"t/bare", "t/A"}, want: "t/A"},
		{name: "a model and a bare object", targets: []ir.TypeID{"t/A", "t/bare1"}, want: "t/A"},
		{name: "two bare objects written inline", targets: []ir.TypeID{"t/bare", "t/bare1"}, want: "t/bare"},
		{name: "`{}` loses to one model", targets: []ir.TypeID{"t/top", "t/A"}, want: "t/A"},
		{name: "a kind beside two models", targets: []ir.TypeID{"t/A", "t/B", "t/str"}, want: "t/A", dropped: []jsontext.Pointer{declaredAt(1), declaredAt(2)}},
		{name: "a degraded node beside two models", targets: []ir.TypeID{"t/any", "t/A", "t/B"}, want: "t/any", dropped: []jsontext.Pointer{declaredAt(1), declaredAt(2)}},
		{name: "a map beside two models", targets: []ir.TypeID{"t/A", "t/B", "t/map"}, want: "t/A", dropped: []jsontext.Pointer{declaredAt(1), declaredAt(2)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := composeRegistry()
			count := len(reg)
			g, _ := stubMerger(reg)

			got := g.fold(declsOf(tc.targets...))

			assert.Equal(t, tc.want, got.Type.Target)
			assert.Len(t, reg, count, "nothing is minted")
			want := make([]string, 0, len(tc.dropped))
			for _, p := range tc.dropped {
				want = append(want, losingDeclarationKey+string(p))
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
		g, _ := stubMerger(reg)
		decls := declsOf("t/A", "t/in1")
		decls[0].prop.Type.Nullable, decls[1].prop.Type.Nullable = tc.a, tc.b

		got := g.fold(decls)

		assert.Equal(t, tc.want, got.Type.Nullable, tc.name)
		m, ok := reg[got.Type.Target].(*ir.Model)
		require.True(t, ok, tc.name)
		require.NotNil(t, m.Base, tc.name)
		assert.False(t, m.Base.Nullable, "%s: the base carries no bit", tc.name)
		assert.False(t, m.Mixins[0].Nullable, "%s: nor does a mixin", tc.name)
	}
}

// TestFold_AComposedFieldKeepsEveryDeclarationsDetail pins that no declaration
// is dropped: each is in the cone, so a constraint any of them writes reaches
// the field, and one contradicting another is reported as for any other fold.
func TestFold_AComposedFieldKeepsEveryDeclarationsDetail(t *testing.T) {
	t.Parallel()
	g, recorded := stubMerger(composeRegistry())
	decls := declsOf("t/A", "t/B", "t/in1")
	decls[0].prop.Constraints = &ir.Constraints{MinProps: new(int64(1))}
	decls[1].prop.Constraints = &ir.Constraints{MaxProps: new(int64(4))}
	decls[2].prop.Constraints = &ir.Constraints{MinProps: new(int64(2))}

	got := g.fold(decls)

	require.NotNil(t, got.Constraints)
	assert.Equal(t, int64(1), *got.Constraints.MinProps, "the first to set a keyword keeps it")
	assert.Equal(t, int64(4), *got.Constraints.MaxProps, "a later declaration's keyword reaches the field")
	require.Len(t, *recorded, 1)
	assert.Equal(t, diag.ConflictingRedecl, (*recorded)[0].Code)
	assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(2)))
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
			g, recorded := stubMerger(composeRegistry())
			decls := declsOf("t/A", "t/in1")
			decls[0].prop.Type.Nullable, decls[1].prop.Type.Nullable = tc.nullable, tc.nullable
			decls[tc.at].prop.Default = &tc.value

			got := g.fold(decls)

			if tc.adopted {
				assert.Equal(t, &tc.value, got.Default)
				assert.Empty(t, *recorded)
				return
			}
			assert.Nil(t, got.Default)
			require.Len(t, *recorded, 1)
			assert.Equal(t, diag.DegradedConstruct, (*recorded)[0].Code)
			assert.Contains(t, (*recorded)[0].Message, "admits the default this declaration gives")
			assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(tc.at)))
		})
	}
}

// TestFold_ComposesOnlyTheDeclarationsItJudges pins the bound on what composes:
// past maxFoldDeclarations a declaration is kept unjudged, so it is no conjunct.
func TestFold_ComposesOnlyTheDeclarationsItJudges(t *testing.T) {
	t.Parallel()
	reg := composeRegistry()
	g, recorded := stubMerger(reg)
	decls := make([]declaration, 0, maxFoldDeclarations+1)
	for range maxFoldDeclarations {
		decls = append(decls, nthDecl(0, "t/A"))
	}
	decls[1] = nthDecl(1, "t/B")
	decls = append(decls, nthDecl(2, "t/Pet"))

	got := g.fold(decls)

	m, ok := reg[got.Type.Target].(*ir.Model)
	require.True(t, ok)
	assert.Empty(t, cmp.Diff([]ir.TypeID{"t/A", "t/B"}, targetsOf(m.Mixins)), "the declaration past the bound composes nothing")
	require.Len(t, *recorded, 1)
	assert.Equal(t, diag.BudgetExceeded, (*recorded)[0].Code)
	assert.Contains(t, got.Unmodeled, losingDeclarationKey+string(declaredAt(2)))
}

// TestSelectBase_ChoosesAsTheRuleSays pins the rule both readers take: the only
// $ref, else the first discriminated one, else none.
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
		{"several discriminated, the first wins", []int{4, 2}, func(int) bool { return true }, 4, true},
	} {
		got, ok := SelectBase(tc.refs, tc.discriminated)
		assert.Equal(t, tc.ok, ok, tc.name)
		assert.Equal(t, tc.want, got, tc.name)
	}
}
