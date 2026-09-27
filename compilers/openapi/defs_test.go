package openapi

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// propByWire looks up m's property by wire name. This file's tests are
// package openapi (they read ids-derived TypeIDs directly, which only an
// internal test file can import alongside compile), so they cannot reach
// conformance_test.go's identically-named helper in package openapi_test;
// this wraps openapitest.PropsByWire's map in the same two-value form instead
// of duplicating its logic.
func propByWire(m *ir.Model, wire string) (ir.Property, bool) {
	p, ok := openapitest.PropsByWire(m.Properties)[wire]
	return p, ok
}

// twoReversedSpec is testdata/openapi/two.yaml (GitHub #557's repro) with A and
// B's declaration order swapped. It is not itself committed to the corpus: the
// harness's own order-invariance oracle already reverses every committed
// spec's mappings automatically, so a second file that only reorders one would
// duplicate what that oracle already runs. This test wants something the
// oracle does not give it — a concrete cmp.Diff plus per-property assertions on
// each order, not merely that the two agree.
const twoReversedSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    B:
      type: object
      properties:
        q: {$ref: "#/$defs/n"}
      $defs:
        n: {type: object, properties: {y: {type: integer}}}
    A:
      type: object
      properties:
        p: {$ref: "#/$defs/n"}
      $defs:
        n: {type: object, properties: {x: {type: string}}}
`

// TestCompile_DefsPointerRelativeToItsOwnSchema is the compiler-level
// regression for GitHub #557: two component schemas that each $ref their own
// "#/$defs/n" must resolve to their OWN definition — never the other's,
// whichever is declared first — in either declaration order, and the two
// orders must intern byte-identical types. Before the fix, the resolver
// cached the first definition it found for the pointer and handed it to the
// second schema's reference too, so which schema's shape "won" depended on
// declaration order.
func TestCompile_DefsPointerRelativeToItsOwnSchema(t *testing.T) {
	t.Parallel()
	forward := readReproducer(t, "two")

	docForward, diagsForward := parseFull(t, string(forward))
	openapitest.RequireNoErrorDiags(t, diagsForward)
	docReversed, diagsReversed := parseFull(t, twoReversedSpec)
	openapitest.RequireNoErrorDiags(t, diagsReversed)

	for _, doc := range []*ir.Document{docForward, docReversed} {
		assertOwnDefsProperty(t, doc, "A", "p", "x")
		assertOwnDefsProperty(t, doc, "B", "q", "y")
	}

	if d := cmp.Diff(docForward.Types, docReversed.Types); d != "" {
		t.Errorf("declaration order must not change identity (-forward +reversed):\n%s", d)
	}
}

// assertOwnDefsProperty requires that the named component's own property (by
// wire name) resolves directly to a Model interned at that SAME component's
// own "$defs/n" position, carrying wantProp — proof the reference landed on
// the schema's own definition rather than a sibling's (GitHub #557). A model
// property is a CarriedRef position, so a bare $ref (no siblings, as every
// property here is) resolves directly to the target with no alias in between.
func assertOwnDefsProperty(t *testing.T, doc *ir.Document, component, prop, wantProp string) {
	t.Helper()
	m, ok := doc.Types[componentID(component)].(*ir.Model)
	require.True(t, ok, "%s owns a Model node", component)
	p, ok := propByWire(m, prop)
	require.True(t, ok, "%s declares property %q", component, prop)

	wantID := ids.ForPointer(ids.Ptr("components", "schemas", component, "$defs", "n"))
	require.Equal(t, wantID, p.Type.Target,
		"%s.%s must resolve to %s's own $defs/n, not a sibling's", component, prop, component)

	target, ok := doc.Types[p.Type.Target].(*ir.Model)
	require.True(t, ok, "the definition itself is a Model")
	_, ok = propByWire(target, wantProp)
	assert.True(t, ok, "%s's own definition declares %q; got %+v", component, wantProp, target.Properties)
}

// f13ReadersSpec is GitHub #557's f13: two sibling components each $ref their
// own "#/$defs/n", one a plain string and the other an "integer|null" union,
// each carrying its own description. Both definitions reduce to the shared
// primitive scalar, which has nowhere of its own to hold a description or a
// null bit, so both merge onto the referencing property from whichever
// schema's OWN definition the reference resolves to (fillPropertyAnnotations,
// refNullable). Before the fix, a collapsed identity could read one sibling's
// nullability or description onto the other's property with no diagnostic.
const f13ReadersSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        p: {$ref: "#/$defs/n"}
      $defs:
        n: {type: string, description: from-A}
    B:
      type: object
      properties:
        q: {$ref: "#/$defs/n"}
      $defs:
        n: {type: ["integer", "null"], description: from-B}
`

