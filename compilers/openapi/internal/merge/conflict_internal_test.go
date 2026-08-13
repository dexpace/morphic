package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// The redeclaration-conflict helpers carry defensive guards for states a
// well-formed lowered document never reaches: a reference into no interned type,
// a base-less opaque scalar, and a cyclic base chain. These exercise those
// guards directly.

func TestResolvePrimKind_DanglingTargetIsNotResolved(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(nil)
	_, ok := g.resolvePrimKind(ir.TypeRef{Target: "t/missing"})
	assert.False(t, ok, "a target absent from the registry resolves to no primitive")
}

func TestResolvePrimKind_BaselessScalarIsNotResolved(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/opaque": &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}},
	})
	_, ok := g.resolvePrimKind(ir.TypeRef{Target: "t/opaque"})
	assert.False(t, ok, "a base-less opaque scalar has no underlying primitive")
}

func TestResolvePrimKind_CyclicBaseChainTerminates(t *testing.T) {
	t.Parallel()
	// A scalar whose Base points back at itself: the bounded walk must terminate
	// and report no primitive rather than spin.
	self := ir.TypeRef{Target: "t/cycle"}
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/cycle": &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/cycle"}, Base: &self},
	})
	_, ok := g.resolvePrimKind(self)
	assert.False(t, ok, "a cyclic base chain terminates without resolving")
}

func TestIsAnyType_DanglingTargetIsNotAny(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(nil)
	assert.False(t, g.isAnyType(ir.TypeRef{Target: "t/missing"}),
		"an unresolvable target is not classified as the top type")
}

func TestDifferentTypeKind_UnresolvableTargetIsNotAConflict(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/model": &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/model"}},
	})
	assert.False(t,
		g.differentTypeKind(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/missing"}),
		"an unresolvable target is not treated as a differing kind")
}

// Both BigVal keywords rest on the same magnitude comparison, so both are
// driven here over the literals that comparison has to get right: one value
// under two spellings, and two values that genuinely differ — mostly at a
// magnitude math/big will not build as a rational at all, with one in-range
// row so a comparison that only handled the extremes would still be caught.
//
// Every literal is built through ir.NewBigVal, which is the only way a bound
// reaches these helpers. A pair it refuses cannot arrive, so a row that used
// one would say nothing about what the compiler compares.
func TestBigValConflictDetails_CompareMagnitudesAtAnyScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		a, b         string
		wantConflict bool
	}{
		{name: "one value, two spellings", a: "1e1000001", b: "10e1000000"},
		{name: "one value, two spellings, in reverse", a: "10e1000000", b: "1e1000001"},
		{name: "one value against a rational's own range", a: "1e2", b: "100"},
		{name: "one exponent apart", a: "1e1000001", b: "1e1000002", wantConflict: true},
		{name: "one leading digit apart", a: "1e1000001", b: "2e1000001", wantConflict: true},
		{name: "a sign apart", a: "1e1000001", b: "-1e1000001", wantConflict: true},
		{name: "too small for a rational, and unequal", a: "1e-1000001", b: "2e-1000001", wantConflict: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, err := ir.NewBigVal(tc.a)
			require.NoError(t, err, "%q is a literal a schema may write", tc.a)
			b, err := ir.NewBigVal(tc.b)
			require.NoError(t, err, "%q is a literal a schema may write", tc.b)

			_, boundOK := boundConflictDetail("minimum", &a, &b, false, false)
			assert.Equal(t, tc.wantConflict, boundOK, "minimum %s against %s", a, b)

			_, multipleOK := multipleOfConflictDetail(&a, &b)
			assert.Equal(t, tc.wantConflict, multipleOK, "multipleOf %s against %s", a, b)
		})
	}
}

func TestIsStructuralType_DistinguishesCompositeFromOpaque(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/model":  &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/model"}},
		"t/list":   &ir.List{TypeCommon: ir.TypeCommon{ID: "t/list"}},
		"t/opaque": &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}},
		"t/string": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/string"}, Prim: ir.PrimString},
	})

	assert.True(t, g.isStructuralType(ir.TypeRef{Target: "t/model"}),
		"model is a structural type")
	assert.True(t, g.isStructuralType(ir.TypeRef{Target: "t/list"}),
		"list is a structural type")
	assert.False(t, g.isStructuralType(ir.TypeRef{Target: "t/opaque"}),
		"base-less opaque scalar is not structural")
	assert.False(t, g.isStructuralType(ir.TypeRef{Target: "t/string"}),
		"primitive is not structural")
	assert.False(t, g.isStructuralType(ir.TypeRef{Target: "t/missing"}),
		"unresolvable target is not structural")
}

