package openapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

func TestAllOf_UntypedRedeclarationDoesNotConflict(t *testing.T) {
	t.Parallel()
	// One branch leaves the field schemaless (the top type), the other types it.
	// `any` intersects with everything under allOf, so this is a narrowing, not a
	// contradiction — it must not be reported.
	spec := componentSpec(`    Anyish:
      allOf:
        - type: object
          properties:
            id: {description: the identifier}
        - type: object
          properties:
            id: {type: integer}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	m, ok := typeByName(doc, "Anyish").(*ir.Model)
	require.True(t, ok, "Anyish should be a model")
	require.Len(t, m.Properties, 1, "id reconciles to one property")
	assert.Empty(t, conflictDiags(diags),
		"the schemaless top type never conflicts with a sibling redeclaration")
}

func TestAllOf_EquivalentNumericBoundsDoNotConflict(t *testing.T) {
	t.Parallel()
	// The same bound spelled two ways (10 and 10.0) denotes one value, so it must
	// compare equal by magnitude and stay silent.
	spec := componentSpec(`    Boundish:
      allOf:
        - type: object
          properties:
            n: {type: number, minimum: 10}
        - type: object
          properties:
            n: {type: number, minimum: 10.0}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"10 and 10.0 are the same numeric bound, not a conflict")
}

func TestAllOf_DifferingNumericBoundsConflict(t *testing.T) {
	t.Parallel()
	// Two branches pin the same lower bound to different magnitudes; the kept
	// winner is arbitrary source order, so the dropped bound is surfaced.
	spec := componentSpec(`    Boundish:
      allOf:
        - type: object
          properties:
            n: {type: integer, minimum: 5}
        - type: object
          properties:
            n: {type: integer, minimum: 10}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "differing numeric bounds are diagnosed once")
	assert.Contains(t, conflicts[0].Message, `"n"`)
}

func TestAllOf_ScalarVersusObjectRedeclarationConflicts(t *testing.T) {
	t.Parallel()
	// A scalar in one branch and a structural type in the other cannot both hold.
	spec := componentSpec(`    Mixed:
      allOf:
        - type: object
          properties:
            f: {type: string}
        - type: object
          properties:
            f:
              type: object
              properties:
                x: {type: string}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "a scalar against an object is diagnosed once")
	assert.Contains(t, conflicts[0].Message, `"f"`)
}

