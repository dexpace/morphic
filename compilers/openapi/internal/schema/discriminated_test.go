package schema_test

import (
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

// reported renders diags as a sorted multiset of what was reported where, since
// diagnostics are appended in the order the walk reaches them.
func reported(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, fmt.Sprintf("%s %s %s: %s", d.Severity, d.Code, d.Provenance.Pointer, d.Message))
	}
	slices.Sort(out)
	return out
}

// petModel returns Pet, which lowers to a model beside its kept union.
func petModel(t *testing.T, doc *ir.Document) *ir.Model {
	t.Helper()
	pet, ok := typeByName(doc, "Pet").(*ir.Model)
	require.True(t, ok, "Pet lowers to a model beside its union")
	assert.Contains(t, pet.Unmodeled, "openapi:oneOf", "the union is kept beside it")
	return pet
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

// degradedShapes are the model-bodied unions kept verbatim beside a
// discriminator other than the discriminated one, each with the branch text
// that makes it that shape. Each goes through lowerBesideUnmodeledUnion.
var degradedShapes = []struct {
	name     string
	branches string
}{
	{"inline branch", "      oneOf: [{$ref: '#/components/schemas/Cat'}, {type: object, properties: {kind: {type: string}}}]\n"},
	{"oneOf beside anyOf", "      oneOf: [{$ref: '#/components/schemas/Cat'}]\n      anyOf: [{$ref: '#/components/schemas/Dog'}]\n"},
	{"validation-only branches", "      oneOf: [{required: [kind]}, {required: [x]}]\n"},
}

func degradedPet(branches string) string {
	return "    Pet:\n      type: object\n" +
		"      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}\n" + branches
}

// TestDiscriminatorBesideUnion_EveryDegradedShapeIsSettled pins that the rule
// holds on every union kept verbatim beside a model, not only the
// discriminated one, both ways.
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

// TestDiscriminatorBesideUnion_SettlesAPositionAReferenceBuiltFirst pins a
// model two settlements are deferred for: a $ref reaches the position first
// and builds it, then the declaration rebuilds it. The first moves the
// discriminator off the rebuilt model; the second finds none left to move.
func TestDiscriminatorBesideUnion_SettlesAPositionAReferenceBuiltFirst(t *testing.T) {
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
		assert.Equal(t, 1, strings.Count(fmt.Sprint(reported(doc.Diagnostics)), at+"/discriminator"),
			"one move, reported once")
	}
}
