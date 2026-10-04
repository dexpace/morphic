package openapi

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// propByWire looks up m's property by wire name. This file is package openapi,
// for the helpers it shares with the other internal tests, so it cannot reach
// conformance_test.go's helper of the same name in package openapi_test; it
// puts openapitest.PropsByWire's map in the same two-value form.
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
// "#/$defs/n" resolve to their OWN definition, never the other's, whichever is
// declared first, and the two orders intern identical types. The resolver
// alone would hand the second reference the first's definition, so which
// shape "won" would depend on declaration order.
func TestCompile_DefsPointerRelativeToItsOwnSchema(t *testing.T) {
	t.Parallel()
	forward := readReproducer(t, "two")

	docForward, diagsForward := parseFull(t, string(forward))
	openapitest.RequireNoErrorDiags(t, diagsForward)
	docReversed, diagsReversed := parseFull(t, twoReversedSpec)
	openapitest.RequireNoErrorDiags(t, diagsReversed)

	for _, doc := range []*ir.Document{docForward, docReversed} {
		assertOwnDefsProperty(t, doc, componentID("A"), ids.Ptr("components", "schemas", "A"), "p", "n", "x")
		assertOwnDefsProperty(t, doc, componentID("B"), ids.Ptr("components", "schemas", "B"), "q", "n", "y")
	}

	if d := cmp.Diff(docForward.Types, docReversed.Types); d != "" {
		t.Errorf("declaration order must not change identity (-forward +reversed):\n%s", d)
	}
}

// assertOwnDefsProperty requires that the owner's own property (by wire name)
// resolves directly to a Model interned at that SAME schema's own "$defs/<def>"
// position, carrying wantProp — proof the reference landed on the schema's own
// definition rather than a sibling's (GitHub #557). A model property is a
// CarriedRef position, so a bare $ref (no siblings, as every property here is)
// resolves directly to the target with no alias in between.
func assertOwnDefsProperty(t *testing.T, doc *ir.Document, owner ir.TypeID, ownerPointer jsontext.Pointer, prop, def, wantProp string) {
	t.Helper()
	m, ok := doc.Types[owner].(*ir.Model)
	require.True(t, ok, "%s is a Model", owner)
	p, ok := propByWire(m, prop)
	require.True(t, ok, "%s declares property %q", owner, prop)

	wantID := ids.ForPointer(ownerPointer + ids.Ptr("$defs", def))
	require.Equal(t, wantID, p.Type.Target,
		"%s.%s must resolve to its own $defs/%s, not a sibling's", owner, prop, def)

	target, ok := doc.Types[p.Type.Target].(*ir.Model)
	require.True(t, ok, "the definition itself is a Model")
	_, ok = propByWire(target, wantProp)
	assert.True(t, ok, "%s's own definition declares %q; got %+v", owner, wantProp, target.Properties)
}

// TestCompile_DefsPointerInOperationsRelativeToItsOwnSchema is the regression for
// GitHub #557 outside components: two operations that each $ref their own
// "#/$defs/n", a path and a definition key the pointer must escape and the
// fragment encode, and a component whose own $defs are read when a callback
// reaches it through another $ref. Each lowers at the pointer that names it.
func TestCompile_DefsPointerInOperationsRelativeToItsOwnSchema(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, string(readReproducer(t, "defs_in_operations")))
	openapitest.RequireNoErrorDiags(t, diags)

	response := func(path, media string) jsontext.Pointer {
		return ids.Ptr("paths", path, "get", "responses", "200", "content", media, "schema")
	}
	tests := []struct {
		name            string
		owner           ir.TypeID
		at              jsontext.Pointer
		prop, def, want string
	}{
		{"an operation", "", response("/a", "application/json"), "p", "n", "x"},
		{"another with the same definition name", "", response("/b/{id}", "application/json"), "q", "n", "y"},
		{"keys that need escaping", "", response("/a~b/{x y}/ü%41+1", "application/vnd.api+json"), "r", "a/b", "z"},
		{"reached through a callback", componentID("Wrapper"), ids.Ptr("components", "schemas", "Wrapper"), "inner", "w", "v"},
	}
	for _, tc := range tests {
		owner := tc.owner
		if owner == "" {
			owner = ids.ForPointer(tc.at)
		}
		t.Run(tc.name, func(t *testing.T) {
			assertOwnDefsProperty(t, doc, owner, tc.at, tc.prop, tc.def, tc.want)
		})
	}
}

