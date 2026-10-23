package schema_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

func TestAllOf_DiscriminatorSubtypeValue(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Pet:
      type: object
      discriminator:
        propertyName: petType
        mapping:
          kitty: "#/components/schemas/Cat"
      properties:
        petType: {type: string}
    Cat:
      allOf:
        - {$ref: "#/components/schemas/Pet"}
        - type: object
          properties:
            meow: {type: string}
    Dog:
      allOf:
        - {$ref: "#/components/schemas/Pet"}
        - type: object
          properties:
            bark: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok, "Pet should be a model")
	require.NotNil(t, pet.Discriminator, "the base carries the discriminator")
	assert.Empty(t, pet.DiscriminatorValue, "the base itself has no wire tag value")

	cat, ok := doc.Types[componentID("Cat")].(*ir.Model)
	require.True(t, ok, "Cat should be a model")
	assert.Equal(t, componentID("Pet"), cat.Base.Target)
	assert.Equal(t, "kitty", cat.DiscriminatorValue,
		"mapping key pointing at Cat becomes its wire tag value")

	dog, ok := doc.Types[componentID("Dog")].(*ir.Model)
	require.True(t, ok, "Dog should be a model")
	assert.Equal(t, componentID("Pet"), dog.Base.Target)
	assert.Equal(t, "Dog", dog.DiscriminatorValue,
		"a subtype absent from the mapping falls back to its schema name")
}

// TestAllOf_InlineSubtypeHasNoImplicitDiscriminatorValue pins GitHub #517.
// OpenAPI's implicit discriminator mapping names a subtype by its component
// schema name; an inline subtype — declared here as a property, once keyed like
// the unrelated component Dog and once keyed like nothing in the document —
// has no schema name of its own and so gets no implicit tag. Before the fix the
// fallback read the subtype's pointer's last token instead, so Kennel's "Dog"
// property claimed the same tag as the component Dog, and its "pet" property
// claimed its own property key as a tag the document never assigned.
func TestAllOf_InlineSubtypeHasNoImplicitDiscriminatorValue(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Pet:
      type: object
      required: [kind]
      properties:
        kind: {type: string}
      discriminator:
        propertyName: kind
    Dog:
      allOf:
        - $ref: '#/components/schemas/Pet'
      type: object
      properties:
        bark: {type: string}
    Kennel:
      type: object
      properties:
        Dog:
          allOf:
            - $ref: '#/components/schemas/Pet'
          type: object
          properties:
            woof: {type: string}
        pet:
          allOf:
            - $ref: '#/components/schemas/Pet'
          type: object
          properties:
            meow: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	kennelDog := ir.TypeID("t/anon/components/schemas/Kennel/properties/Dog")
	kennelPet := ir.TypeID("t/anon/components/schemas/Kennel/properties/pet")
	want := map[ir.TypeID]string{
		componentID("Dog"): "Dog",
		kennelDog:          "",
		kennelPet:          "",
	}
	got := discriminatorValues(t, doc, componentID("Dog"), kennelDog, kennelPet)
	assert.Empty(t, cmp.Diff(want, got),
		"the component keeps its implicit tag; neither inline subtype gets one")
}

// TestAllOf_InlineSubtypeDiscriminatorValueFromMapping pins the path the #517
// fix leaves untouched: a mapping entry can still name an inline subtype by
// JSON reference, and mappingTagsFor — not subtypeDiscriminatorValue's
// implicit-name fallback — is what gives it that tag.
func TestAllOf_InlineSubtypeDiscriminatorValueFromMapping(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Kennel:
      type: object
      properties:
        Dog:
          allOf:
            - $ref: '#/components/schemas/Pet'
          type: object
          properties:
            woof: {type: string}
        pet:
          allOf:
            - $ref: '#/components/schemas/Pet'
          type: object
          properties:
            meow: {type: string}
    Pet:
      type: object
      required: [kind]
      properties:
        kind: {type: string}
      discriminator:
        propertyName: kind
        mapping:
          woofer: '#/components/schemas/Kennel/properties/Dog'
    Dog:
      allOf:
        - $ref: '#/components/schemas/Pet'
      type: object
      properties:
        bark: {type: string}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	kennelDog := ir.TypeID("t/anon/components/schemas/Kennel/properties/Dog")
	kennelPet := ir.TypeID("t/anon/components/schemas/Kennel/properties/pet")
	want := map[ir.TypeID]string{
		componentID("Dog"): "Dog",
		kennelDog:          "woofer",
		kennelPet:          "",
	}
	got := discriminatorValues(t, doc, componentID("Dog"), kennelDog, kennelPet)
	assert.Empty(t, cmp.Diff(want, got),
		"a mapping entry names Kennel's inline Dog by JSON reference; pet still gets no implicit tag")

	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok, "Pet should be a model")
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, map[string]ir.TypeID{"woofer": kennelDog}, pet.Discriminator.Mapping,
		"the base's own mapping resolves the same inline subtype the tag was read from")
}

