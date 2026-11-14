// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema_test

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/schema"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

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
	doc, diags := schema.LowerSpec(t, spec)
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
	doc, _ := schema.LowerSpec(t, spec)
	u := typeByName(doc, "Shape").(*ir.Union)
	require.NotNil(t, u.Discriminator)
	assert.Equal(t, "shapeType", u.Discriminator.PropertyName)
	assert.NotEmpty(t, u.Discriminator.Default)
}

// TestDiscriminatorMapping_RoutesToTheVariantAliasingItsTarget pins that a
// mapping names a variant where a keyword beside a branch's $ref gave the branch
// an alias over the component the mapping names, spelled either way and past a
// null branch, and that a component a variant names itself stays the target.
func TestDiscriminatorMapping_RoutesToTheVariantAliasingItsTarget(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    Dog: {type: object, properties: {kind: {type: string}}}
    Pet:
      oneOf:
        - {type: "null"}
        - {$ref: '#/components/schemas/Cat', description: a cat}
        - {$ref: '#/components/schemas/Dog'}
      discriminator:
        propertyName: kind
        mapping: {cat: '#/components/schemas/Cat', tom: Cat, dog: '#/components/schemas/Dog'}
    Pair:
      oneOf:
        - {$ref: '#/components/schemas/Cat'}
        - {$ref: '#/components/schemas/Cat', description: an alias beside the type itself}
      discriminator: {propertyName: kind, mapping: {cat: '#/components/schemas/Cat'}}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, validated(t, doc))
	pet, ok := typeByName(doc, "Pet").(*ir.Union)
	require.True(t, ok)
	alias := pet.Variants[0].Type.Target
	assert.NotEqual(t, ir.TypeID("t/openapi/components/schemas/Cat"), alias)
	assert.Empty(t, cmp.Diff(map[string]ir.TypeID{
		"cat": alias, "tom": alias, "dog": "t/openapi/components/schemas/Dog",
	}, pet.Discriminator.Mapping))
	pair, ok := typeByName(doc, "Pair").(*ir.Union)
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Cat"), pair.Discriminator.Mapping["cat"],
		"the type itself is a variant, so the mapping stays on it")
}

// TestDiscriminatorMapping_WritesTheRoutesItImplies pins how a union's mapping
// holds the routes OpenAPI implies (docs/compilers/openapi.md#discriminator):
// where it declares an entry, or a variant's type is not the component its
// branch names, each variant no entry routes takes the name of that component,
// past a null branch, unless two branches name it, an entry spells it already
// or the branch names none. A union whose variants are the components their
// branches name, with no mapping, routes by their names and holds none. Each is
// read in both declaration orders, and the policy reading tags reads nothing in.
func TestDiscriminatorMapping_WritesTheRoutesItImplies(t *testing.T) {
	t.Parallel()
	const cat, dog = "{$ref: '#/components/schemas/Cat'}", "{$ref: '#/components/schemas/Dog'}"
	const aliasedCat = "{$ref: '#/components/schemas/Cat', description: a cat}"
	const inline = "{type: object, properties: {kind: {type: string}}}"
	cases := []struct {
		name, ver, branches, mapping string
		want                         map[string]string // tag -> the variant's target; "alias" for U/oneOf/0's
	}{
		// OpenAPI routes the value Cat to Cat beside the entry, but a mapping is
		// what a writer takes Cat's tag from, and the key Cat would give it a
		// second tag to write, which nothing could mark as only read (#1863).
		{"an entry beside an unnamed variant", "3.1.0", cat + ", " + dog, "{cat: Cat}",
			map[string]string{"cat": "Cat", "Dog": "Dog"}},
		{"past a null branch", "3.1.0", "{type: 'null'}, " + cat + ", " + dog, "{cat: Cat}",
			map[string]string{"cat": "Cat", "Dog": "Dog"}},
		{"an aliased variant", "3.1.0", aliasedCat + ", " + dog, "",
			map[string]string{"Cat": "alias", "Dog": "Dog"}},
		{"the components themselves", "3.1.0", cat + ", " + dog, "", nil},
		{"an entry spelling the name", "3.1.0", cat + ", " + dog, "{Dog: Cat}", map[string]string{"Dog": "Cat"}},
		{"two branches naming one", "3.1.0", aliasedCat + ", " + cat + ", " + dog, "{d: Dog}", map[string]string{"d": "Dog"}},
		{"an inline variant", "3.1.0", cat + ", " + inline, "{cat: Cat}", map[string]string{"cat": "Cat"}},
		{"the default named", "3.2.0", cat + ", " + dog, "{cat: Cat}, defaultMapping: Dog",
			map[string]string{"cat": "Cat", "Dog": "Dog"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			disc := "      discriminator: {propertyName: kind}\n"
			if c.mapping != "" {
				disc = "      discriminator: {propertyName: kind, mapping: " + c.mapping + "}\n"
			}
			union := "    U:\n      oneOf: [" + c.branches + "]\n" + disc
			const rest = "    Cat: {type: object, properties: {kind: {type: string}}}\n" +
				"    Dog: {type: object, properties: {kind: {type: string}}}\n"
			var docs []*ir.Document
			for _, src := range []string{union + rest, rest + union} {
				doc, diags := parseFull(t, openapitest.ComponentSpecVer(c.ver, src))
				openapitest.RequireNoErrorDiags(t, diags)
				assert.Empty(t, validated(t, doc), "the document must validate")
				docs = append(docs, doc)
			}
			assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types))
			u, ok := typeByName(docs[0], "U").(*ir.Union)
			require.True(t, ok)
			require.NotNil(t, u.Discriminator)
			assert.Empty(t, u.Provenance.Inferred, "the routes are the document's")
			if c.want == nil {
				assert.Nil(t, u.Discriminator.Mapping)
				return
			}
			want := make(map[string]ir.TypeID, len(c.want))
			for tag, target := range c.want {
				want[tag] = componentID(target)
				if target == "alias" {
					want[tag] = u.Variants[0].Type.Target
				}
			}
			assert.Empty(t, cmp.Diff(want, u.Discriminator.Mapping))
		})
	}
}