func TestTypesConflict_OpaqueScalarVsPrimitiveIsNotConflict(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/opaque": &ir.Scalar{TypeCommon: ir.TypeCommon{ID: "t/opaque"}},
		"t/string": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/string"}, Prim: ir.PrimString},
	})

	assert.False(t, g.typesConflict(ir.TypeRef{Target: "t/opaque"}, ir.TypeRef{Target: "t/string"}),
		"opaque scalar vs primitive is not flagged (never guess)")
	assert.False(t, g.typesConflict(ir.TypeRef{Target: "t/string"}, ir.TypeRef{Target: "t/opaque"}),
		"primitive vs opaque scalar is not flagged (never guess)")
}

func TestTypesConflict_StructuralVsPrimitiveIsConflict(t *testing.T) {
	t.Parallel()
	g, _ := stubMerger(map[ir.TypeID]ir.TypeDef{
		"t/model":  &ir.Model{TypeCommon: ir.TypeCommon{ID: "t/model"}},
		"t/string": &ir.Primitive{TypeCommon: ir.TypeCommon{ID: "t/string"}, Prim: ir.PrimString},
	})

	assert.True(t, g.typesConflict(ir.TypeRef{Target: "t/model"}, ir.TypeRef{Target: "t/string"}),
		"model vs primitive is a provable conflict")
	assert.True(t, g.typesConflict(ir.TypeRef{Target: "t/string"}, ir.TypeRef{Target: "t/model"}),
		"primitive vs model is a provable conflict")
}

// The tests below reach constraintsConflict and mergeConstraints for keywords
// no OpenAPI spec can drive them through. ir.Constraints is one struct
// shared by every user of the type (properties and lists alike), and the
// OpenAPI compiler does not yet populate Precision, Scale, PatternMessage, or
// the list-owned MinItems/MaxItems/UniqueItems trio from any wire keyword —
// so no YAML spec can drive a redeclaration through those fields. The merge
// and conflict logic must still be correct for them.

func TestConstraintsConflict_PatternMessageConflictReported(t *testing.T) {
	t.Parallel()
	a := &ir.Constraints{PatternMessage: "must be alphanumeric"}
	b := &ir.Constraints{PatternMessage: "must match the legacy format"}
	detail, ok := constraintsConflict(a, b)
	require.True(t, ok, "differing patternMessage is a conflict")
	assert.Contains(t, detail, "patternMessage")
	assert.Contains(t, detail, "must be alphanumeric")
	assert.Contains(t, detail, "must match the legacy format")
}

func TestConstraintsConflict_UniqueItemsNeverConflicts(t *testing.T) {
	t.Parallel()
	// UniqueItems is a bare bool: false cannot be distinguished from "not set",
	// so a differing UniqueItems must never be reported as a conflict in
	// either direction.
	cases := []struct {
		name string
		a, b bool
	}{
		{"true vs false", true, false},
		{"false vs true", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, ok := constraintsConflict(&ir.Constraints{UniqueItems: tc.a}, &ir.Constraints{UniqueItems: tc.b})
			assert.False(t, ok, "differing uniqueItems must not be reported as a conflict")
		})
	}
}

func TestMergeConstraints_UniqueItemsIntersectsToTrue(t *testing.T) {
	t.Parallel()
	// allOf is an intersection: a collection satisfying both branches must be
	// unique whenever either branch requires it, so the merge always adopts
	// true and never downgrades true back to false.
	dst := mergeConstraints(&ir.Constraints{UniqueItems: false}, &ir.Constraints{UniqueItems: true})
	require.NotNil(t, dst)
	assert.True(t, dst.UniqueItems, "dst adopts true from src")

	dst = mergeConstraints(&ir.Constraints{UniqueItems: true}, &ir.Constraints{UniqueItems: false})
	require.NotNil(t, dst)
	assert.True(t, dst.UniqueItems, "dst is never downgraded from true to false")
}