func TestAllOf_DistinctInlineObjectsDoNotConflict(t *testing.T) {
	t.Parallel()
	// Two branches redeclare the field as distinct inline objects. Each hoists its
	// own model at its own pointer, so the targets differ — but two objects of the
	// same kind are not provably contradictory, and conflict detection never
	// guesses.
	spec := componentSpec(`    Objish:
      allOf:
        - type: object
          properties:
            f:
              type: object
              properties:
                x: {type: string}
        - type: object
          properties:
            f:
              type: object
              properties:
                x: {type: string}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"two distinct inline objects for one field are not a provable conflict")
}

// allOfConflictSpec builds a component whose two inline allOf branches each
// declare field v with the given flow-style schemas, for exercising per-keyword
// redeclaration conflict detection.
func allOfConflictSpec(schemaA, schemaB string) string {
	return componentSpec(
		"    T:\n" +
			"      allOf:\n" +
			"        - type: object\n" +
			"          properties: {v: " + schemaA + "}\n" +
			"        - type: object\n" +
			"          properties: {v: " + schemaB + "}\n")
}

func TestAllOf_ConstraintAndFormatConflictsDiagnosed(t *testing.T) {
	t.Parallel()
	// Each keyword class that can contradict across branches: an inclusive vs
	// exclusive bound of equal magnitude, a differing pattern, a differing
	// multipleOf, and two format-derived primitives (string vs uuid). Each keeps
	// the first declaration but surfaces exactly one conflict naming the field,
	// both branch sites, and — for the constraint cases — the offending keyword
	// with both of its conflicting values, so the author never has to diff both
	// branches by hand.
	cases := []struct {
		name, a, b, wantDetail string
	}{
		{
			name:       "exclusive sense",
			a:          "{type: number, minimum: 10}",
			b:          "{type: number, exclusiveMinimum: 10}",
			wantDetail: "conflicting minimum (10 and exclusive 10)",
		},
		{
			name:       "pattern",
			a:          "{type: string, pattern: '^a$'}",
			b:          "{type: string, pattern: '^b$'}",
			wantDetail: `conflicting pattern ("^a$" and "^b$")`,
		},
		{
			name:       "multipleOf",
			a:          "{type: integer, multipleOf: 2}",
			b:          "{type: integer, multipleOf: 3}",
			wantDetail: "conflicting multipleOf (2 and 3)",
		},
		{
			name: "string vs uuid",
			a:    "{type: string}",
			b:    "{type: string, format: uuid}",
			// A type conflict, not a constraint one — checked via the
			// type-conflict message shape in TestAllOf_TypeConflictMessageNamesBothTypes.
			wantDetail: "incompatible types",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := lowerSpec(t, allOfConflictSpec(tc.a, tc.b))
			requireNoErrorDiags(t, diags)
			conflicts := conflictDiags(diags)
			require.Len(t, conflicts, 1, "%s is diagnosed exactly once", tc.name)
			assert.Contains(t, conflicts[0].Message, `"v"`, "the diagnostic names the field")
			assert.Contains(t, conflicts[0].Message, "allOf/0", "names the first branch site")
			assert.Contains(t, conflicts[0].Message, "allOf/1", "names the second branch site")
			assert.Contains(t, conflicts[0].Message, tc.wantDetail,
				"%s: the diagnostic names what actually differs", tc.name)
		})
	}
}

func TestAllOf_CompatibleConstraintRedeclarationsStaySilent(t *testing.T) {
	t.Parallel()
	// The false-positive guards for the constraint path: a keyword present on
	// only one branch (both branches still carry constraints) is intersected,
	// not a conflict — and the merge must genuinely carry both keywords
	// forward on the reconciled property, not just stay quiet about the one it
	// used to drop; equal multipleOf spelled two ways is one value; and an
	// unknown-format scalar resolves through its Base to the same primitive as
	// the plain type, so it is not a type conflict.
	cases := []struct {
		name         string
		a, b         string
		assertMerged func(t *testing.T, c *ir.Constraints)
	}{
		{
			name: "one-sided keywords",
			a:    "{type: string, maxLength: 10}",
			b:    "{type: string, minLength: 2}",
			assertMerged: func(t *testing.T, c *ir.Constraints) {
				t.Helper()
				require.NotNil(t, c)
				require.NotNil(t, c.MaxLength, "the first branch's maxLength is kept")
				require.NotNil(t, c.MinLength, "the second branch's minLength is adopted, not dropped")
				assert.Equal(t, int64(10), *c.MaxLength)
				assert.Equal(t, int64(2), *c.MinLength)
			},
		},
		{
			name: "one-sided pattern",
			a:    "{type: string, maxLength: 10}",
			b:    "{type: string, pattern: '^a'}",
			assertMerged: func(t *testing.T, c *ir.Constraints) {
				t.Helper()
				require.NotNil(t, c)
				require.NotNil(t, c.MaxLength, "the first branch's maxLength is kept")
				assert.Equal(t, int64(10), *c.MaxLength)
				assert.Equal(t, "^a", c.Pattern, "the second branch's pattern is adopted, not dropped")
			},
		},
		{
			name: "min and exclusiveMin adopted together",
			a:    "{type: number, multipleOf: 2}",
			b:    "{type: number, exclusiveMinimum: 5}",
			assertMerged: func(t *testing.T, c *ir.Constraints) {
				t.Helper()
				require.NotNil(t, c)
				require.NotNil(t, c.Min, "the second branch's exclusiveMinimum is adopted as Min")
				assert.Equal(t, "5", c.Min.String())
				assert.True(t, c.ExclusiveMin,
					"ExclusiveMin travels with the adopted Min, not left at its false zero value")
			},
		},
		{name: "equivalent multipleOf", a: "{type: number, multipleOf: 2}", b: "{type: number, multipleOf: 2.0}"},
		{name: "custom format over base", a: "{type: string, format: weird}", b: "{type: string}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, allOfConflictSpec(tc.a, tc.b))
			requireNoErrorDiags(t, diags)
			assert.Empty(t, conflictDiags(diags), "%s must not be reported as a conflict", tc.name)
			if tc.assertMerged == nil {
				return
			}
			m, ok := typeByName(doc, "T").(*ir.Model)
			require.True(t, ok, "T should be a model")
			require.Len(t, m.Properties, 1, "v reconciles to one property")
			tc.assertMerged(t, m.Properties[0].Constraints)
		})
	}
}

// The tests below call constraintsConflict and mergeConstraints directly
// rather than through an OpenAPI spec fixture. ir.Constraints is one struct
// shared by every user of the type (properties and lists alike), and the
// OpenAPI compiler does not yet populate Precision, Scale, PatternMessage, or
// the list-owned MinItems/MaxItems/UniqueItems trio from any wire keyword —
// so no YAML spec in this package can drive a redeclaration through those
// fields. The merge and conflict logic must still be correct for them.

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
	// the OpenAPI-spec-driven table tests above.
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

func TestAllOf_DiscriminatorHierarchy(t *testing.T) {
	t.Parallel()
	spec := componentSpecVer("3.2.0", `    Pet:
      type: object
      discriminator:
        propertyName: petType
        mapping:
          cat: '#/components/schemas/Cat'
        defaultMapping: '#/components/schemas/Dog'
      properties: {petType: {type: string}}
    Extra:
      type: object
      properties: {x: {type: string}}
    Cat:
      allOf:
        - {$ref: '#/components/schemas/Pet'}
        - {type: object, properties: {meow: {type: boolean}}}
    Dog:
      allOf:
        - {$ref: '#/components/schemas/Pet'}
        - {$ref: '#/components/schemas/Extra'}
        - {type: object, properties: {bark: {type: boolean}}}
`)
	doc, _ := lowerSpec(t, spec)

	pet := typeByName(doc, "Pet").(*ir.Model)
	require.NotNil(t, pet.Discriminator)
	assert.NotEmpty(t, pet.Discriminator.Property, "declared petType resolves to a PropID")
	assert.NotEmpty(t, pet.Discriminator.Mapping)
	assert.NotEmpty(t, pet.Discriminator.Default, "defaultMapping resolved")

	cat := typeByName(doc, "Cat").(*ir.Model)
	require.NotNil(t, cat.Base)
	assert.Equal(t, "cat", cat.DiscriminatorValue, "mapping key wins")

	dog := typeByName(doc, "Dog").(*ir.Model)
	require.NotNil(t, dog.Base, "the discriminator-anchoring ref is the base")
	require.Len(t, dog.Mixins, 1, "the second ref becomes a mixin")
	assert.Equal(t, "Dog", dog.DiscriminatorValue, "falls back to schema name")
}

func TestModelDiscriminator_UndeclaredPropertyAndBadMapping(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Vehicle:
      type: object
      discriminator:
        propertyName: kind
        mapping:
          car: '#'
      properties: {name: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	v := typeByName(doc, "Vehicle").(*ir.Model)
	require.NotNil(t, v.Discriminator)
	assert.Empty(t, v.Discriminator.Property, "undeclared property")
	assert.Equal(t, "kind", v.Discriminator.PropertyName)
	var sawBad bool
	for _, d := range diags {
		if d.Code == codeUnresolvedRef {
			sawBad = true
		}
	}
	assert.True(t, sawBad, "bad mapping target diagnostic")
}

func TestOneOf_DiscriminatorWithDefault(t *testing.T) {
	t.Parallel()
	spec := componentSpecVer("3.2.0", `    Shape:
      oneOf:
        - {$ref: '#/components/schemas/Circle'}
        - {$ref: '#/components/schemas/Square'}
      discriminator:
        propertyName: shapeType
        mapping: {circle: '#/components/schemas/Circle'}
        defaultMapping: '#/components/schemas/Square'
    Circle: {type: object, properties: {r: {type: number}}}
    Square: {type: object, properties: {s: {type: number}}}
`)
	doc, _ := lowerSpec(t, spec)
	u := typeByName(doc, "Shape").(*ir.Union)
	require.NotNil(t, u.Discriminator)
	assert.Equal(t, "shapeType", u.Discriminator.PropertyName)
	assert.NotEmpty(t, u.Discriminator.Default)
}

func TestAnyOf_ThreeVariantsWithNull(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    N:
      anyOf:
        - {type: string}
        - {type: integer}
        - {type: 'null'}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	u := typeByName(doc, "N").(*ir.Union)
	assert.Len(t, u.Variants, 2, "null branch stripped from variants")
	assert.False(t, u.Exclusive, "anyOf is not exclusive")
}

func TestUnion_VariantHints(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    U:
      oneOf:
        - {$ref: '#/components/schemas/Named', description: sibling}
        - {type: string}
    Named: {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	u := typeByName(doc, "U").(*ir.Union)
	require.Len(t, u.Variants, 2)
	hints := []string{u.Variants[0].Name.Hint, u.Variants[1].Name.Hint}
	assert.Contains(t, hints, "Named", "ref-with-siblings hint from target name")
	assert.Contains(t, hints, "variant_1", "inline branch positional hint")
}

func TestAllOf_UnresolvedRefBranch(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Bad:
      allOf:
        - {$ref: '#'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	var sawUnresolved bool
	for _, d := range diags {
		if d.Code == codeUnresolvedRef {
			sawUnresolved = true
		}
	}
	assert.True(t, sawUnresolved)
}

func TestAllOf_MultiRefWithUnresolvedDoesNotAnchor(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    Base:
      type: object
      discriminator: {propertyName: t}
      properties: {t: {type: string}}
    Sub:
      allOf:
        - {$ref: '#/components/schemas/Ghost'}
        - {$ref: '#/components/schemas/Base'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	sub := typeByName(doc, "Sub").(*ir.Model)
	require.NotNil(t, sub.Base, "the discriminator-anchoring Base becomes base despite an unresolved sibling ref")
	assert.Equal(t, "Sub", sub.DiscriminatorValue)
	_ = diags
}

func TestEnum_ValueTypeVariants(t *testing.T) {
	t.Parallel()
	spec := componentSpec(`    EInt: {type: integer, enum: [1, 2]}
    ENum: {type: number, enum: [1.5, 2.5]}
    EBool: {type: boolean, enum: [true, false]}
    ENoTypeBool: {enum: [true, false]}
    ENoTypeNum: {enum: [1, 2]}
    ENoTypeStr: {enum: [a, b]}
    EBytes: {enum: [!!binary aGk=]}
`)
	doc, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	want := map[string]ir.PrimKind{
		"EInt": ir.PrimInteger, "ENum": ir.PrimNumber, "EBool": ir.PrimBool,
		"ENoTypeBool": ir.PrimBool, "ENoTypeNum": ir.PrimNumber,
		"ENoTypeStr": ir.PrimString, "EBytes": ir.PrimString,
	}
	for name, prim := range want {
		e, ok := typeByName(doc, name).(*ir.Enum)
		require.True(t, ok, "%s is an enum", name)
		assert.Equal(t, prim, e.ValueType, "%s value type", name)
	}
}

func TestAllOf_OpaqueScalarVsPrimitiveNoConflict(t *testing.T) {
	t.Parallel()
	// An opaque scalar (format without a base type) is unknown, not structural,
	// so it's not provably incompatible with a primitive. The "never guess"
	// principle means we don't flag this as a conflict.
	spec := componentSpec(`    OpaqueTest:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {format: custom}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"opaque scalar vs primitive is not flagged as a conflict")
}