// TestAllOf_DiscriminatorValueFromDistantMapping pins the mapping-key spelling of
// a tag at depth: the root names a grandchild explicitly, and the grandchild's own
// branch is an intermediate that declares no discriminator, so the key is only
// reachable by walking past the immediate parent (GitHub #305).
//
// The conformance case discriminator-transitive.yaml witnesses the same walk.
func TestAllOf_DiscriminatorValueFromDistantMapping(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Pet:
      type: object
      required: [petType]
      properties: {petType: {type: string}}
      discriminator:
        propertyName: petType
        mapping:
          dog: '#/components/schemas/Dog'
          puppy: '#/components/schemas/Puppy'
    Dog:
      allOf:
        - {$ref: '#/components/schemas/Pet'}
      properties: {bark: {type: boolean}}
    Puppy:
      allOf:
        - {$ref: '#/components/schemas/Dog'}
      properties: {weeks: {type: integer}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	dog := typeByName(doc, "Dog").(*ir.Model)
	assert.Equal(t, "dog", dog.DiscriminatorValue, "the direct child takes its own mapping key")

	puppy := typeByName(doc, "Puppy").(*ir.Model)
	require.NotNil(t, puppy.Base)
	assert.Equal(t, componentID("Dog"), puppy.Base.Target,
		"Puppy composes Dog, which declares no discriminator of its own")
	assert.Equal(t, "puppy", puppy.DiscriminatorValue,
		"the key Pet's mapping spells for Puppy survives the intermediate")
}

// TestAllOf_DiscriminatorValueFromNearestAncestor pins which of two discriminated
// ancestors supplies the tag when both mappings name the same subtype. The walk
// is level by level so the answer is the nearer one, rather than whichever chain
// happened to be walked first — a subtype's own hierarchy outranks one it reaches
// only through a longer composition.
func TestAllOf_DiscriminatorValueFromNearestAncestor(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Far:
      type: object
      properties: {k: {type: string}}
      discriminator:
        propertyName: k
        mapping: {far: '#/components/schemas/Leaf'}
    Mid:
      allOf:
        - {$ref: '#/components/schemas/Far'}
      properties: {m: {type: string}}
    Near:
      type: object
      properties: {n: {type: string}}
      discriminator:
        propertyName: n
        mapping: {near: '#/components/schemas/Leaf'}
    Leaf:
      allOf:
        - {$ref: '#/components/schemas/Mid'}
        - {$ref: '#/components/schemas/Near'}
      properties: {l: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	leaf := typeByName(doc, "Leaf").(*ir.Model)
	assert.Equal(t, "near", leaf.DiscriminatorValue,
		"Near is one hop up and Far two, so Near's mapping key wins")
}

// TestAllOf_DiscriminatorValueUndiscriminatedChain is the boundary of the
// ancestor walk: composing at any depth is not on its own a reason to carry a
// tag. Without this, a walk that fell back to the implicit schema name whenever
// it found a base would stamp every composed model in the document.
func TestAllOf_DiscriminatorValueUndiscriminatedChain(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Plant:
      type: object
      properties: {stem: {type: string}}
    Shrub:
      allOf:
        - {$ref: '#/components/schemas/Plant'}
      properties: {twigs: {type: integer}}
    Sapling:
      allOf:
        - {$ref: '#/components/schemas/Shrub'}
      properties: {rings: {type: integer}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	for _, name := range []string{"Shrub", "Sapling"} {
		m := typeByName(doc, name).(*ir.Model)
		require.NotNil(t, m.Base, "%s composes a base", name)
		assert.Empty(t, m.DiscriminatorValue,
			"%s has no discriminated ancestor at any depth", name)
	}
}

// TestAllOf_DiscriminatorValueCyclicComposition drives the ancestor walk over a
// composition cycle. Nothing upstream refuses one — this spec compiles without an
// error diagnostic — so the walk meets it, and the visited set rather than the
// depth cap is what has to stop it.
//
// The fan-out is what makes that a real claim: every schema composes every other,
// so a walk that re-entered a schema it had already left would branch five ways
// per level for the length of the cap instead of terminating.
func TestAllOf_DiscriminatorValueCyclicComposition(t *testing.T) {
	t.Parallel()
	const n = 6
	var b strings.Builder
	for i := range n {
		refs := make([]string, 0, n-1)
		for j := range n {
			if j != i {
				refs = append(refs, fmt.Sprintf("{$ref: '#/components/schemas/X%d'}", j))
			}
		}
		fmt.Fprintf(&b, "    X%d:\n      allOf: [%s]\n      properties: {p%d: {type: string}}\n",
			i, strings.Join(refs, ", "), i)
	}
	doc, diags := lowerSpec(t, openapitest.ComponentSpec(b.String()))
	openapitest.RequireNoErrorDiags(t, diags)

	for i := range n {
		name := fmt.Sprintf("X%d", i)
		m, ok := typeByName(doc, name).(*ir.Model)
		require.True(t, ok, "%s lowers to a Model", name)
		assert.Empty(t, m.DiscriminatorValue, "%s has no discriminated ancestor", name)
	}
}

// TestAllOf_DiscriminatorValueSelfAncestorInCycle pins that a composition cycle
// does not make a schema one of its own ancestors. A declares the discriminator
// and composes B, which composes A back, so walking A's ancestors comes round to
// A — whose mapping names A. A hierarchy of one schema standing above itself is
// not a hierarchy, so the walk skips the schema it started from.
func TestAllOf_DiscriminatorValueSelfAncestorInCycle(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    A:
      allOf: [{$ref: '#/components/schemas/B'}]
      required: [k]
      properties: {k: {type: string}}
      discriminator:
        propertyName: k
        mapping:
          selfkey: '#/components/schemas/A'
    B:
      allOf: [{$ref: '#/components/schemas/A'}]
      properties: {b: {type: string}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)

	a, ok := typeByName(doc, "A").(*ir.Model)
	require.True(t, ok, "A lowers to a Model")
	require.NotNil(t, a.Discriminator, "A anchors the hierarchy it declares")
	assert.Empty(t, a.DiscriminatorValue,
		"and takes no tag from its own mapping by way of the cycle")
}

// TestAllOf_DiscriminatorValueChainDeeperThanCap pins the ancestor walk's cap as
// behaviour rather than as a comment. A chain longer than the cap stops being
// searched, so the tag the root would have supplied is not stamped — stated here
// so the limit is a decision on record and a chain that grows past it is a
// failing test rather than a silent reversion to the bug this walk fixed.
//
// The shallower half is the control: the same construction one level inside the
// cap does carry the tag, which is what says the deep case failed on the cap and
// not on the construction.
func TestAllOf_DiscriminatorValueChainDeeperThanCap(t *testing.T) {
	t.Parallel()
	// maxDiscriminatorAncestorDepth is unexported; 256 is its committed value, and
	// the two cases below straddle it.
	const ancestorCap = 256
	for _, tc := range []struct {
		name  string
		links int
		want  string
	}{
		{"within the cap", ancestorCap - 1, fmt.Sprintf("S%d", ancestorCap-2)},
		{"beyond the cap", ancestorCap + 4, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			b.WriteString("    Root:\n      type: object\n      properties: {k: {type: string}}\n" +
				"      discriminator: {propertyName: k}\n")
			prev := "Root"
			for i := range tc.links {
				fmt.Fprintf(&b, "    S%d:\n      allOf: [{$ref: '#/components/schemas/%s'}]\n"+
					"      properties: {p%d: {type: string}}\n", i, prev, i)
				prev = fmt.Sprintf("S%d", i)
			}
			doc, diags := lowerSpec(t, openapitest.ComponentSpec(b.String()))
			openapitest.RequireNoErrorDiags(t, diags)

			leaf := typeByName(doc, prev).(*ir.Model)
			assert.Equal(t, tc.want, leaf.DiscriminatorValue,
				"%s is %d links below the discriminated root", prev, tc.links)
		})
	}
}

func TestModelDiscriminator_UndeclaredPropertyAndBadMapping(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Vehicle:
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
	assert.True(t, openapitest.HasDiag(diags, diag.UnresolvedRef), "bad mapping target diagnostic")
}

func TestOneOf_DiscriminatorWithDefault(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpecVer("3.2.0", `    Shape:
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

func TestAllOf_ModelWithOwnDiscriminator(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    Base:
      allOf:
        - {$ref: '#/components/schemas/Common'}
      discriminator: {propertyName: kind}
      properties: {kind: {type: string}}
    Common: {type: object, properties: {id: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	base := typeByName(doc, "Base").(*ir.Model)
	require.NotNil(t, base.Discriminator, "allOf model may declare its own discriminator")
	assert.NotEmpty(t, base.Discriminator.Property)
}

func TestAllOf_BoolRefBranchHasNoDiscriminator(t *testing.T) {
	t.Parallel()
	spec := openapitest.ComponentSpec(`    BoolComp: false
    Sub:
      allOf:
        - {$ref: '#/components/schemas/BoolComp'}
        - {type: object, properties: {a: {type: string}}}
`)
	doc, diags := lowerSpec(t, spec)
	require.NotNil(t, doc)
	sub := typeByName(doc, "Sub").(*ir.Model)
	assert.Empty(t, sub.DiscriminatorValue, "a bool-schema ref target anchors no hierarchy")
	_ = diags
}

// TestAllOf_DiscriminatorAliasTagIsOrderInvariant pins that a subtype two mapping
// keys name takes the same tag whichever key is written first (GitHub #410).
// Model.DiscriminatorValue holds one value, and a mapping is unordered, so the
// key it holds must be chosen by a rule the source order cannot reach: the
// smallest in byte order. The base keeps every key, so nothing is lost, and the
// election is reported so a reader knows the subtype's tag set is wider than
// the field shows.
func TestAllOf_DiscriminatorAliasTagIsOrderInvariant(t *testing.T) {
	t.Parallel()
	const base = `    Pet:
      type: object
      required: [k]
      properties: {k: {type: string}}
      discriminator:
        propertyName: k
        mapping:
`
	const sub = `    Dog:
      allOf: [{$ref: '#/components/schemas/Pet'}]
      properties: {bark: {type: boolean}}
`
	const alpha = "          alpha: '#/components/schemas/Dog'\n"
	const zulu = "          zulu: '#/components/schemas/Dog'\n"

	for _, tc := range []struct{ name, mapping string }{
		{"zulu first", zulu + alpha},
		{"alpha first", alpha + zulu},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags := lowerSpec(t, openapitest.ComponentSpec(base+tc.mapping+sub))
			openapitest.RequireNoErrorDiags(t, diags)

			dog, ok := doc.Types[componentID("Dog")].(*ir.Model)
			require.True(t, ok, "Dog should be a model")
			assert.Equal(t, "alpha", dog.DiscriminatorValue,
				"the smallest key in byte order, whichever was written first")

			pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
			require.True(t, ok, "Pet should be a model")
			assert.Equal(t, map[string]ir.TypeID{"alpha": componentID("Dog"), "zulu": componentID("Dog")},
				pet.Discriminator.Mapping, "the base keeps both keys")

			assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, "/components/schemas/Dog"),
				"the election is reported at the subtype: %+v", diags)
			assert.True(t, openapitest.HasDiagAt(diags, diag.DegradedConstruct, ir.SeverityInfo),
				"as information, since the base's mapping loses nothing")
		})
	}
}

// TestDiscriminatorMapping_InlineTargetResolvesInEitherOrder pins that a
// discriminator mapping, or a 3.2 defaultMapping, naming an inline position
// resolves whichever is declared first: the base carrying the discriminator, or
// the schema enclosing the target. With the base first the target was not yet
// built when the mapping was read, so the entry was dropped as unresolved
// (GitHub #530). The two-order harness cannot see this: it stops at the first
// error diagnostic, which that order produced.
//
// Each row's spec is written base-first, the order that failed; the other order
// moves the base's block after the owner's.
func TestDiscriminatorMapping_InlineTargetResolvesInEitherOrder(t *testing.T) {
	t.Parallel()

	const subBody = `{allOf: [{$ref: '#/components/schemas/Pet'}], type: object, properties: {woof: {type: string}}}`
	// pet is the discriminated base, entry its mapping or defaultMapping line.
	pet := func(entry string) string {
		return `    Pet:
      type: object
      required: [kind]
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        ` + entry + "\n"
	}
	mapping := func(target string) string { return pet(`mapping: {woofer: '#` + target + `'}`) }
	spec32 := func(schemas string) string { return openapitest.ComponentSpecVer("3.2.0", schemas) }

	const (
		propTarget    = "/components/schemas/Kennel/properties/Dog"
		itemsTarget   = "/components/schemas/Kennel/items"
		keywordTarget = "/components/schemas/Kennel/properties/items/items"
		defsTarget    = "/components/schemas/Kennel/$defs/Dog"
		pathsBase     = "/paths/~1a/get/responses/200/content/application~1json/schema"
		pathsTarget   = "/paths/~1b/get/responses/200/content/application~1json/schema/items"
	)
	ownerProperty := `    Kennel:
      type: object
      properties:
        Dog: ` + subBody + "\n"
	ownerItems := `    Kennel:
      type: array
      items: ` + subBody + "\n"
	ownerKeywordKey := `    Kennel:
      type: object
      properties:
        items:
          type: array
          items: ` + subBody + "\n"
	ownerDefs := `    Kennel:
      type: object
      $defs:
        Dog: ` + subBody + "\n"
	pathBase := `  /a:
    get:
      operationId: getA
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                required: [kind]
                properties: {kind: {type: string}}
                discriminator:
                  propertyName: kind
                  mapping: {woofer: '#` + pathsTarget + `'}
`
	pathOwner := `  /b:
    get:
      operationId: getB
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  allOf: [{$ref: '#` + pathsBase + `'}]
                  type: object
                  properties: {woof: {type: string}}
`

	cases := []struct {
		name             string
		spec             func(string) string
		base, owner      string
		baseID, targetID ir.TypeID
		wantDefault      bool
	}{
		{"a property", openapitest.ComponentSpec, mapping(propTarget), ownerProperty,
			componentID("Pet"), "t/anon" + propTarget, false},
		{"items", openapitest.ComponentSpec, mapping(itemsTarget), ownerItems,
			componentID("Pet"), "t/anon" + itemsTarget, false},
		{"a keyword-named key", openapitest.ComponentSpec, mapping(keywordTarget), ownerKeywordKey,
			componentID("Pet"), "t/anon" + keywordTarget, false},
		{"a $defs entry", openapitest.ComponentSpec, mapping(defsTarget), ownerDefs,
			componentID("Pet"), "t/anon" + defsTarget, false},
		{"a 3.2 defaultMapping", spec32, pet(`defaultMapping: '#` + propTarget + `'`), ownerProperty,
			componentID("Pet"), "t/anon" + propTarget, true},
		{"a /paths inline subtype", openapitest.PathsSpec, pathBase, pathOwner,
			"t/anon" + pathsBase, "t/anon" + pathsTarget, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first, diags := parseFull(t, tc.spec(tc.base+tc.owner))
			openapitest.RequireNoErrorDiags(t, diags)
			last, diags := parseFull(t, tc.spec(tc.owner+tc.base))
			openapitest.RequireNoErrorDiags(t, diags)

			for _, doc := range []*ir.Document{first, last} {
				base, ok := doc.Types[tc.baseID].(*ir.Model)
				require.True(t, ok, "the discriminator base is a model")
				require.NotNil(t, base.Discriminator)

				subtype, ok := doc.Types[tc.targetID].(*ir.Model)
				require.True(t, ok, "the mapping target is a model")

				if tc.wantDefault {
					assert.Equal(t, tc.targetID, base.Discriminator.Default)
					// defaultMapping is the fallback for a tag the mapping does not
					// recognize, so it names no tag for its target, and an inline
					// subtype has no name to imply one (GitHub #517).
					assert.Empty(t, subtype.DiscriminatorValue)
				} else {
					assert.Equal(t, tc.targetID, base.Discriminator.Mapping["woofer"])
					assert.Equal(t, "woofer", subtype.DiscriminatorValue)
				}
			}

			// The type registry is a map, so this is the properly order-blind
			// claim; the /paths row's operation LIST legitimately reorders with
			// its two path items, which is declaration order working as
			// designed and no part of what this test is pinning.
			assert.Empty(t, cmp.Diff(first.Types, last.Types),
				"declaring the base before or after the owner must not change the registry")
		})
	}
}

// TestDiscriminatorMapping_ToAPureRefNamesItsTarget pins GitHub #758. A
// mapping names a type, and a position that is only a $ref to another is a use
// of that one's type, so a mapping to it names what the $ref names, as a union
// variant written there does. A component is a declaration of its own and
// keeps its own node. Each row is compiled base-first and reversed: both must
// map each tag to the same type, with nothing reported, pass.Validate included.
func TestDiscriminatorMapping_ToAPureRefNamesItsTarget(t *testing.T) {
	t.Parallel()
	const subtype = "{allOf: [{$ref: '#/components/schemas/Pet'}], type: object, properties: {woof: {type: string}}}"
	pet := "    Pet:\n      type: object\n      required: [kind]\n      properties: {kind: {type: string}}\n" +
		"      discriminator: {propertyName: kind, mapping: {%s}}\n"
	union := "    Pet:\n      oneOf: [%s]\n" +
		"      discriminator: {propertyName: kind, mapping: {%s}}\n"
	for _, c := range []struct {
		name  string
		base  string
		owner string
		want  map[string]ir.TypeID
	}{
		{
			name:  "a model base's mapping to a property",
			base:  fmt.Sprintf(pet, "woofer: '#/components/schemas/Kennel/properties/Dog'"),
			owner: "    Dog: " + subtype + "\n    Kennel: {type: object, properties: {Dog: {$ref: '#/components/schemas/Dog'}}}\n",
			want:  map[string]ir.TypeID{"woofer": componentID("Dog")},
		},
		{
			name: "a union's mapping to its branches",
			base: fmt.Sprintf(union, "{$ref: '#/components/schemas/Cat'}, {type: object, properties: {kind: {type: string}}}",
				"c: '#/components/schemas/Pet/oneOf/0', d: '#/components/schemas/Pet/oneOf/1'"),
			owner: "    Cat: {type: object, properties: {kind: {type: string}}}\n",
			want: map[string]ir.TypeID{"c": componentID("Cat"),
				"d": "t/anon/components/schemas/Pet/oneOf/1"},
		},
		{
			name: "a chain of such positions",
			base: fmt.Sprintf(pet, "woofer: '#/components/schemas/Kennel/properties/a'"),
			owner: "    Dog: " + subtype + "\n    Kennel:\n      type: object\n      properties:\n" +
				"        a: {$ref: '#/components/schemas/Kennel/properties/b'}\n" +
				"        b: {$ref: '#/components/schemas/Dog'}\n",
			want: map[string]ir.TypeID{"woofer": componentID("Dog")},
		},
		{
			name: "a union branch naming a component alias",
			base: fmt.Sprintf(union, "{$ref: '#/components/schemas/CatAlias'}",
				"c: '#/components/schemas/Pet/oneOf/0'"),
			owner: "    Cat: {type: object, properties: {kind: {type: string}}}\n" +
				"    CatAlias: {$ref: '#/components/schemas/Cat'}\n",
			want: map[string]ir.TypeID{"c": componentID("CatAlias")},
		},
		{
			name: "a union branch naming the component keyed by the empty string",
			base: fmt.Sprintf(union, "{$ref: '#/components/schemas/'}", "c: '#/components/schemas/'"),
			owner: "    Cat: {type: object, properties: {kind: {type: string}}}\n" +
				"    '': {$ref: '#/components/schemas/Cat'}\n",
			want: map[string]ir.TypeID{"c": "t/anon/components/schemas/"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			first, diags := parseFull(t, openapitest.ComponentSpec(c.base+c.owner))
			openapitest.RequireNoErrorDiags(t, diags)
			last, diags := parseFull(t, openapitest.ComponentSpec(c.owner+c.base))
			openapitest.RequireNoErrorDiags(t, diags)

			assert.Empty(t, cmp.Diff(first, last, orderInvariantIR()...))
			for _, doc := range []*ir.Document{first, last} {
				assert.Equal(t, c.want, discriminatorOf(t, doc, componentID("Pet")).Mapping)
				assert.Empty(t, pass.Validate(doc), "every target is a variant")
			}
		})
	}
}

// TestDiscriminatorMapping_ChainOfPositionsDeeperThanCap pins the
// read-through's cap as behaviour rather than as a comment, as
// TestAllOf_DiscriminatorValueChainDeeperThanCap does for the ancestor walk. A
// chain of $ref positions longer than the cap that is no cycle stops at the
// position the cap reached, so the mapping names that position's alias. The
// chain exactly the cap long is the control: it reads through to the component.
func TestDiscriminatorMapping_ChainOfPositionsDeeperThanCap(t *testing.T) {
	t.Parallel()
	// maxTypePositionHops is unexported; 64 is its committed value, and the two
	// cases below straddle it.
	const hopCap = 64
	for _, tc := range []struct {
		name  string
		links int
		want  ir.TypeID
	}{
		{"as long as the cap", hopCap, componentID("Dog")},
		{"beyond the cap", hopCap + 1, "t/anon/components/schemas/Kennel/properties/a64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var kennel strings.Builder
			kennel.WriteString("    Kennel:\n      type: object\n      properties:\n")
			for i := range tc.links - 1 {
				fmt.Fprintf(&kennel, "        a%d: {$ref: '#/components/schemas/Kennel/properties/a%d'}\n", i, i+1)
			}
			fmt.Fprintf(&kennel, "        a%d: {$ref: '#/components/schemas/Dog'}\n", tc.links-1)
			spec := "    Pet:\n      type: object\n      required: [kind]\n      properties: {kind: {type: string}}\n" +
				"      discriminator: {propertyName: kind, mapping: {woofer: '#/components/schemas/Kennel/properties/a0'}}\n" +
				"    Dog: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object, properties: {woof: {type: string}}}\n" +
				kennel.String()

			doc, diags := parseFull(t, openapitest.ComponentSpec(spec))

			openapitest.RequireNoErrorDiags(t, diags)
			assert.Equal(t, map[string]ir.TypeID{"woofer": tc.want}, discriminatorOf(t, doc, componentID("Pet")).Mapping,
				"a0 is %d $ref positions above the component", tc.links)
		})
	}
}

// TestDiscriminatorMapping_StopsAtAPureRefItCannotFollow pins where the
// read-through stops short of a target: a $ref into another document, one to
// nothing, and one on a loop name no position a type is read at, so the
// mapping keeps naming the position that $ref sits at, in either order of the
// entries, and the $ref is reported where it is written.
func TestDiscriminatorMapping_StopsAtAPureRefItCannotFollow(t *testing.T) {
	t.Parallel()
	const branches = "      oneOf: [{$ref: 'other.yaml#/X'}, {$ref: '#/x-lib/Missing'}, {$ref: '#/components/schemas/Pet/oneOf/2'}]\n"
	for _, mapping := range []string{"{e: '#/components/schemas/Pet/oneOf/0', m: '#/components/schemas/Pet/oneOf/1', " +
		"l: '#/components/schemas/Pet/oneOf/2'}", "{l: '#/components/schemas/Pet/oneOf/2', " +
		"m: '#/components/schemas/Pet/oneOf/1', e: '#/components/schemas/Pet/oneOf/0'}"} {
		doc, diags := parseFull(t, openapitest.ComponentSpec("    Pet:\n"+branches+
			"      discriminator: {propertyName: kind, mapping: "+mapping+"}\n")+"x-lib: {}\n")

		branch := func(i string) ir.TypeID { return ir.TypeID("t/anon/components/schemas/Pet/oneOf/" + i) }
		assert.Equal(t, map[string]ir.TypeID{"e": branch("0"), "m": branch("1"), "l": branch("2")},
			discriminatorOf(t, doc, componentID("Pet")).Mapping, mapping)
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, "/components/schemas/Pet/oneOf/0"), mapping)
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnresolvedRef, "/components/schemas/Pet/oneOf/1"), mapping)
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.CyclicRef, "/components/schemas/Pet/oneOf/2"), mapping)
	}
}

// TestDiscriminatorMapping_ThroughARefNamingTheSourceByItsFileName pins a
// pure $ref position whose $ref names the source by its file name, with
// external references off: a same-document reference, read as the source, so
// the mapping reaches the schema it names in either order of its entries. Read
// as another document, it would be refused, and the mapping would name whatever
// interned the next position first.
func TestDiscriminatorMapping_ThroughARefNamingTheSourceByItsFileName(t *testing.T) {
	t.Parallel()
	pet := func(mapping string) string {
		return "    Pet:\n      type: object\n      required: [kind]\n      properties: {kind: {type: string}}\n" +
			"      discriminator: {propertyName: kind, mapping: {" + mapping + "}}\n"
	}
	const lib = "x-lib:\n  Cat: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object}\n" +
		"  CatAlias: {$ref: 'spec.yaml#/x-lib/Cat'}\n"
	for _, mapping := range []string{"cat: '#/x-lib/Cat', alias: '#/x-lib/CatAlias'",
		"alias: '#/x-lib/CatAlias', cat: '#/x-lib/Cat'"} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(pet(mapping))+lib)
		openapitest.RequireNoErrorDiags(t, diags)
		got := discriminatorOf(t, doc, componentID("Pet")).Mapping
		assert.Equal(t, map[string]ir.TypeID{"cat": "t/anon/x-lib/Cat", "alias": "t/anon/x-lib/Cat"}, got, mapping)
	}
}

// TestDiscriminatorMapping_ToASubtypeThroughAPureRefTagsIt pins the subtype's
// side of GitHub #758: the mapping key that names a subtype through a pure
// $ref position is its discriminatorValue, in either order, not its name.
func TestDiscriminatorMapping_ToASubtypeThroughAPureRefTagsIt(t *testing.T) {
	t.Parallel()
	const base = `    Pet:
      type: object
      required: [kind]
      properties: {kind: {type: string}}
      discriminator: {propertyName: kind, mapping: {woofer: '#/components/schemas/Kennel/properties/Dog'}}
`
	const owner = `    Dog: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object}
    Kennel: {type: object, properties: {Dog: {$ref: '#/components/schemas/Dog'}}}
`
	for _, spec := range []string{base + owner, owner + base} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(spec))
		openapitest.RequireNoErrorDiags(t, diags)
		dog, ok := doc.Types[componentID("Dog")].(*ir.Model)
		require.True(t, ok)
		assert.Equal(t, "woofer", dog.DiscriminatorValue)
	}
}

// TestDiscriminatorMapping_NamesWhatAVariantWrittenThereNames pins that the
// read-through stops exactly where the lowering gives a $ref position a node of
// its own. A branch carries the $ref beside one keyword at a time, and the
// mapping to it must name what the union's variant there names: the target,
// or the alias holding what the keyword says. A keyword the lowering keeps on
// an alias, but the read-through did not stop for, makes them differ.
func TestDiscriminatorMapping_NamesWhatAVariantWrittenThereNames(t *testing.T) {
	t.Parallel()
	for _, sibling := range []string{
		"", "description: d", "title: t", "deprecated: true", "example: {}", "examples: [{}]",
		"x-ext: 1", "minProperties: 1", "readOnly: true", "not: {required: [x]}",
		"oneOf: [{type: object}]", "allOf: [{type: object}]", "if: {required: [x]}",
		"$comment: c", "externalDocs: {url: 'https://example.com'}", "nullable: true",
		"properties: {extra: {type: string}}", "required: [kind]", "type: object",
	} {
		branch := "{$ref: '#/components/schemas/Cat'}"
		if sibling != "" {
			branch = "{$ref: '#/components/schemas/Cat', " + sibling + "}"
		}
		doc, _ := parseFull(t, openapitest.ComponentSpec(
			"    Pet:\n      oneOf: ["+branch+"]\n      discriminator: {propertyName: kind, mapping: {c: '#/components/schemas/Pet/oneOf/0'}}\n"+
				"    Cat: {type: object, properties: {kind: {type: string}}}\n"))
		pet, ok := doc.Types[componentID("Pet")].(*ir.Union)
		require.True(t, ok, sibling)
		require.Len(t, pet.Variants, 1, sibling)
		assert.Equal(t, pet.Variants[0].Type.Target, pet.Discriminator.Mapping["c"], "beside %q", sibling)
	}
}

// TestDiscriminatorMapping_NamesTheVariantNamingThePosition pins a union whose
// variant is a $ref to the position its mapping names, or to a position on the
// way there. That $ref hoists an alias at the position, as a $ref to any
// position does, so the mapping names that alias and not the target past it,
// which is no variant (GitHub #758). Where no variant names the position, the
// mapping still reads through it. Each row is compiled with the union first and
// again last: both must map the tag to the variant's type, which pass.Validate
// accepts, and build the same registry.
func TestDiscriminatorMapping_NamesTheVariantNamingThePosition(t *testing.T) {
	t.Parallel()
	const (
		cat    = "    Cat: {type: object, properties: {kind: {type: string}}}\n"
		catRef = "x-lib:\n  CatRef: {$ref: '#/components/schemas/Cat'}\n"
	)
	union := func(branch, discriminator string) string {
		return "    Pet:\n      oneOf: [{$ref: '" + branch + "'}]\n" +
			"      discriminator: {propertyName: kind, " + discriminator + "}\n"
	}
	tests := []struct {
		name       string
		version    string
		components []string
		tail       string
		variant    ir.TypeID // what the variant names
		isDefault  bool      // the discriminator's defaultMapping, not its mapping c
	}{
		{
			name:       "an extension's value",
			components: []string{union("#/x-lib/CatRef", "mapping: {c: '#/x-lib/CatRef'}"), cat},
			tail:       catRef,
			variant:    "t/anon/x-lib/CatRef",
		},
		{
			name: "a property",
			components: []string{union("#/components/schemas/Lib/properties/c",
				"mapping: {c: '#/components/schemas/Lib/properties/c'}"), cat,
				"    Lib: {type: object, properties: {c: {$ref: '#/components/schemas/Cat'}}}\n"},
			variant: "t/anon/components/schemas/Lib/properties/c",
		},
		{
			name: "a $defs entry",
			components: []string{union("#/$defs/C", "mapping: {c: '#/$defs/C'}") +
				"      $defs: {C: {$ref: '#/components/schemas/Cat'}}\n", cat},
			variant: "t/anon/components/schemas/Pet/$defs/C",
		},
		{
			name: "another union's branch",
			components: []string{union("#/components/schemas/Other/oneOf/0",
				"mapping: {c: '#/components/schemas/Other/oneOf/0'}"), cat,
				"    Other: {oneOf: [{$ref: '#/components/schemas/Cat'}, {type: string}]}\n"},
			variant: "t/anon/components/schemas/Other/oneOf/0",
		},
		{
			name:       "a 3.2 defaultMapping",
			version:    "3.2.0",
			components: []string{union("#/x-lib/CatRef", "defaultMapping: '#/x-lib/CatRef'"), cat},
			tail:       catRef,
			variant:    "t/anon/x-lib/CatRef",
			isDefault:  true,
		},
		{
			name:       "the union's own branch, through to the position its $ref names",
			components: []string{union("#/x-lib/A", "mapping: {c: '#/components/schemas/Pet/oneOf/0'}"), cat},
			tail:       "x-lib:\n  A: {$ref: '#/x-lib/B'}\n  B: {$ref: '#/components/schemas/Cat'}\n",
			variant:    "t/anon/x-lib/A",
		},
		{
			name: "a position no variant names, read through to the variant",
			components: []string{"    Pet:\n      oneOf: [{$ref: '#/components/schemas/Cat'}]\n" +
				"      discriminator: {propertyName: kind, mapping: {c: '#/x-lib/CatRef'}}\n", cat},
			tail:    catRef,
			variant: componentID("Cat"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			version := tc.version
			if version == "" {
				version = "3.1.0"
			}
			first, diags := parseFull(t, rawSpec(version, tc.components, tc.tail, false))
			openapitest.RequireNoErrorDiags(t, diags)
			last, diags := parseFull(t, rawSpec(version, tc.components, tc.tail, true))
			openapitest.RequireNoErrorDiags(t, diags)

			for _, doc := range []*ir.Document{first, last} {
				pet, ok := doc.Types[componentID("Pet")].(*ir.Union)
				require.True(t, ok, "Pet is a union")
				require.Len(t, pet.Variants, 1)
				require.NotNil(t, pet.Discriminator)
				assert.Equal(t, tc.variant, pet.Variants[0].Type.Target)
				got := pet.Discriminator.Mapping["c"]
				if tc.isDefault {
					got = pet.Discriminator.Default
				}
				assert.Equal(t, pet.Variants[0].Type.Target, got, "the mapping names what the variant names")
				assert.Empty(t, pass.Validate(doc), "every target is a variant")
			}
			assert.Empty(t, cmp.Diff(first.Types, last.Types),
				"declaring the union before or after what its variant names must not change the registry")
		})
	}
}

// TestDiscriminatorMapping_HoistedTargetKeepsItsDiagnostics pins that what
// lowering a mapping target reports reaches the compile's diagnostics, for a
// mapping entry and a defaultMapping alike. The targets sit under not and if,
// which no declaration lowers, so the hoist is the only lowering that sees the
// format each declares beside a model: dropping its findings would lose them.
func TestDiscriminatorMapping_HoistedTargetKeepsItsDiagnostics(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, openapitest.ComponentSpecVer("3.2.0", `    Pet:
      type: object
      required: [kind]
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {n: '#/components/schemas/Kennel/not'}
        defaultMapping: '#/components/schemas/Kennel/if'
    Kennel:
      type: object
      not: {type: object, format: wat, properties: {a: {type: string}}}
      if: {type: object, format: huh, properties: {b: {type: string}}}
      then: {required: [b]}
`))
	openapitest.RequireNoErrorDiags(t, diags)

	pet, ok := doc.Types[componentID("Pet")].(*ir.Model)
	require.True(t, ok, "Pet is a model")
	require.NotNil(t, pet.Discriminator)
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Kennel/not"), pet.Discriminator.Mapping["n"])
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Kennel/if"), pet.Discriminator.Default)

	for _, pointer := range []string{"/components/schemas/Kennel/not", "/components/schemas/Kennel/if"} {
		assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, pointer),
			"the format beside the model hoisted at %s is reported", pointer)
	}
}

// TestDiscriminatorBesideUnion_MovesWhenARouteIsNoSubtype pins that a model
// beside a union kept verbatim does not keep a discriminator that routes to a
// type that is not its subtype, which pass.Validate rejects. Each route counts:
// a mapping target, the default, and a component a $ref branch names, which
// the discriminator routes to by name. Each "lowers to" row names a type whose
// own allOf names Pet but which lowers to no Model with that Base, which only
// the lowered document can tell.
func TestDiscriminatorBesideUnion_MovesWhenARouteIsNoSubtype(t *testing.T) {
	t.Parallel()
	const viaPet = "{allOf: [{$ref: '#/components/schemas/Pet'}]"
	cases := []struct {
		name, version, discriminator, rest, want string
	}{
		{"mapped", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}", cat + dog, `"mapping"`},
		{"named by branch", "3.1.0", "{propertyName: kind}", cat + dog, `"propertyName"`},
		{"one mapped subtype and one branch that is not", "3.1.0",
			"{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}",
			"    Cat: " + viaPet + "}\n" + dog, `"mapping"`},
		{"default", "3.2.0", "{propertyName: kind, defaultMapping: '#/components/schemas/Other'}",
			subtypes + "    Other: {type: object}\n", `"defaultMapping"`},
		{"mixin only", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}",
			"    Cat: {allOf: [{$ref: '#/components/schemas/Dog'}, {$ref: '#/components/schemas/Other'}]}\n" +
				dog + "    Other: {type: object}\n", `"mapping"`},
		{"position declaring an inline object", "3.1.0",
			"{propertyName: kind, mapping: {cat: '#/components/schemas/Holder/properties/pet'}}",
			subtypes + "    Holder: {type: object, properties: {pet: {type: object, properties: {kind: {type: string}}}}}\n",
			`"mapping"`},
		{"component that is only an alias", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Alias'}}",
			subtypes + "    Alias: {$ref: '#/components/schemas/Cat'}\n", `"mapping"`},
		{"lowers to an enum", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}",
			"    Cat: " + viaPet + ", enum: [{kind: cat}]}\n" + subDog, `"mapping"`},
		{"lowers to a literal", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}",
			"    Cat: " + viaPet + ", const: {kind: cat}}\n" + subDog, `"mapping"`},
		{"lowers to an alias over its $ref", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}",
			"    Cat: {$ref: '#/components/schemas/Other', allOf: [{$ref: '#/components/schemas/Pet'}]}\n" +
				"    Other: {type: object}\n" + subDog, `"mapping"`},
		{"lowers to a distributed union", "3.1.0", "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}",
			"    Cat: " + viaPet + ", oneOf: [{$ref: '#/components/schemas/House'}, {$ref: '#/components/schemas/Wild'}]}\n" +
				"    House: {type: object}\n    Wild: {type: object}\n" + subDog, `"mapping"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range discriminatedOrders(t, c.version, petWith(c.discriminator), c.rest) {
				assertMoved(t, doc, c.want)
			}
		})
	}
}