// TestDiscriminatorDefault_RoutesToTheVariantAliasingItsTarget pins that a
// defaultMapping is routed as a mapping's target is, so it names a variant
// where a keyword beside the branch's $ref gave the branch an alias.
func TestDiscriminatorDefault_RoutesToTheVariantAliasingItsTarget(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.2.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    Dog: {type: object, properties: {kind: {type: string}}}
    Pet:
      oneOf:
        - {$ref: '#/components/schemas/Cat', description: a cat}
        - {$ref: '#/components/schemas/Dog'}
      discriminator: {propertyName: kind, defaultMapping: '#/components/schemas/Cat'}
`)
	openapitest.RequireNoErrorDiags(t, diags)
	assert.Empty(t, validated(t, doc))
	pet, ok := typeByName(doc, "Pet").(*ir.Union)
	require.True(t, ok)
	assert.NotEqual(t, ir.TypeID("t/openapi/components/schemas/Cat"), pet.Variants[0].Type.Target)
	assert.Equal(t, pet.Variants[0].Type.Target, pet.Discriminator.Default)
}

// TestDiscriminatorMapping_ATypeSeveralVariantsAliasRoutesToTheFirst pins a
// value naming a type two branches alias, each with its own keyword beside its
// $ref (docs/compilers/openapi.md#discriminator): nothing in the value tells
// the variants apart, so it routes to the first in branch order, with an info
// at its entry naming both branches, for a oneOf and an anyOf, whichever
// branch comes first, past null branches written in any spelling, whichever
// key the mapping writes first and in either declaration order, and the
// document validates.
func TestDiscriminatorMapping_ATypeSeveralVariantsAliasRoutesToTheFirst(t *testing.T) {
	t.Parallel()
	const (
		green      = "{$ref: '#/components/schemas/Apple', description: green}"
		red        = "{$ref: '#/components/schemas/Apple', description: red}"
		banana     = "{$ref: '#/components/schemas/Banana'}"
		greenFirst = "{green_apple: '#/components/schemas/Apple', red_apple: '#/components/schemas/Apple', banana: Banana}"
		redFirst   = "{red_apple: '#/components/schemas/Apple', banana: Banana, green_apple: '#/components/schemas/Apple'}"
	)
	for _, c := range []struct {
		name, key, branches, mapping string
		first, second                int // the branches aliasing Apple
	}{
		{"oneOf", "oneOf", green + ", " + red + ", " + banana, greenFirst, 0, 1},
		{"anyOf", "anyOf", green + ", " + red + ", " + banana, greenFirst, 0, 1},
		{"red written first in the mapping", "oneOf", green + ", " + red + ", " + banana, redFirst, 0, 1},
		{"the red branch first", "oneOf", red + ", " + green + ", " + banana, greenFirst, 0, 1},
		{"another branch first", "oneOf", banana + ", " + green + ", " + red, greenFirst, 1, 2},
		{"null branches between", "oneOf", "{const: null}, " + green + ", {enum: [null]}, " + red + ", " + banana,
			greenFirst, 1, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fruit := "    Fruit:\n      " + c.key + ": [" + c.branches + "]\n" +
				"      discriminator: {propertyName: kind, mapping: " + c.mapping + "}\n"
			const rest = "    Apple: {type: object, properties: {kind: {type: string}}}\n" +
				"    Banana: {type: object, properties: {kind: {type: string}}}\n"
			branch := func(i int) string { return "/components/schemas/Fruit/" + c.key + "/" + strconv.Itoa(i) }
			info := func(tag string) string {
				return fmt.Sprintf("info %s /components/schemas/Fruit/discriminator/mapping/%s: discriminator mapping %q "+
					"names a schema 2 branches of this union reach through a $ref (%q, %q), and a tag routes to one "+
					"variant; routed to the first", diag.DegradedConstruct, tag, tag, branch(c.first), branch(c.second))
			}
			var docs []*ir.Document
			for _, src := range []string{fruit + rest, rest + fruit} {
				doc, diags := parseFull(t, openapitest.ComponentSpec(src))
				assert.Equal(t, []string{info("green_apple"), info("red_apple")}, reported(withCode(diags, diag.DegradedConstruct)))
				openapitest.RequireNoErrorDiags(t, diags)
				assert.Empty(t, validated(t, doc), "every tag routes to a variant")
				u, ok := typeByName(doc, "Fruit").(*ir.Union)
				require.True(t, ok)
				first := ir.TypeID("t/anon" + branch(c.first))
				assert.Equal(t, map[string]ir.TypeID{"green_apple": first, "red_apple": first, "banana": componentID("Banana")},
					u.Discriminator.Mapping)
				docs = append(docs, doc)
			}
			assert.Empty(t, cmp.Diff(docs[0].Types, docs[1].Types))
		})
	}
}

// TestDiscriminatorMapping_ATypeManyVariantsAliasNamesAFew pins the info a
// value routed to the first of more aliasing branches than a report lists
// gives: it names the first three and counts the rest, which it spells for no
// entry, so a union whose every branch aliases one type costs each entry no
// more.
func TestDiscriminatorMapping_ATypeManyVariantsAliasNamesAFew(t *testing.T) {
	t.Parallel()
	branches := make([]string, 0, 5)
	for i := range 5 {
		branches = append(branches, fmt.Sprintf("{$ref: '#/components/schemas/Apple', description: d%d}", i))
	}
	doc, diags := parseFull(t, openapitest.ComponentSpec("    Apple: {type: object, properties: {kind: {type: string}}}\n"+
		"    Fruit: {oneOf: ["+strings.Join(branches, ", ")+"], discriminator: {propertyName: kind, mapping: {a: Apple}}}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	const branch = "/components/schemas/Fruit/oneOf/"
	assert.Equal(t, []string{"info " + diag.DegradedConstruct + " /components/schemas/Fruit/discriminator/mapping/a: " +
		`discriminator mapping "a" names a schema 5 branches of this union reach through a $ref ("` + branch + `0", "` +
		branch + `1", "` + branch + `2" and 2 more), and a tag routes to one variant; routed to the first`},
		reported(withCode(diags, diag.DegradedConstruct)))
	assert.Equal(t, ir.TypeID("t/anon"+branch+"0"), discriminatorOf(t, doc, componentID("Fruit")).Mapping["a"])
}