func TestMergeConstraints_AdoptsEveryUnsetKeyword(t *testing.T) {
	t.Parallel()
	// Every keyword ir.Constraints carries: dst leaves each one unset, so the
	// merge must adopt all of them from src, not just the ones exercised by
	// the spec-driven table tests in the compiler package.
	five, ten := int64(5), int64(10)
	minVal, maxVal, multipleOf := ir.BigVal("1"), ir.BigVal("9"), ir.BigVal("2")
	src := &ir.Constraints{
		Min: &minVal, ExclusiveMin: true,
		Max: &maxVal, ExclusiveMax: true,
		MultipleOf:     &multipleOf,
		Precision:      &ten,
		Scale:          &five,
		MinLength:      &five,
		MaxLength:      &ten,
		Pattern:        "^a$",
		PatternMessage: "must start with a",
		MinItems:       &five,
		MaxItems:       &ten,
		UniqueItems:    true,
		MinProps:       &five,
		MaxProps:       &ten,
	}
	merged := mergeConstraints(&ir.Constraints{}, src)
	require.NotNil(t, merged)
	assert.Same(t, src.Min, merged.Min)
	assert.Equal(t, src.ExclusiveMin, merged.ExclusiveMin)
	assert.Same(t, src.Max, merged.Max)
	assert.Equal(t, src.ExclusiveMax, merged.ExclusiveMax)
	assert.Same(t, src.MultipleOf, merged.MultipleOf)
	assert.Same(t, src.Precision, merged.Precision)
	assert.Same(t, src.Scale, merged.Scale)
	assert.Same(t, src.MinLength, merged.MinLength)
	assert.Same(t, src.MaxLength, merged.MaxLength)
	assert.Equal(t, src.Pattern, merged.Pattern)
	assert.Equal(t, src.PatternMessage, merged.PatternMessage)
	assert.Same(t, src.MinItems, merged.MinItems)
	assert.Same(t, src.MaxItems, merged.MaxItems)
	assert.Equal(t, src.UniqueItems, merged.UniqueItems)
	assert.Same(t, src.MinProps, merged.MinProps)
	assert.Same(t, src.MaxProps, merged.MaxProps)
}

func TestMergeConstraints_LeavesSetKeywordsUntouched(t *testing.T) {
	t.Parallel()
	// dst already sets every keyword exercised here; src's differing values
	// must never overwrite dst's — first declaration wins, same as every
	// other reconcileProperty field.
	dstMin, srcMin := ir.BigVal("1"), ir.BigVal("99")
	dst := &ir.Constraints{Min: &dstMin, Pattern: "^dst$", PatternMessage: "dst message", UniqueItems: true}
	src := &ir.Constraints{Min: &srcMin, Pattern: "^src$", PatternMessage: "src message", UniqueItems: false}
	merged := mergeConstraints(dst, src)
	require.NotNil(t, merged)
	assert.Same(t, &dstMin, merged.Min, "dst's Min is kept, src's is not adopted")
	assert.Equal(t, "^dst$", merged.Pattern)
	assert.Equal(t, "dst message", merged.PatternMessage)
	assert.True(t, merged.UniqueItems, "true is never downgraded by a false src")
}

func TestMergeConstraints_NilOperands(t *testing.T) {
	t.Parallel()
	// A nil dst adopts src wholesale; a nil src leaves dst untouched; both nil
	// stays nil.
	full := &ir.Constraints{Pattern: "^a$"}
	assert.Same(t, full, mergeConstraints(nil, full), "nil dst adopts src wholesale")
	assert.Same(t, full, mergeConstraints(full, nil), "nil src leaves dst untouched")
	assert.Nil(t, mergeConstraints(nil, nil))
}

func TestConstraintsConflict_DeterministicKeywordOrder(t *testing.T) {
	t.Parallel()
	// minLength is checked before maxLength in constraintsConflict's fixed
	// order, so when both conflict at once the reported keyword is always
	// minLength, never maxLength.
	two, three := int64(2), int64(3)
	twenty, thirty := int64(20), int64(30)
	a := &ir.Constraints{MinLength: &two, MaxLength: &twenty}
	b := &ir.Constraints{MinLength: &three, MaxLength: &thirty}
	detail, ok := constraintsConflict(a, b)
	require.True(t, ok)
	assert.Contains(t, detail, "minLength", "the earlier-checked keyword is reported")
	assert.NotContains(t, detail, "maxLength", "the later-checked keyword is not also reported")
}