// TestDiscriminatorBesideUnion_LeavesAMapWhenItMoves pins that a discriminator
// moving off a body whose one other member fact is its catch-all leaves a map,
// which is a MapT, as it is when no discriminator is written (ir-design §4.6).
func TestDiscriminatorBesideUnion_LeavesAMapWhenItMoves(t *testing.T) {
	t.Parallel()
	for _, doc := range discriminatedOrders(t, "3.1.0", mapPet, cat+dog) {
		assertMovedToMap(t, typeByName(doc, "Pet"))
		assert.Empty(t, censusReports(doc.Diagnostics, "/components/schemas/Pet"),
			"the map holds all the census asked about, and the move keeps the discriminator itself")
	}
}

// TestDiscriminatorBesideUnion_SettlesAMapAReferenceNamesFirst is
// TestDiscriminatorBesideUnion_SettlesAPositionAReferenceNamesFirst over a map
// body: the reference builds nothing, so the one settlement the declaration
// defers moves the discriminator and leaves the MapT, in either order.
func TestDiscriminatorBesideUnion_SettlesAMapAReferenceNamesFirst(t *testing.T) {
	t.Parallel()
	const at = "/components/schemas/Holder/properties/pet"
	outside := "    Outside: {$ref: '#/components/schemas/Holder/properties/pet'}\n"
	holder := "    Holder:\n      type: object\n      properties:\n        pet:\n          type: object\n" +
		"          additionalProperties: {type: string}\n          discriminator: {propertyName: kind}\n" +
		"          oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Dog'}]\n"
	for _, doc := range discriminatedOrders(t, "3.1.0", outside, holder+cat+dog) {
		assertMovedToMap(t, doc.Types["t/anon"+at])
		assert.Contains(t, openapitest.DiagMessageAt(t, doc.Diagnostics, diag.DegradedConstruct, ir.SeverityInfo, at+"/discriminator"),
			"not a subtype", "one move, reported once")
		assert.Empty(t, censusReports(doc.Diagnostics, at), "the map homes all the census asked about")
	}
}