// TestDiscriminatorMapping_ReadsThroughWhateverIsBesideAPositionsRef pins that a
// target naming a position that writes a $ref means what the $ref names,
// whatever is written beside it, in a carried position and at one holding an
// alias, for a union's variants and a model's subtypes alike: the position is
// no variant or subtype, so stopping at it reports a mapping the document
// meant (GitHub #764).
func TestDiscriminatorMapping_ReadsThroughWhateverIsBesideAPositionsRef(t *testing.T) {
	t.Parallel()
	for _, beside := range []string{"description: d", "k-beside: 1", "minLength: 1"} {
		t.Run(beside, func(t *testing.T) {
			t.Parallel()
			doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    Cat: {type: object, properties: {kind: {type: string}}}
    Dog: {type: object, properties: {kind: {type: string}}}
    Holder: {type: object, properties: {p: {$ref: '#/components/schemas/Cat', `+beside+`}}}
    Dogs: {type: array, items: {$ref: '#/components/schemas/Dog', `+beside+`}}
    Pet:
      oneOf: [{$ref: '#/components/schemas/Cat'}, {$ref: '#/components/schemas/Dog'}]
      discriminator:
        propertyName: kind
        mapping: {c: '#/components/schemas/Holder/properties/p', d: '#/components/schemas/Dogs/items'}
    Base:
      type: object
      properties: {kind: {type: string}}
      discriminator:
        propertyName: kind
        mapping: {s: '#/components/schemas/Subs/items'}
    Sub: {allOf: [{$ref: '#/components/schemas/Base'}]}
    Subs: {type: array, items: {$ref: '#/components/schemas/Sub', `+beside+`}}
`)
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Empty(t, validated(t, doc))
			pet, ok := typeByName(doc, "Pet").(*ir.Union)
			require.True(t, ok)
			assert.Empty(t, cmp.Diff(map[string]ir.TypeID{
				"c": "t/openapi/components/schemas/Cat", "d": "t/openapi/components/schemas/Dog",
			}, pet.Discriminator.Mapping))
			base, ok := typeByName(doc, "Base").(*ir.Model)
			require.True(t, ok)
			assert.Equal(t, ir.TypeID("t/openapi/components/schemas/Sub"), base.Discriminator.Mapping["s"])
			sub, ok := typeByName(doc, "Sub").(*ir.Model)
			require.True(t, ok)
			assert.Equal(t, "s", sub.DiscriminatorValue)
		})
	}
}

// TestDiscriminator_AnEmptyPropertyNameIsKeptVerbatim pins a discriminator
// naming the empty string, a legal member name the IR's PropertyName cannot
// hold, its empty value meaning none: on a model declaring no property of
// that name, one inheriting it, a union, plain or distributed, and a routed
// subtype's, the node holds no discriminator, the census keeps it verbatim and
// says why, once, and a union is untagged, nothing inferred though its
// variants pin a tag. A model
// declaring the property locates the tag by its PropID. Each holds at every
// minor, in either declaration order, the document valid and the IR whole.
func TestDiscriminator_AnEmptyPropertyNameIsKeptVerbatim(t *testing.T) {
	t.Parallel()
	blocks := []string{
		"    A: {type: object, required: [kind], properties: {'': {type: string}, kind: {type: string, enum: [a]}}}\n",
		"    B: {type: object, required: [kind], properties: {kind: {type: string, enum: [b]}}}\n",
		"    Plain: {type: object, discriminator: {propertyName: ''}}\n",
		"    Inherits: {allOf: [{$ref: '#/components/schemas/A'}], discriminator: {propertyName: ''}}\n",
		"    Mapped: {oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}], " +
			"discriminator: {propertyName: '', mapping: {a: A}}}\n",
		"    Union: {oneOf: [{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}], discriminator: {propertyName: ''}}\n",
		"    Beside: {type: object, properties: {kind: {type: string}}, oneOf: [{type: object, properties: {x: {type: string}}}, " +
			"{type: object, properties: {y: {type: string}}}], discriminator: {propertyName: ''}}\n",
		"    Own: {type: object, properties: {'': {type: string}}, discriminator: {propertyName: ''}}\n",
		"    Base: {type: object, properties: {kind: {type: string}}, " +
			"discriminator: {propertyName: kind, mapping: {r: '#/components/schemas/Routed'}}}\n",
		"    Routed: {allOf: [{$ref: '#/components/schemas/Base'}], oneOf: [{$ref: '#/components/schemas/A'}, " +
			"{$ref: '#/components/schemas/B'}], discriminator: {propertyName: ''}}\n",
	}
	for _, version := range []string{"3.0.3", "3.1.1", "3.2.0"} {
		for _, reversed := range []bool{false, true} {
			doc, diags := parseFull(t, rawSpec(version, blocks, "", reversed))
			openapitest.RequireNoErrorDiags(t, diags)
			assert.Empty(t, irverify.Verify(doc), "%s, reversed %t", version, reversed)
			assert.Empty(t, validated(t, doc))
			names := []string{"Plain", "Inherits", "Mapped", "Union", "Beside", "Routed"}
			kept := make([]string, 0, len(names))
			for _, name := range names {
				td := typeByName(doc, name)
				assert.Nil(t, discriminatorIn(td), "%s holds none", name)
				assert.Equal(t, ir.ReasonDegradedLowering, td.Common().Unmodeled["openapi:discriminator"].Reason, name)
				assert.Empty(t, td.Common().Provenance.Inferred, "%s: nothing is inferred", name)
				kept = append(kept, "info "+diag.DegradedConstruct+" /components/schemas/"+name+": this position "+
					"lowered to a "+string(td.Kind())+" routed by no tag (the IR's PropertyName cannot hold the empty "+
					"name its discriminator gives the tag property), which has no home for discriminator declared "+
					"beside it; kept verbatim under Unmodeled")
			}
			slices.Sort(kept)
			assert.Equal(t, kept, reported(withCode(diags, diag.DegradedConstruct)), "%s, reversed %t", version, reversed)
			for _, name := range []string{"Mapped", "Union", "Beside"} {
				u, ok := typeByName(doc, name).(*ir.Union)
				require.True(t, ok)
				assert.False(t, u.WireTagged, "%s is untagged", name)
			}
			minted, ok := doc.Types["t/composed/components/schemas/Routed/oneOf"].(*ir.Union)
			require.True(t, ok, "Routed composes its union as a mixin")
			assert.Nil(t, minted.Discriminator, "the union Routed mints holds none")
			own, ok := typeByName(doc, "Own").(*ir.Model)
			require.True(t, ok)
			require.NotNil(t, own.Discriminator)
			assert.Equal(t, own.Properties[0].ID, own.Discriminator.Property, "the tag is the property Own declares")
		}
	}
}

// TestDiscriminator_AnEmptyPropertyNameBesideAnUntaggedUnionIsKeptForItsNode
// pins the report of a discriminator naming the empty tag beside a Union no
// discriminator routes, an enum's or a type array's, listing object or not: it
// is kept and reported as one naming any other tag is, by what the node is, as
// the empty name is not why the node holds none.
func TestDiscriminator_AnEmptyPropertyNameBesideAnUntaggedUnionIsKeptForItsNode(t *testing.T) {
	t.Parallel()
	names := []string{"Mixed", "Multi", "Obj"}
	for _, tag := range []string{"''", "kind"} {
		doc, diags := parseFull(t, openapitest.ComponentSpec(
			"    Mixed: {enum: [1, a], discriminator: {propertyName: "+tag+"}}\n"+
				"    Multi: {type: [string, integer], discriminator: {propertyName: "+tag+"}}\n"+
				"    Obj: {type: [object, string], properties: {kind: {type: string}}, discriminator: {propertyName: "+tag+"}}\n"))
		openapitest.RequireNoErrorDiags(t, diags)
		got := reported(withCode(diags, diag.DegradedConstruct))
		for _, name := range names {
			assert.Contains(t, got, "info "+diag.DegradedConstruct+" /components/schemas/"+name+`: this position lowered `+
				`to a node of kind "union", which has no home for discriminator declared beside it; kept verbatim under `+
				"Unmodeled", "%s, tag %s", name, tag)
			assert.Equal(t, ir.ReasonDegradedLowering, typeByName(doc, name).Common().Unmodeled["openapi:discriminator"].Reason)
		}
	}
}

// discriminatorIn returns the discriminator the model or union td holds, or
// nil for none or another kind.
func discriminatorIn(td ir.TypeDef) *ir.Discriminator {
	switch n := td.(type) {
	case *ir.Model:
		return n.Discriminator
	case *ir.Union:
		return n.Discriminator
	default:
		return nil
	}
}