// defsNullabilityAndDescriptionSpec has two sibling components that each $ref
// their own "#/$defs/n", one a plain string and the other an "integer|null"
// union, each carrying its own description. Both definitions reduce to the
// shared primitive scalar, which has nowhere of its own to hold a description
// or a null bit, so both merge onto the referencing property from the schema's
// OWN definition (fillPropertyAnnotations, refNullable). A collapsed identity
// would read one sibling's nullability or description onto the other's
// property with no diagnostic.
const defsNullabilityAndDescriptionSpec = `openapi: 3.1.0
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
	doc, diags := parseFull(t, defsNullabilityAndDescriptionSpec)
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

// defsMappingSpec is a discriminated oneOf whose mapping values are
// "#/$defs/..." pointers naming Pet's own sibling definitions, the same
// definitions its oneOf branches already $ref. Read through the resolver's
// cache, a mapping value could name whichever definition it happened to hold,
// typing "dog" with cat's shape or the reverse.
const defsMappingSpec = `openapi: 3.1.0
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
	doc, diags := parseFull(t, defsMappingSpec)
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

// defsChainSpec has a C whose $ref is an ordinary component-qualified pointer
// (not a bare "#/$defs/..." one, so load never holds it) landing on A.$defs.m,
// whose OWN $ref is a bare "#/$defs/n" that must be read relative to m's OWN
// position, A, the schema m is written under, never relative to C, the schema
// that reaches m from elsewhere.
const defsChainSpec = `openapi: 3.1.0
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

// defsChainReversedSpec is defsChainSpec with A and C's declaration order
// swapped; both orders must agree, since neither m's nor n's position in the
// document depends on where C happens to be declared.
const defsChainReversedSpec = `openapi: 3.1.0
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
		{"C declared first", defsChainSpec},
		{"A declared first", defsChainReversedSpec},
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

// defsNoSiblingSpec has an A with no $defs of its own, not even from an
// ancestor, while its sibling B does. A's "#/$defs/n" stays unresolved rather
// than borrowing B's definition, as the resolver's cache, keyed on the fragment
// alone for the whole document, would let it.
const defsNoSiblingSpec = `openapi: 3.1.0
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

// defsNoSiblingReversedSpec is defsNoSiblingSpec with A and B's declaration
// order swapped; A's reference must stay unresolved in both orders.
const defsNoSiblingReversedSpec = `openapi: 3.1.0
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
		{"A declared first", defsNoSiblingSpec},
		{"B declared first", defsNoSiblingReversedSpec},
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

// TestCompile_DefsPointerNoDefinitionAnswersForIsReportedOnceWhereverItSits pins
// that a "#/$defs/..." reference no definition answers for is one error at the
// reference, with the resolver's reason, at a position the lowering models (a
// property) and at one it keeps verbatim ("not"), where only load reports it.
func TestCompile_DefsPointerNoDefinitionAnswersForIsReportedOnceWhereverItSits(t *testing.T) {
	t.Parallel()
	const spec = `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    A:
      type: object
      not: {$ref: "#/$defs/missing"}
      properties:
        p: {$ref: "#/$defs/missing"}
`
	_, diags := parseFull(t, spec)

	const want = `unresolved $ref "#/$defs/missing": definition not found: #/$defs/missing`
	for _, site := range []string{"/components/schemas/A/not", "/components/schemas/A/properties/p"} {
		assert.Equal(t, want, openapitest.DiagMessageAt(t, diags, diag.UnresolvedRef, ir.SeverityError, site), site)
	}
}