// TestDiscriminatorBesideUnion_ARequiredNameKeepsTheBodyAModel pins a body
// whose catch-all sits beside a required name its properties do not declare:
// the name is in Model.Requires, which no map holds, so the discriminator's
// move leaves a Model, nothing kept verbatim for want of a home. It holds for
// a plain object, a composed one, and the object variant of a multi-typed body.
func TestDiscriminatorBesideUnion_ARequiredNameKeepsTheBodyAModel(t *testing.T) {
	t.Parallel()
	const (
		body    = "      additionalProperties: {type: string}\n      required: [a]\n      discriminator: {propertyName: kind}\n" + petBranches
		pointer = "/components/schemas/Pet"
	)
	for _, tc := range []struct {
		name, pet string
		node      ir.TypeID
	}{
		{"plain", "    Pet:\n      type: object\n" + body, componentID("Pet")},
		{"composed", "    Pet:\n      allOf: [{type: object}]\n" + body, componentID("Pet")},
		{"variant", "    Pet:\n      type: [object, string]\n" + body, "t/anon" + pointer + "/type/0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range discriminatedOrders(t, "3.1.0", tc.pet, cat+dog) {
				m, ok := doc.Types[tc.node].(*ir.Model)
				require.True(t, ok, "a required name keeps the body a Model; got %T", doc.Types[tc.node])
				assert.Nil(t, m.Discriminator, "the discriminator moved")
				assert.Equal(t, []string{"a"}, m.Requires)
				assert.NotContains(t, m.Unmodeled, "openapi:required")
				assert.Empty(t, censusReports(doc.Diagnostics, pointer))
			}
		})
	}
}