func TestCompile_DefsReferenceMergesTargetsOwnNullabilityAndDescription(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, f13ReadersSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	a, ok := doc.Types[componentID("A")].(*ir.Model)
	require.True(t, ok)
	p, ok := propByWire(a, "p")
	require.True(t, ok)
	assert.False(t, p.Type.Nullable, "A's own $defs/n is a plain string, not a union with null")
	assert.Equal(t, "from-A", descriptionOf(doc, p), "p must read A's own definition, not B's")

	b, ok := doc.Types[componentID("B")].(*ir.Model)
	require.True(t, ok)
	q, ok := propByWire(b, "q")
	require.True(t, ok)
	assert.True(t, q.Type.Nullable, "B's own $defs/n admits null")
	assert.Equal(t, "from-B", descriptionOf(doc, q), "q must read B's own definition, not A's")
}

// descriptionOf returns p's own description when the property carries one
// directly, or — when p's type reduced to a shared primitive with nowhere of
// its own to hold it — the description merged onto p.Docs from the reference's
// resolved target. Whichever field this compiler attaches it to, it must be
// the referenced schema's OWN description, never a sibling's; this reads
// whichever field is populated rather than assuming one, so the assertion
// survives a refactor of exactly where the merge lands.
func descriptionOf(doc *ir.Document, p ir.Property) string {
	if p.Docs.Description != "" {
		return p.Docs.Description
	}
	if td, ok := doc.Types[p.Type.Target]; ok {
		return td.Common().Docs.Description
	}
	return ""
}

// f12MappingSpec is GitHub #557's f12: a discriminated oneOf whose mapping
// values are "#/$defs/..." pointers naming Pet's own sibling definitions — the
// same definitions its oneOf branches already $ref. Before the fix, both
// mapping values could resolve to whichever definition the resolver's cache
// happened to hold, typing "dog" with cat's shape or the reverse.
const f12MappingSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Pet:
      oneOf: [{$ref: "#/$defs/cat"}, {$ref: "#/$defs/dog"}]
      discriminator: {propertyName: kind, mapping: {cat: "#/$defs/cat", dog: "#/$defs/dog"}}
      $defs:
        cat: {type: object, required: [kind], properties: {kind: {type: string}, meow: {type: string}}}
        dog: {type: object, required: [kind], properties: {kind: {type: string}, bark: {type: string}}}
`

func TestCompile_DefsDiscriminatorMappingNamesItsOwnSiblingDefinition(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, f12MappingSpec)
	openapitest.RequireNoErrorDiags(t, diags)

	u, ok := doc.Types[componentID("Pet")].(*ir.Union)
	require.True(t, ok, "a discriminated oneOf lowers to a Union")
	require.NotNil(t, u.Discriminator)

	wantCat := ids.ForPointer(ids.Ptr("components", "schemas", "Pet", "$defs", "cat"))
	wantDog := ids.ForPointer(ids.Ptr("components", "schemas", "Pet", "$defs", "dog"))

	catID, ok := u.Discriminator.Mapping["cat"]
	require.True(t, ok)
	dogID, ok := u.Discriminator.Mapping["dog"]
	require.True(t, ok)
	assert.Equal(t, wantCat, catID, "the cat mapping value names cat's own definition")
	assert.Equal(t, wantDog, dogID, "the dog mapping value names dog's own definition")
	assert.NotEqual(t, catID, dogID, "each mapping value names its own definition, not a shared one")

	require.Len(t, u.Variants, 2)
	variantTargets := []ir.TypeID{u.Variants[0].Type.Target, u.Variants[1].Type.Target}
	assert.Contains(t, variantTargets, catID, "the mapping names the same node its oneOf branch interned")
	assert.Contains(t, variantTargets, dogID)
}

// f10ChainParentSpec is GitHub #557's f10: C's $ref is an ordinary
// component-qualified pointer (not a bare "#/$defs/..." one, so load never
// holds it) landing on A.$defs.m, whose OWN $ref is a bare "#/$defs/n" that
// must be read relative to m's OWN position — A, the schema m is written
// under — never relative to C, the schema that reaches m from elsewhere.
const f10ChainParentSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    C: {$ref: "#/components/schemas/A/$defs/m"}
    A:
      $defs:
        m: {$ref: "#/$defs/n"}
        n: {type: object, properties: {x: {type: string}}}
`