func TestAllOf_ThreeWayRedeclarationProducesTwoDiagnostics(t *testing.T) {
	t.Parallel()
	// When three allOf branches declare the same field with different types,
	// reconciliation runs twice: branch[1] vs branch[0], then branch[2] vs branch[0].
	// Each incompatible pair produces one diagnostic, so we expect two total.
	spec := componentSpec(`    ThreeWay:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: integer}
        - type: object
          properties:
            id: {type: boolean}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 2, "three-way redeclaration produces two diagnostics")
}

func TestAllOf_ThreeWayCompatibleRedeclarationStaysSilent(t *testing.T) {
	t.Parallel()
	// When three allOf branches declare the same field with compatible types
	// (all the same), no conflict is reported.
	spec := componentSpec(`    ThreeWayCompat:
      allOf:
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: string}
        - type: object
          properties:
            id: {type: string}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	assert.Empty(t, conflictDiags(diags),
		"three-way compatible redeclaration stays silent")
}

func TestAllOf_SatisfiableNarrowingsStaySilent(t *testing.T) {
	t.Parallel()
	// Each of these narrows a base type to a stricter but still-satisfiable
	// shape under allOf intersection, so none is a provable contradiction:
	//   - an enum of the same primitive the base type declares,
	//   - a const pinning one legal value of the base type,
	//   - a multi-type union narrowed down to one of its own member types.
	// isStructuralType no longer treats Enum, Union, or Literal as provably
	// non-scalar, so a bare scalar sibling against one of these is never
	// flagged (see the false-positive fix on resolvePrimKind/isStructuralType).
	cases := []struct {
		name, a, b string
	}{
		{"enum narrows base type", "{type: string}", "{type: string, enum: [active, inactive]}"},
		{"const narrows base type", "{type: string}", "{type: string, const: dog}"},
		{"union narrowed to one member", "{type: [string, integer]}", "{type: string}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := lowerSpec(t, allOfConflictSpec(tc.a, tc.b))
			requireNoErrorDiags(t, diags)
			assert.Empty(t, conflictDiags(diags), "%s must not be reported as a conflict", tc.name)
		})
	}
}