// TestDiscriminatorBesideUnion_KeepsARoutingMapAModel pins the other outcome:
// a discriminator every route of which is a subtype stays, and a model that is
// a hierarchy's base is no map, whatever else it declares.
func TestDiscriminatorBesideUnion_KeepsARoutingMapAModel(t *testing.T) {
	t.Parallel()
	for _, doc := range discriminatedOrders(t, "3.1.0", mapPet, subtypes) {
		assertStayed(t, doc)
		assert.NotNil(t, petModel(t, doc).AdditionalProps, "the catch-all stays beside the discriminator")
	}
}

// TestDiscriminatorBesideUnion_StaysWhenEveryRouteIsASubtype pins the case the
// model keeps its discriminator beside a kept union: every route is a subtype,
// at any distance and however it is spelled. A branch no name reaches, inline
// or a position, is no route, so it does not move the discriminator either.
func TestDiscriminatorBesideUnion_StaysWhenEveryRouteIsASubtype(t *testing.T) {
	t.Parallel()
	const grand = "    Cat: {allOf: [{$ref: '#/components/schemas/Mid'}], type: object}\n" +
		"    Mid: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object}\n" +
		"    Dog: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object}\n"
	unnamed := "    Pet:\n      type: object\n      discriminator: {propertyName: kind}\n" +
		"      oneOf: [{type: object, properties: {kind: {type: string}}}, {$ref: '#/components/schemas/Holder/properties/pet'}]\n"
	cases := []struct {
		name, version, pet, rest string
	}{
		{"mapped", "3.1.0", petWith("{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}"), subtypes},
		{"named by branch", "3.1.0", petWith("{propertyName: kind}"), subtypes},
		{"mapped by bare name", "3.1.0", petWith("{propertyName: kind, mapping: {cat: Cat, dog: Dog}}"), subtypes},
		{"mapped through a position that is only a $ref", "3.1.0",
			petWith("{propertyName: kind, mapping: {cat: '#/components/schemas/Holder/properties/pet'}}"),
			subtypes + "    Holder: {type: object, properties: {pet: {$ref: '#/components/schemas/Cat'}}}\n"},
		{"grandchild", "3.1.0", petWith("{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}"), grand},
		{"default", "3.2.0", petWith("{propertyName: kind, defaultMapping: '#/components/schemas/Dog'}"), subtypes},
		{"branches no name reaches", "3.1.0", unnamed, cat + "    Holder: {type: object, properties: {pet: {type: object}}}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range discriminatedOrders(t, c.version, c.pet, c.rest) {
				assertStayed(t, doc)
			}
		})
	}
}