// f10ChainParentReversedSpec is f10ChainParentSpec with A and C's declaration
// order swapped; both orders must agree, since neither m's nor n's position in
// the document depends on where C happens to be declared.
const f10ChainParentReversedSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      $defs:
        m: {$ref: "#/$defs/n"}
        n: {type: object, properties: {x: {type: string}}}
    C: {$ref: "#/components/schemas/A/$defs/m"}
`

func TestCompile_DefsPointerReachedThroughAnotherResolvesAgainstThatSchema(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, spec string }{
		{"C declared first", f10ChainParentSpec},
		{"A declared first", f10ChainParentReversedSpec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.spec)
			openapitest.RequireNoErrorDiags(t, diags)

			wantM := ids.ForPointer(ids.Ptr("components", "schemas", "A", "$defs", "m"))
			wantN := ids.ForPointer(ids.Ptr("components", "schemas", "A", "$defs", "n"))

			// C is a bare $ref with nothing beside it, so lowering it hoists an
			// alias Scalar over whatever it names (annotation.HomeOwnNode, since a
			// top-level component is not a CarriedRef position).
			c, ok := doc.Types[componentID("C")].(*ir.Scalar)
			require.True(t, ok, "C's bare $ref hoists an alias over the target it names")
			require.NotNil(t, c.Base)
			assert.Equal(t, wantM, c.Base.Target, "C names A.$defs.m")

			// m is likewise a bare $ref reached only because C's reference landed on
			// it, so it too hoists an alias over its own target.
			m, ok := doc.Types[wantM].(*ir.Scalar)
			require.True(t, ok, "m's own bare $ref hoists an alias too")
			require.NotNil(t, m.Base)
			assert.Equal(t, wantN, m.Base.Target,
				"m's own '#/$defs/n' is read from m's position, landing on A's own n — not on C's")

			n, ok := doc.Types[wantN].(*ir.Model)
			require.True(t, ok, "the terminal definition is a concrete Model")
			_, ok = propByWire(n, "x")
			assert.True(t, ok)
		})
	}
}

// f5bSpec is GitHub #557's f5b: A has no $defs of its own — not even from an
// ancestor — while its sibling B does. A's "#/$defs/n" must stay unresolved
// rather than borrowing B's definition; before the fix, the resolver's cache
// (keyed on the fragment alone, document-wide) could hand A's reference
// whatever B's already resolved.
const f5bSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      properties:
        p: {$ref: "#/$defs/n"}
    B:
      type: object
      properties:
        q: {$ref: "#/$defs/n"}
      $defs:
        n: {type: object, properties: {y: {type: integer}}}
`

// f5bReversedSpec is f5bSpec with A and B's declaration order swapped; A's
// reference must stay unresolved in both orders.
const f5bReversedSpec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    B:
      type: object
      properties:
        q: {$ref: "#/$defs/n"}
      $defs:
        n: {type: object, properties: {y: {type: integer}}}
    A:
      type: object
      properties:
        p: {$ref: "#/$defs/n"}
`

func TestCompile_DefsPointerWithNoSiblingDefsIsUnresolvedNotBorrowed(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, spec string }{
		{"A declared first", f5bSpec},
		{"B declared first", f5bReversedSpec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, tc.spec)

			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, "/components/schemas/A/properties/p"),
				"A has no $defs of its own, and the rule never falls back to a sibling's; got %+v", diags)

			a, ok := doc.Types[componentID("A")].(*ir.Model)
			require.True(t, ok)
			p, ok := propByWire(a, "p")
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("t/prim/any"), p.Type.Target,
				"an unresolved $ref degrades to any rather than borrowing B's sibling definition")

			b, ok := doc.Types[componentID("B")].(*ir.Model)
			require.True(t, ok)
			q, ok := propByWire(b, "q")
			require.True(t, ok)
			wantN := ids.ForPointer(ids.Ptr("components", "schemas", "B", "$defs", "n"))
			assert.Equal(t, wantN, q.Type.Target, "B's own reference still resolves to its own definition")
		})
	}
}