func TestAllOf_EnumVersusIncompatibleTypeStillConflicts(t *testing.T) {
	t.Parallel()
	// Guard against over-correcting the enum false-positive fix: an enum whose
	// declared ValueType is string still provably conflicts with an
	// incompatible primitive (integer). Enum now resolves via resolvePrimKind
	// instead of being treated as opaque structural data, so this comparison
	// goes through the same aok&&bok path as two plain scalars.
	_, diags := lowerSpec(t, allOfConflictSpec(
		"{type: string, enum: [active, inactive]}", "{type: integer}"))
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "enum of strings vs integer is still a provable conflict")
	assert.Contains(t, conflicts[0].Message, `"v"`, "the diagnostic names the conflicting field")
}

func TestAllOf_TypeConflictMessageNamesBothTypes(t *testing.T) {
	t.Parallel()
	// The type-conflict diagnostic must say what actually differs, not just
	// that something did: the two conflicting type identities, so the author
	// can see at a glance what disagreed without cross-referencing the spec.
	_, diags := lowerSpec(t, allOfConflictSpec("{type: string}", "{type: integer}"))
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1)
	assert.Contains(t, conflicts[0].Message, "t/prim/string", "names the first branch's type")
	assert.Contains(t, conflicts[0].Message, "t/prim/integer", "names the second branch's type")
}