// TestDiscriminatorBesideUnion_StaysForASubtypeAtAnyDepth pins that no depth bound
// stands between a route and Pet: pass.Validate follows a hierarchy to any
// depth, so a bound here would move a discriminator it accepts.
func TestDiscriminatorBesideUnion_StaysForASubtypeAtAnyDepth(t *testing.T) {
	t.Parallel()
	var chain strings.Builder
	for i := range 300 {
		fmt.Fprintf(&chain, "    C%d: {allOf: [{$ref: '#/components/schemas/C%d'}], type: object}\n", i, i+1)
	}
	chain.WriteString("    C300: {allOf: [{$ref: '#/components/schemas/Pet'}], type: object}\n")
	pet := petWith("{propertyName: kind, mapping: {c: '#/components/schemas/C0'}}")
	for _, doc := range discriminatedOrders(t, "3.1.0", pet, subtypes+chain.String()) {
		assertStayed(t, doc)
	}
}

// TestDiscriminatorBesideUnion_ReportsTheMoveOnce pins the diagnostic: exactly
// one, at the discriminator, when it moves, in either declaration order, and
// none when it stays.
func TestDiscriminatorBesideUnion_ReportsTheMoveOnce(t *testing.T) {
	t.Parallel()
	pet := petWith("{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}")
	for _, src := range []string{pet + cat + dog, cat + dog + pet} {
		_, diags := parseFull(t, openapitest.ComponentSpec(src))
		assert.Contains(t, openapitest.DiagMessageAt(t, diags, diag.DegradedConstruct, ir.SeverityInfo, movedPointer),
			"not a subtype of the model holding it")
	}
	_, diags := parseFull(t, openapitest.ComponentSpec(pet+subtypes))
	assert.False(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, movedPointer))
}

// TestDiscriminatorBesideUnion_EveryDegradedShapeIsSettled pins that the rule
// holds on every union kept verbatim beside a model, not only one its
// discriminator alone degrades, both ways.
func TestDiscriminatorBesideUnion_EveryDegradedShapeIsSettled(t *testing.T) {
	t.Parallel()
	for _, shape := range degradedShapes {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range discriminatedOrders(t, "3.1.0", degradedPet(shape.branches), cat+dog) {
				assertMoved(t, doc, `"mapping"`)
			}
			for _, doc := range discriminatedOrders(t, "3.1.0", degradedPet(shape.branches), subtypes) {
				assertStayed(t, doc)
			}
		})
	}
}

// TestDiscriminatorBesideUnion_AllOfBodyIsSettled pins the other model body a
// kept union can sit beside: one composed with allOf lowers through
// lowerAllOf, and its discriminator is settled the same way.
func TestDiscriminatorBesideUnion_AllOfBodyIsSettled(t *testing.T) {
	t.Parallel()
	pet := "    Pet:\n      allOf: [{$ref: '#/components/schemas/Base'}]\n" +
		"      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}\n" + petBranches
	for _, doc := range discriminatedOrders(t, "3.1.0", pet, cat+dog+"    Base: {type: object}\n") {
		assertMoved(t, doc, `"mapping"`)
	}
}

// TestDiscriminatorBesideUnion_MultiTypedBodyIsSettled pins the other model a
// discriminator beside a kept union can sit on: a multi-typed body lowers to a
// union whose object variant lowerUnion builds from the same schema, so the
// variant holds it. Routed to a non-subtype, it moves to the node keeping the
// union; with no route at all, it stays on the variant.
func TestDiscriminatorBesideUnion_MultiTypedBodyIsSettled(t *testing.T) {
	t.Parallel()
	const variant = ir.TypeID("t/anon/components/schemas/Pet/type/0")
	multi := func(discriminator, branches string) string {
		return "    Pet:\n      type: [object, string]\n      properties: {kind: {type: string}}\n" +
			"      discriminator: " + discriminator + "\n" + branches
	}
	cases := []struct {
		name, pet string
		moves     bool
	}{
		{"mapped to a non-subtype", multi("{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}", petBranches), true},
		{"no route", multi("{propertyName: kind}", "      oneOf: [{type: object, properties: {kind: {type: string}}}, {type: string}]\n"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, doc := range discriminatedOrders(t, "3.1.0", c.pet, cat+dog) {
				owner, ok := doc.Types[componentID("Pet")].(*ir.Union)
				require.True(t, ok, "a multi-typed body lowers to a union")
				assert.Contains(t, owner.Unmodeled, "openapi:oneOf", "the union is kept on it")
				held, ok := doc.Types[variant].(*ir.Model)
				require.True(t, ok, "its object variant is a model")
				assert.NotContains(t, held.Unmodeled, "openapi:discriminator", "the variant never keeps it verbatim")
				if !c.moves {
					assert.NotNil(t, held.Discriminator, "nothing routes anywhere, so it stays")
					assert.NotContains(t, owner.Unmodeled, "openapi:discriminator")
					continue
				}
				assert.Nil(t, held.Discriminator)
				entry, ok := owner.Unmodeled["openapi:discriminator"]
				require.True(t, ok, "it is kept beside the union it was written for")
				assert.Equal(t, ir.Provenance{Pointer: movedPointer}, entry.Provenance)
				assert.Contains(t, string(entry.Value), `"mapping"`)
			}
		})
	}
}

// TestDiscriminatorBesideUnion_MovesOnlyWhatItCopied pins that the move never
// loses a discriminator it could not keep verbatim. One merged in through `<<`
// is read as a written one is (GitHub #395), so Pet lowers as it does with the
// discriminator written in place. One that converts to no JSON, through an
// extension written `.nan`, is reported as unpreservable and stays on the
// model, and nothing claims a copy never made.
func TestDiscriminatorBesideUnion_MovesOnlyWhatItCopied(t *testing.T) {
	t.Parallel()
	const disc = "{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}"
	written, _ := parseFull(t, openapitest.ComponentSpec(cat+dog+petWith(disc)))
	merged, diags := parseFull(t, openapitest.ComponentSpec(cat+dog+
		"    Pet:\n      <<: {discriminator: "+disc+"}\n      type: object\n"+petBranches))
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, cmp.Diff(typeByName(written, "Pet"), typeByName(merged, "Pet")),
		"a merged discriminator lowers as a written one")

	doc, diags := parseFull(t, openapitest.ComponentSpec(cat+dog+
		petWith("{propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}, x-weight: .nan}")))
	pet := petModel(t, doc)
	require.NotNil(t, pet.Discriminator, "the model read it, so it is not lost")
	assert.Equal(t, "kind", pet.Discriminator.PropertyName)
	assert.NotContains(t, pet.Unmodeled, "openapi:discriminator")
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.UnpreservableConstruct, movedPointer),
		"the failed copy is reported: %+v", diags)
	assert.False(t, slices.ContainsFunc(diags, func(d ir.Diagnostic) bool {
		return strings.Contains(d.Message, "beside the union it was written for")
	}), "no move is announced")
}

// TestDiscriminatorBesideUnion_UnreachableBranchesAreNoRoute pins that a
// branch the discriminator cannot route to by name decides nothing, whichever
// way it is unreachable: a $ref to an undeclared component, or one into
// another document, though its fragment spells a component this one declares,
// Lone, which is no subtype. The unresolved $refs are error diagnostics, so
// the document is checked with pass.Validate directly.
func TestDiscriminatorBesideUnion_UnreachableBranchesAreNoRoute(t *testing.T) {
	t.Parallel()
	const branches = "      oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Nope'}, " +
		"{$ref: 'https://example.com/x.yaml#/components/schemas/Lone'}]\n"
	const lone = "    Lone: {type: object}\n"
	for _, c := range []struct {
		name, rest string
		stays      bool
	}{
		{"beside a subtype", subtypes + lone, true},
		{"beside a non-subtype", cat + dog + lone, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			pet := degradedPet(branches)
			docs := make([]*ir.Document, 0, 2)
			for _, src := range []string{pet + c.rest, c.rest + pet} {
				doc, _ := parseFull(t, openapitest.ComponentSpec(src))
				assert.Empty(t, pass.Validate(doc), "the document must validate")
				docs = append(docs, doc)
				if c.stays {
					assertStayed(t, doc)
					continue
				}
				assertMoved(t, doc, `"mapping"`)
			}
			assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types, orderInvariantIR()...))
			assert.Equal(t, reported(docs[0].Diagnostics), reported(docs[1].Diagnostics))
		})
	}
}

