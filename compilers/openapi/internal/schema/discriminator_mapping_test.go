package schema_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

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
	// The two cases below straddle the cap.
	const hopCap = schema.MaxTypePositionHops
	for _, tc := range []struct {
		name  string
		links int
		want  ir.TypeID
	}{
		{"as long as the cap", hopCap, componentID("Dog")},
		{"beyond the cap", hopCap + 1, ir.TypeID(fmt.Sprintf("t/anon/components/schemas/Kennel/properties/a%d", hopCap))},
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