func TestAllOf_PropertyAlongsideAllOfConflictMessageIsAccurate(t *testing.T) {
	t.Parallel()
	// A property declared directly on the schema alongside allOf redeclares a
	// field the inline branch also declares. The redeclaration's provenance
	// pointer here is a plain "properties/id" path, never an "allOf/N" branch,
	// so the message must not claim "allOf branches redeclare" — it must read
	// correctly for a co-declared sibling property too (mergeProperty folds
	// both cases the same way; see its doc comment).
	spec := componentSpec(`    Along:
      type: object
      properties:
        id: {type: string}
      allOf:
        - type: object
          properties:
            id: {type: integer}
`)
	_, diags := lowerSpec(t, spec)
	requireNoErrorDiags(t, diags)
	conflicts := conflictDiags(diags)
	require.Len(t, conflicts, 1, "the co-declared property conflict is diagnosed once")
	d := conflicts[0]
	assert.Contains(t, d.Message, `"id"`, "the diagnostic names the conflicting field")
	assert.NotContains(t, d.Message, "allOf branches",
		"the redeclaration site is a co-declared property, not an allOf branch")
	assert.Equal(t, "/components/schemas/Along/properties/id", d.Provenance.Pointer,
		"the diagnostic's own site is the co-declared property, not an allOf branch")
}

func TestEnum_HeterogeneousBecomesUnionWithBadValue(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    Mixed:\n      enum: [active, .inf]\n")
	doc, diags := lowerSpec(t, spec)
	u, ok := typeByName(doc, "Mixed").(*ir.Union)
	require.True(t, ok, "heterogeneous enum lowers to a union of literals")
	assert.Len(t, u.Variants, 2)
	var sawDegraded, sawValueWarn bool
	for _, d := range diags {
		if d.Code == codeDegradedConstruct && d.Severity == ir.SeverityInfo {
			sawDegraded = true
		}
		if d.Severity == ir.SeverityWarning {
			sawValueWarn = true
		}
	}
	assert.True(t, sawDegraded, "heterogeneous-enum info diagnostic")
	assert.True(t, sawValueWarn, "unconvertible literal value warning")
}