// TestDiscriminatorBesideUnion_CyclicRouteMoves pins that a route whose
// hierarchy loops without reaching Pet is no subtype, and the check ends.
func TestDiscriminatorBesideUnion_CyclicRouteMoves(t *testing.T) {
	t.Parallel()
	pet := petWith("{propertyName: kind, mapping: {loop: '#/components/schemas/Loop'}}")
	rest := subtypes +
		"    Loop: {allOf: [{$ref: '#/components/schemas/LoopB'}]}\n" +
		"    LoopB: {allOf: [{$ref: '#/components/schemas/Loop'}]}\n"
	for _, doc := range discriminatedOrders(t, "3.1.0", pet, rest) {
		assertMoved(t, doc, `"loop"`)
	}
}

// TestDiscriminatorBesideUnion_SubtypeKeepsItsTag pins what a move leaves
// alone: a route that is a subtype still carries the tag the mapping gives it,
// since that is the value its instances carry on the wire.
func TestDiscriminatorBesideUnion_SubtypeKeepsItsTag(t *testing.T) {
	t.Parallel()
	pet := petWith("{propertyName: kind, mapping: {cat: '#/components/schemas/Cat', dog: '#/components/schemas/Dog'}}")
	rest := subCat + dog
	for _, doc := range discriminatedOrders(t, "3.1.0", pet, rest) {
		assertMoved(t, doc, `"dog"`)
		sub, ok := typeByName(doc, "Cat").(*ir.Model)
		require.True(t, ok)
		assert.Equal(t, "cat", sub.DiscriminatorValue)
	}
}

// TestDiscriminatorBesideUnion_SettlesAnOperationsSchemaToo pins that the
// settlement waits for the operations as well as the components: a request
// body lowers in the service walk, after every component.
func TestDiscriminatorBesideUnion_SettlesAnOperationsSchemaToo(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /pets:
    post:
      requestBody:
        content:
          application/json:
            schema:
              type: object
              discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}
              oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Dog'}]
      responses:
        "204": {description: none}
components:
  schemas:
` + cat + dog
	doc, diags := parseFull(t, spec)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, pass.Validate(doc))
	const at = "/paths/~1pets/post/requestBody/content/application~1json/schema/discriminator"
	assert.True(t, openapitest.HasDiagCodeAt(diags, diag.DegradedConstruct, at), "%+v", diags)
}

// TestDiscriminatorBesideUnion_SettlesAPositionAReferenceNamesFirst pins a
// model a $ref names before its declaration lowers it: the reference builds
// nothing, so the declaration alone lowers the model and defers its settlement,
// which moves the discriminator off it once, in either order.
func TestDiscriminatorBesideUnion_SettlesAPositionAReferenceNamesFirst(t *testing.T) {
	t.Parallel()
	const at = "/components/schemas/Holder/properties/pet"
	outside := "    Outside: {$ref: '#/components/schemas/Holder/properties/pet'}\n"
	holder := "    Holder:\n      type: object\n      properties:\n        pet:\n          type: object\n" +
		"          discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}\n" +
		"          oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Dog'}]\n"
	for _, doc := range discriminatedOrders(t, "3.1.0", outside, holder+cat+dog) {
		pet, ok := doc.Types["t/anon"+at].(*ir.Model)
		require.True(t, ok)
		assert.Nil(t, pet.Discriminator)
		assert.Contains(t, pet.Unmodeled, "openapi:discriminator")
		assert.Contains(t, openapitest.DiagMessageAt(t, doc.Diagnostics, diag.DegradedConstruct, ir.SeverityInfo, at+"/discriminator"),
			"not a subtype", "one move, reported once")
	}
}

// TestMapping_InAnotherDocumentIsReportedWhereItIsWritten pins the report of a
// discriminator mapping value another document writes that names nothing
// there: once, at the value in that document's Document.Sources entry, in the
// words a mapping in the source draws, so the message does not name the
// document its Provenance already does. The value naming what it declares
// resolves and draws nothing.
func TestMapping_InAnotherDocumentIsReportedWhereItIsWritten(t *testing.T) {
	t.Parallel()
	_, diags, _ := compileBeside(t, map[string]string{"pets.yaml": `Pet:
  oneOf: [{$ref: '#/Cat'}]
  discriminator: {propertyName: kind, mapping: {cat: '#/Cat', dog: '#/Dog'}}
Cat: {type: object, properties: {kind: {type: string}}}
`}, `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    A: {$ref: './pets.yaml#/Pet'}
`)

	assert.Empty(t, cmp.Diff([]ir.Diagnostic{{Severity: ir.SeverityError, Code: "openapi/unresolved-ref",
		Message: `discriminator mapping "dog" references unresolved schema "#/Dog": ` +
			"it names nothing in the document it points into",
		Provenance: ir.Provenance{Source: 1, Pointer: "/Pet/discriminator/mapping/dog"}}},
		withCode(diags, "openapi/unresolved-ref")))
}

// TestDiscriminatorMapping_ToRawTargetsIsOrderInvariant pins that a mapping or
// defaultMapping naming a position the parsed model holds as raw YAML, an
// extension's value or an enum member, resolves to the type at that position
// whichever order the document declares things in (GitHub #757). Each row is
// written in the order that failed, with the mapping before any $ref to its
// target, and compiled again reversed. Neither compile may report an error,
// and both must produce the same registry.
func TestDiscriminatorMapping_ToRawTargetsIsOrderInvariant(t *testing.T) {
	t.Parallel()
	const (
		petSub = "allOf: [{$ref: '#/components/schemas/Pet'}], type: object"
		cat    = "  Cat: {" + petSub + ", properties: {meow: {type: string}}}\n"
	)
	pet := componentID("Pet")
	anon := func(pointer string) ir.TypeID { return ir.TypeID("t/anon/" + pointer) }
	model := func(base ir.TypeID, tag, pointer string) wantMapping {
		return wantMapping{base: base, tag: tag, target: anon(pointer), kind: ir.KindModel}
	}
	tests := []struct {
		name       string
		version    string
		components []string
		tail       string
		wants      []wantMapping
	}{
		{
			name:       "an extension's value named by a mapping alone",
			components: []string{rawBase("mapping: {c: '#/x-lib/Cat'}")},
			tail:       "x-lib:\n" + cat,
			wants:      []wantMapping{model(pet, "c", "x-lib/Cat")},
		},
		{
			name: "an extension's value also named by a $ref",
			components: []string{
				rawBase("mapping: {c: '#/x-lib/Cat'}"),
				"    Holder: {$ref: '#/x-lib/Cat'}\n",
			},
			tail:  "x-lib:\n" + cat,
			wants: []wantMapping{model(pet, "c", "x-lib/Cat")},
		},
		{
			name: "an enum member",
			components: []string{
				rawBase("mapping: {e: '#/components/schemas/Kennel/properties/e/enum/0'}"),
				"    Kennel:\n      type: object\n      properties:\n        e:\n          enum:\n" +
					"            - {type: object, properties: {wingspan: {type: number}}}\n",
			},
			// A member that is an object is lowered to a literal, which no model
			// subtypes, so only the resolution is pinned here, not a variant.
			wants: []wantMapping{{pet, "e", anon("components/schemas/Kennel/properties/e/enum/0"), ir.KindLiteral}},
		},
		{
			name:       "a defaultMapping to an extension's value",
			version:    "3.2.0",
			components: []string{rawBase("defaultMapping: '#/x-lib/Cat'")},
			tail:       "x-lib:\n" + cat,
			wants:      []wantMapping{model(pet, "", "x-lib/Cat")},
		},
		{
			name:       "a mapping in a raw schema only a $ref reaches",
			components: []string{"    Holder: {$ref: '#/x-lib/Inner'}\n"},
			tail: "x-lib:\n" +
				"  Inner:\n    type: object\n    required: [tag]\n    properties: {tag: {type: string}}\n" +
				"    discriminator: {propertyName: tag, mapping: {d: '#/x-lib/Dog'}}\n" +
				"  Dog: {allOf: [{$ref: '#/x-lib/Inner'}], type: object}\n",
			wants: []wantMapping{model(anon("x-lib/Inner"), "d", "x-lib/Dog")},
		},
		{
			name:       "a mapping in a raw schema only another mapping reaches",
			components: []string{rawBase("mapping: {i: '#/x-lib/Inner'}")},
			tail: "x-lib:\n" +
				"  Inner:\n    allOf: [{$ref: '#/components/schemas/Pet'}]\n    type: object\n" +
				"    required: [tag]\n    properties: {tag: {type: string}}\n" +
				"    discriminator: {propertyName: tag, mapping: {d: '#/x-lib/Dog'}}\n" +
				"  Dog: {allOf: [{$ref: '#/x-lib/Inner'}], type: object}\n",
			wants: []wantMapping{
				model(pet, "i", "x-lib/Inner"),
				model(anon("x-lib/Inner"), "d", "x-lib/Dog"),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			version := tc.version
			if version == "" {
				version = "3.1.0"
			}
			first, diags := parseFull(t, rawSpec(version, tc.components, tc.tail, false))
			openapitest.RequireNoErrorDiags(t, diags)
			last, diags := parseFull(t, rawSpec(version, tc.components, tc.tail, true))
			openapitest.RequireNoErrorDiags(t, diags)

			for _, doc := range []*ir.Document{first, last} {
				for _, want := range tc.wants {
					base, ok := doc.Types[want.base].(*ir.Model)
					require.True(t, ok, "%s is a model", want.base)
					require.NotNil(t, base.Discriminator, "%s is discriminated", want.base)

					got := base.Discriminator.Default
					if want.tag != "" {
						got = base.Discriminator.Mapping[want.tag]
					}
					assert.Equal(t, want.target, got, "%s %q", want.base, want.tag)

					target, ok := doc.Types[want.target]
					require.True(t, ok, "the target %s is in the registry", want.target)
					assert.Equal(t, want.kind, target.Kind(), "the kind of %s", want.target)
				}
			}

			assert.Empty(t, cmp.Diff(first.Types, last.Types),
				"declaring the mapping before or after what names its target must not change the registry")
		})
	}
}

// discriminatorValues reads the DiscriminatorValue of every Model named in
// typeIDs off doc, so a mismatch anywhere in the set shows every id at once
// rather than only the first one asserted.
func discriminatorValues(t *testing.T, doc *ir.Document, typeIDs ...ir.TypeID) map[ir.TypeID]string {
	t.Helper()
	got := make(map[ir.TypeID]string, len(typeIDs))
	for _, id := range typeIDs {
		m, ok := doc.Types[id].(*ir.Model)
		require.True(t, ok, "%s should be a model", id)
		got[id] = m.DiscriminatorValue
	}
	return got
}

// discriminatorOf returns the discriminator the model or union id declares.
func discriminatorOf(t *testing.T, doc *ir.Document, id ir.TypeID) *ir.Discriminator {
	t.Helper()
	switch td := doc.Types[id].(type) {
	case *ir.Model:
		require.NotNil(t, td.Discriminator, id)
		return td.Discriminator
	case *ir.Union:
		require.NotNil(t, td.Discriminator, id)
		return td.Discriminator
	default:
		require.Failf(t, "not a discriminated type", "%s is a %T", id, td)
		return nil
	}
}

const (
	cat = "    Cat: {type: object, properties: {kind: {type: string}, meow: {type: boolean}}}\n"
	dog = "    Dog: {type: object, properties: {kind: {type: string}, bark: {type: boolean}}}\n"

	// subCat and subDog declare Cat and Dog as allOf subtypes of Pet.
	subCat   = "    Cat: {allOf: [{$ref: '#/components/schemas/Pet'}, {type: object, properties: {meow: {type: boolean}}}]}\n"
	subDog   = "    Dog: {allOf: [{$ref: '#/components/schemas/Pet'}, {type: object, properties: {bark: {type: boolean}}}]}\n"
	subtypes = subCat + subDog

	petBranches = "      oneOf:\n        - $ref: '#/components/schemas/Cat'\n        - $ref: '#/components/schemas/Dog'\n"

	// movedPointer is where the diagnostic for a discriminator kept verbatim
	// rather than on Pet is reported.
	movedPointer = "/components/schemas/Pet/discriminator"
)

// petWith declares Pet, a model whose oneOf names Cat and Dog, beside the
// discriminator text given.
func petWith(discriminator string) string {
	return "    Pet:\n      type: object\n      discriminator: " + discriminator + "\n" + petBranches
}

// discriminatedOrders compiles pet and rest in both declaration orders, rest
// first second, and requires each to validate and the two to agree on their
// types and on what they report. A decision read off the registry while Pet
// lowers sees no subtype declared after it.
func discriminatedOrders(t *testing.T, version, pet, rest string) []*ir.Document {
	t.Helper()
	docs := make([]*ir.Document, 0, 2)
	for _, src := range []string{pet + rest, rest + pet} {
		doc, diags := parseFull(t, openapitest.ComponentSpecVer(version, src))
		openapitest.RequireNoErrorDiags(t, diags)
		assert.Empty(t, pass.Validate(doc), "the document must validate")
		docs = append(docs, doc)
	}
	assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types, orderInvariantIR()...),
		"declaration order must not change the types")
	assert.Equal(t, reported(docs[0].Diagnostics), reported(docs[1].Diagnostics),
		"declaration order must not change what is reported, only the order it is reported in")
	return docs
}

// assertMoved requires Pet's discriminator to be kept verbatim, whole, rather
// than on the model, and want to appear in what was kept.
func assertMoved(t *testing.T, doc *ir.Document, want string) {
	t.Helper()
	pet := petModel(t, doc)
	assert.Nil(t, pet.Discriminator, "a discriminator routing to a non-subtype stays off the model")
	entry, ok := pet.Unmodeled["openapi:discriminator"]
	require.True(t, ok, "the discriminator is kept verbatim")
	assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
	assert.Equal(t, ir.Provenance{Pointer: movedPointer}, entry.Provenance)
	assert.Contains(t, string(entry.Value), `"propertyName":"kind"`, "the whole object is kept")
	assert.Contains(t, string(entry.Value), want)
}

// assertStayed requires Pet to hold its discriminator and to store it nowhere
// else.
func assertStayed(t *testing.T, doc *ir.Document) {
	t.Helper()
	pet := petModel(t, doc)
	require.NotNil(t, pet.Discriminator, "every route is a subtype, so the model keeps it")
	assert.Equal(t, "kind", pet.Discriminator.PropertyName)
	assert.NotContains(t, pet.Unmodeled, "openapi:discriminator", "nothing is stored twice")
}

// mapPet declares Pet as a map beside its union: a catch-all is its only member
// fact other than the discriminator.
const mapPet = "    Pet:\n      type: object\n      additionalProperties: {type: string}\n" +
	"      discriminator: {propertyName: kind}\n" + petBranches

// censusReports returns the messages of the census reports made at pointer,
// each naming the keywords that position kept for want of a home.
func censusReports(diags []ir.Diagnostic, pointer jsontext.Pointer) []string {
	var out []string
	for _, d := range diags {
		if d.Code == diag.DegradedConstruct && d.Provenance.Pointer == pointer &&
			strings.HasPrefix(d.Message, "this position lowered to ") {
			out = append(out, d.Message)
		}
	}
	return out
}

// assertMovedToMap requires td to be the MapT a map body is once its
// discriminator has moved, holding the union and the discriminator verbatim.
func assertMovedToMap(t *testing.T, td ir.TypeDef) {
	t.Helper()
	mt, ok := td.(*ir.MapT)
	require.True(t, ok, "got %T", td)
	assert.Equal(t, ir.TypeRef{Target: "t/prim/string"}, mt.Value)
	assert.Contains(t, mt.Unmodeled, "openapi:oneOf", "the union is kept beside it")
	assert.Contains(t, mt.Unmodeled, "openapi:discriminator", "and the discriminator with it")
}

// degradedShapes are the cases besides the discriminator itself that keep a
// union verbatim beside a model, each with the branch text that makes the
// union that case. Each goes through lowerBesideUnmodeledUnion.
var degradedShapes = []struct {
	name     string
	branches string
}{
	{"inline branch", "      oneOf: [{$ref: '#/components/schemas/Cat'}, {type: object, properties: {kind: {type: string}}}]\n"},
	{"oneOf beside anyOf", "      oneOf: [{$ref: '#/components/schemas/Cat'}]\n      anyOf: [{$ref: '#/components/schemas/Dog'}]\n"},
	{"validation-only branches", "      oneOf: [{required: [kind]}, {required: [x]}]\n"},
}

// degradedPet declares Pet, a model whose discriminator maps cat to Cat,
// beside the branches given.
func degradedPet(branches string) string {
	return "    Pet:\n      type: object\n" +
		"      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}\n" + branches
}

// petModel returns Pet, which lowers to a model beside its kept union.
func petModel(t *testing.T, doc *ir.Document) *ir.Model {
	t.Helper()
	pet, ok := typeByName(doc, "Pet").(*ir.Model)
	require.True(t, ok, "Pet lowers to a model beside its union")
	assert.Contains(t, pet.Unmodeled, "openapi:oneOf", "the union is kept beside it")
	return pet
}

// rawBase is a discriminated Pet whose mapping is mapping, followed by whatever
// the row declares after it.
func rawBase(mapping string) string {
	return "    Pet:\n" +
		"      type: object\n" +
		"      required: [kind]\n" +
		"      properties: {kind: {type: string}}\n" +
		"      discriminator: {propertyName: kind, " + mapping + "}\n"
}

// rawSpec writes components in the order given, and the root-level extensions
// tail after them. With reversed set it declares the components in the opposite
// order and the extensions ahead of them.
func rawSpec(version string, components []string, tail string, reversed bool) string {
	if !reversed {
		return openapitest.ComponentSpecVer(version, strings.Join(components, "")) + tail
	}
	rev := make([]string, len(components))
	for i, c := range components {
		rev[len(components)-1-i] = c
	}
	doc := openapitest.ComponentSpecVer(version, strings.Join(rev, ""))
	return strings.Replace(doc, "paths: {}\n", tail+"paths: {}\n", 1)
}

// wantMapping names a discriminator, one of its mapping entries (or its default
// when tag is empty), and the type of the given kind that entry must resolve to.
type wantMapping struct {
	base   ir.TypeID
	tag    string
	target ir.TypeID
	kind   ir.TypeKind
}
