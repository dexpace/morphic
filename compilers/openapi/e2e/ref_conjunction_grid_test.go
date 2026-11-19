// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// conjFamily is a family of keywords and what to write of it beside a use of
// each target, none where the family says nothing of that target. entry, where
// set, is written as an entry of an allOf beside the $ref in one spelling and
// beside the use in the other, since an allOf cannot be written twice.
type conjFamily struct {
	name         string
	stringSchema string
	obj          string
	arr          string
	entries      map[string]string
	only31       bool
}

// conjFamilies are the keyword families a use may write beside its $ref.
var conjFamilies = []conjFamily{
	{name: "bound", stringSchema: "minLength: 1", obj: "minProperties: 1", arr: "maxItems: 3"},
	{name: "type", stringSchema: "type: string", obj: "type: object", arr: "type: array"},
	{name: "enum", stringSchema: "enum: [a, b]"},
	{name: "const", stringSchema: "const: a", only31: true},
	{name: "nullable", stringSchema: "nullable: true", obj: "nullable: true"},
	{name: "docs", stringSchema: "description: d, title: Ti", obj: "description: d, title: Ti", arr: "description: d, title: Ti"},
	{name: "deprecated", stringSchema: "deprecated: true", obj: "deprecated: true", arr: "deprecated: true"},
	{name: "example", stringSchema: "example: x", obj: "example: {a: x}", arr: "example: [x]"},
	{name: "default", stringSchema: "default: x", obj: "default: {a: x}", arr: "default: [x]"},
	{name: "readOnly", stringSchema: "readOnly: true", obj: "readOnly: true", arr: "readOnly: true"},
	{name: "writeOnly", stringSchema: "writeOnly: true", obj: "writeOnly: true", arr: "writeOnly: true"},
	{name: "properties", stringSchema: "properties: {b: {type: string}}", obj: "properties: {b: {type: string}}"},
	{name: "required", stringSchema: "required: [a]", obj: "required: [a]"},
	{name: "additionalProperties", stringSchema: "additionalProperties: {type: string}", obj: "additionalProperties: {type: string}"},
	{name: "items", stringSchema: "items: {type: string}", arr: "items: {type: string}"},
	{
		name: "oneOf", stringSchema: "oneOf: [{type: string}, {type: boolean}]",
		obj: "oneOf: [{type: object, properties: {b: {type: string}}}, {type: object, properties: {c: {type: string}}}]",
		arr: "oneOf: [{type: array, maxItems: 2}, {type: boolean}]",
	},
	{
		name: "anyOf", stringSchema: "anyOf: [{type: string, maxLength: 5}, {type: boolean}]",
		obj: "anyOf: [{required: [a]}, {required: [b]}]", arr: "anyOf: [{type: array, maxItems: 2}, {type: boolean}]",
	},
	{name: "not", stringSchema: "not: {enum: [n]}", obj: "not: {enum: [n]}", arr: "not: {enum: [n]}"},
	{name: "format", stringSchema: "format: password", obj: "format: password", arr: "format: password"},
	{name: "xml", stringSchema: "xml: {name: x}", obj: "xml: {name: x}", arr: "xml: {name: x}"},
	{name: "externalDocs", stringSchema: "externalDocs: {url: 'https://example.com'}", obj: "externalDocs: {url: 'https://example.com'}"},
	{
		name: "promotedExtension", stringSchema: `deprecated: true, x-deprecated-since: "2"`,
		obj: `deprecated: true, x-deprecated-since: "2"`, arr: `deprecated: true, x-deprecated-since: "2"`,
	},
	{name: "extension", stringSchema: "x-a: 1", obj: "x-a: 1", arr: "x-a: 1"},
	{name: "unknownKey", stringSchema: "unknownKeyword: 1", obj: "unknownKeyword: 1", arr: "unknownKeyword: 1"},
	{name: "allOfBeside", entries: map[string]string{"Str": "{maxLength: 5}", "Obj": "{properties: {b: {type: string}}}", "Arr": "{maxItems: 3}"}},
}

// keywordsOf is what the family writes of the target, empty where it says
// nothing.
func (f conjFamily) keywordsOf(target string) string {
	switch target {
	case "Str":
		return f.stringSchema
	case "Obj":
		return f.obj
	default:
		return f.arr
	}
}

// spellings writes a use of the target saying keywords, beside its $ref and
// as an allOf of it.
func (f conjFamily) spellings(target string) (beside, wrapped string) {
	if entry, ok := f.entries[target]; ok {
		return "{" + refTo(target) + ", allOf: [" + entry + "]}", "{allOf: [{" + refTo(target) + "}, " + entry + "]}"
	}
	k := f.keywordsOf(target)
	return besideSpelling(target, k), wrapSpelling(target, k)
}

// TestRefConjunction_AUseWritingKeywordsBesideItsRefCompilesAsTheAllOfSpelling
// holds that `{$ref: T, K}` and `{allOf: [{$ref: T}], K}` compile to one
// document, for each family of keyword K, over a string, an object and an array,
// at a component, a property, a parameter, a header, an items, a map value, a
// union branch and a request body, at 3.0.3 and 3.1.0: the same nodes, with
// the same kinds, bounds, annotations, Unmodeled keys and reasons and
// inferred marks, and the same findings, with no fold and the anonymous IDs
// alone relocated.
func TestRefConjunction_AUseWritingKeywordsBesideItsRefCompilesAsTheAllOfSpelling(t *testing.T) {
	t.Parallel()
	for _, family := range conjFamilies {
		t.Run(family.name, func(t *testing.T) {
			t.Parallel()
			for _, version := range []string{"3.0.3", "3.1.0"} {
				if family.only31 && version == "3.0.3" {
					continue
				}
				for _, target := range []string{"Str", "Obj", "Arr"} {
					if family.keywordsOf(target) == "" && family.entries[target] == "" {
						continue
					}
					beside, wrapped := family.spellings(target)
					for _, site := range conjSites {
						t.Run(fmt.Sprintf("%s/%s/%s", version, target, site.name), func(t *testing.T) {
							t.Parallel()
							first, _ := compileUnrefused(t, site.write(version, beside, conjTargets[target]))
							second, _ := compileUnrefused(t, site.write(version, wrapped, conjTargets[target]))
							requireSpellingsEqual(t, first, second, beside)
						})
					}
				}
			}
		})
	}
}

// nodeAt is the JSON of the type with the ID given, nil where the document
// holds none.
func nodeAt(tree any, id string) map[string]any {
	node, _ := valueAtPath(tree, "types", id).(map[string]any)
	return node
}

// carriersNamed lists the properties, parameters and headers of the document
// named name: the objects of its JSON that carry a name and a type and are no
// type of their own.
func carriersNamed(tree any, name string) []map[string]any {
	var found []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if valueAtPath(x, "name", "source") == name && x["type"] != nil && x["kind"] == nil {
				found = append(found, x)
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(tree)
	return found
}

// carrierName is the name a carrying site gives its carrier, empty for a site
// that has none.
func carrierName(site conjSite) string {
	switch site.name {
	case "property", "parameter":
		return "p"
	case "header":
		return "X-H"
	default:
		return ""
	}
}

// requireCarrier returns the one carrier the site writes in the document.
func requireCarrier(t *testing.T, tree any, site conjSite) map[string]any {
	t.Helper()
	found := carriersNamed(tree, carrierName(site))
	require.Len(t, found, 1, "the one %s the document writes", site.name)
	return found[0]
}

// holdsPromotion reports whether the JSON of a node or carrier holds the
// deprecation the extension fills, its mark and the entry keeping the key.
func holdsPromotion(node any) (since, marked, kept bool) {
	since = valueAtPath(node, "deprecation", "since") == "2"
	marked = strings.Contains(fmt.Sprint(valueAtPath(node, "provenance", "inferred")), "extension-promotion")
	for key := range keptReasons(valueAtPath(node, "unmodeled")) {
		// A parameter or a header keeps what its schema writes under openapi:schema/.
		kept = kept || key == "openapi:x-deprecated-since" || key == "openapi:schema/x-deprecated-since"
	}
	return since, marked, kept
}

// TestRefConjunction_APromotedFactAndItsMarkSitOnOneNodeInEverySpelling holds,
// for a deprecation with a `since` the extension fills, written beside a $ref
// and in the allOf spelling, that the Deprecation, the mark naming the
// promotion and the Unmodeled entry keeping the extension sit on one node: the
// anonymous model over an object at a property, a parameter or a header, which
// leaves its carrier none of the three, the carrier itself over any other
// target, and the node hoisted at an items, a union branch or a body.
func TestRefConjunction_APromotedFactAndItsMarkSitOnOneNodeInEverySpelling(t *testing.T) {
	t.Parallel()
	keywords := `deprecated: true, x-deprecated-since: "2"`
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, target := range []string{"Obj", "Str"} {
			for _, spelling := range []struct {
				name  string
				write func(string, string) string
			}{{"beside", besideSpelling}, {"wrap", wrapSpelling}} {
				for _, site := range conjSites {
					schema := spelling.write(target, keywords)
					t.Run(fmt.Sprintf("%s/%s/%s/%s", version, target, site.name, spelling.name), func(t *testing.T) {
						t.Parallel()
						doc, _ := compileUnrefused(t, site.write(version, schema, conjTargets[target]))
						tree := documentTree(t, doc)
						if carrierName(site) == "" {
							id := "type/anonymous" + site.pointer
							if site.name == "component" {
								id = string(componentID("Use"))
							}
							node := nodeAt(tree, id)
							require.NotNil(t, node, "the node hoisted at %s", site.pointer)
							since, marked, kept := holdsPromotion(node)
							assert.True(t, since && marked && kept, "the hoisted node holds the fact, its mark and its entry")
							return
						}
						carrier := requireCarrier(t, tree, site)
						since, marked, kept := holdsPromotion(carrier)
						if target == "Str" {
							assert.True(t, since && marked && kept, "the carrier over a string holds the fact, its mark and its entry")
							return
						}
						assert.False(t, since || marked || kept, "the carrier over an object holds none of the three")
						decorator := nodeAt(tree, fmt.Sprint(valueAtPath(carrier, "type", "target")))
						require.NotNil(t, decorator)
						assert.Equal(t, "model", decorator["kind"])
						assert.Equal(t, true, decorator["anonymous"])
						since, marked, kept = holdsPromotion(decorator)
						assert.True(t, since && marked && kept, "the model over the object holds the fact, its mark and its entry")
					})
				}
			}
		}
	}
}

// TestRefConjunction_AnAllOfOfOneRefHoistsNoNode holds that an allOf whose
// only entry is a $ref to a string or an object, writing nothing beside it,
// is a use of the target: no anonymous node stands at its position or names
// the target as its base, and the position names the target, but at a
// component, which owns a node: a model over an object, a scalar over any
// other target.
func TestRefConjunction_AnAllOfOfOneRefHoistsNoNode(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, target := range []string{"Obj", "Str"} {
			for _, site := range conjSites {
				t.Run(fmt.Sprintf("%s/%s/%s", version, target, site.name), func(t *testing.T) {
					t.Parallel()
					requireNothingHoisted(t, version, target, site, "{allOf: [{"+refTo(target)+"}]}")
				})
			}
		}
	}
}

// variantHints lists the name hints of the variants of the union component.
func variantHints(t *testing.T, doc *ir.Document, name string) []string {
	t.Helper()
	union, ok := kindNode(t, doc, componentID(name)).(*ir.Union)
	require.True(t, ok, "%s is a union", name)
	hints := make([]string, 0, len(union.Variants))
	for _, variant := range union.Variants {
		hints = append(hints, variant.Name.Hint)
	}
	return hints
}

// firstVariantNode is the node the first variant of the union component names.
func firstVariantNode(t *testing.T, doc *ir.Document, name string) ir.TypeDef {
	t.Helper()
	union, ok := kindNode(t, doc, componentID(name)).(*ir.Union)
	require.True(t, ok, "%s is a union", name)
	require.NotEmpty(t, union.Variants)
	return kindNode(t, doc, union.Variants[0].Type.Target)
}

// TestRefConjunction_AHoistedNodeAndItsVariantAreNamedAlikeInEverySpelling holds
// that a union branch saying more of its target than a bare $ref, a bound or a
// description, hoists a node hinted by its position and a variant named as the
// bare $ref's variant is, the target's name, whether the branch writes the
// bound beside the $ref, beside an allOf of it or on an entry of that allOf, over a
// string and over an object.
func TestRefConjunction_AHoistedNodeAndItsVariantAreNamedAlikeInEverySpelling(t *testing.T) {
	t.Parallel()
	ref := func(n string) string { return "{" + refTo(n) + "}" }
	cases := []struct{ name, branch, variant string }{
		{"BesideB", "{" + refTo("Str") + ", minLength: 1}", "str"},
		{"WrapB", "{allOf: [" + ref("Str") + "], minLength: 1}", "str"},
		{"EntryB", "{allOf: [" + ref("Str") + ", {minLength: 1}]}", "str"},
		{"BesideO", "{" + refTo("Obj") + ", minProperties: 1}", "obj"},
		{"WrapO", "{allOf: [" + ref("Obj") + "], minProperties: 1}", "obj"},
		{"EntryO", "{allOf: [" + ref("Obj") + ", {minProperties: 1}]}", "obj"},
		{"BesideD", "{" + refTo("Str") + ", description: d}", "str"},
		{"WrapD", "{allOf: [" + ref("Str") + "], description: d}", "str"},
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			schemas := make([]string, 0, 2+len(cases))
			schemas = append(schemas, conjTargets["Str"], conjTargets["Obj"])
			for _, tc := range cases {
				schemas = append(schemas, tc.name+": {oneOf: ["+tc.branch+", {type: boolean}]}")
			}
			doc, _ := compileUnrefused(t, admissionSpec(version, "", schemas...))
			for _, tc := range cases {
				assert.Equal(t, []string{tc.variant, "variant_1"}, variantHints(t, doc, tc.name), "the variants of %s", tc.name)
				assert.Equal(t, "variant_0", firstVariantNode(t, doc, tc.name).Common().Name.Hint, "the node hoisted by %s", tc.name)
			}
		})
	}
}

// TestRefConjunction_ATitleNamesTheVariantHoldingItWhereverItIsWritten holds
// that a title on an inline entry names the union branch and the node
// hoisted there, over a string and an object, when the entry is a conjunct
// of the branch's allOf, when it is nested in one more allOf, and when it is
// written beside the entry's $ref, as it does beside the allOf or beside the
// branch's own $ref.
func TestRefConjunction_ATitleNamesTheVariantHoldingItWhereverItIsWritten(t *testing.T) {
	t.Parallel()
	branches := func(target string) map[string]string {
		ref := "{" + refTo(target) + "}"
		return map[string]string{
			"Flat":        "{allOf: [" + ref + ", {title: Mark}]}",
			"Nested":      "{allOf: [{allOf: [" + ref + ", {title: Mark}]}]}",
			"Wrap":        "{allOf: [" + ref + "], title: Mark}",
			"Beside":      "{" + refTo(target) + ", title: Mark}",
			"EntryBeside": "{allOf: [{" + refTo(target) + ", title: Mark}]}",
		}
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, target := range []string{"Str", "Obj"} {
			t.Run(version+"/"+target, func(t *testing.T) {
				t.Parallel()
				schemas := []string{conjTargets["Str"], conjTargets["Obj"]}
				for name, branch := range branches(target) {
					schemas = append(schemas, name+target+": {oneOf: ["+branch+", {type: boolean}]}")
				}
				doc, _ := compileUnrefused(t, admissionSpec(version, "", schemas...))
				for name := range branches(target) {
					assert.Equal(t, []string{"mark", "variant_1"}, variantHints(t, doc, name+target), "the variants of %s", name)
					assert.Equal(t, "mark", firstVariantNode(t, doc, name+target).Common().Name.Hint, "the node of %s", name)
				}
			})
		}
	}
}

// requireOnlyFindings holds the diagnostics of a compile to the codes given:
// nothing else is reported, whatever its severity.
func requireOnlyFindings(t *testing.T, diags []ir.Diagnostic, what string, allowed ...string) {
	t.Helper()
	for _, diagnostic := range diags {
		assert.Contains(t, allowed, diagnostic.Code, "%s: %s %s at %s", what, diagnostic.Severity, diagnostic.Code, diagnostic.Provenance.Pointer)
	}
}

// spell writes a use of a target saying keywords in one of the spellings.
type spell func(target, keywords string) string

// compileSpelled compiles the schemas build gives with each spelling and
// returns the document of each, the beside spelling's findings with them.
func compileSpelled(
	t *testing.T, version string, build func(spellValue spell) []string,
) (beside, wrapped *ir.Document, findings []ir.Diagnostic) {
	t.Helper()
	beside, findings = compileUnrefused(t, admissionSpec(version, "", build(besideSpelling)...))
	wrapped, _ = compileUnrefused(t, admissionSpec(version, "", build(wrapSpelling)...))
	return beside, wrapped, findings
}

// TestRefConjunction_AStructuralKeywordBesideARefIsComposedOverItsTarget holds
// that required, properties and an allOf-less use of an object writing them
// beside its $ref, at a component and at a property, is a model whose base is
// the target, declaring what it writes, with nothing kept and nothing
// reported, and the same as the allOf spelling of the use.
func TestRefConjunction_AStructuralKeywordBesideARefIsComposedOverItsTarget(t *testing.T) {
	t.Parallel()
	user := "User: {type: object, properties: {id: {type: string}}}"
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			beside, wrapped, diags := compileSpelled(t, version, func(spellValue spell) []string {
				return []string{
					user,
					"Created: " + spellValue("User", "required: [id]"),
					"Extended: " + spellValue("User", "properties: {age: {type: integer}}"),
					"Holder: {type: object, properties: {u: " + spellValue("User", "required: [id]") + "}}",
				}
			})
			requireSpellingsEqual(t, beside, wrapped, "required and properties beside a $ref")
			requireOnlyFindings(t, diags, "a keyword a model holds beside a $ref", "openapi/later-version-construct")

			created := requireModelOf(t, beside, "Created")
			require.NotNil(t, created.Base)
			assert.Equal(t, componentID("User"), created.Base.Target)
			assert.Equal(t, []string{"id"}, created.Requires)
			assert.Empty(t, keptOn(created))

			extended := requireModelOf(t, beside, "Extended")
			require.NotNil(t, extended.Base)
			assert.Equal(t, componentID("User"), extended.Base.Target)
			require.Len(t, extended.Properties, 1)
			assert.Equal(t, "age", extended.Properties[0].Name.Source)

			property, ok := propertyByWire(requireModelOf(t, beside, "Holder"), "u")
			require.True(t, ok)
			held := kindNode(t, beside, property.Type.Target)
			model, ok := held.(*ir.Model)
			require.True(t, ok, "the property names a model over the object, as the allOf spelling does")
			assert.Equal(t, []string{"id"}, model.Requires)
		})
	}
}

// TestRefConjunction_AValueSetBesideARefNarrowsItsTargetInEverySpelling holds
// that an enum or a const written beside a $ref and beside an allOf of it
// lowers alike (a value set on an entry of that allOf is a case of the node
// standing at the position, which these do not hold): the property names the enum or the
// literal, which keeps the $ref, with one report, and an enum or type the
// target already says is a use of the target keeping nothing.
func TestRefConjunction_AValueSetBesideARefNarrowsItsTargetInEverySpelling(t *testing.T) {
	t.Parallel()
	levels := []string{"Level: {type: string, enum: [low, mid, high]}", "Port: {type: integer}"}
	uses := []struct{ name, target, keywords, kind string }{
		{"enum", "Level", "enum: [low]", "enum"},
		{"const", "Level", "const: low", "literal"},
		{"out", "Level", "enum: [other]", "enum"},
		{"portEnum", "Port", "enum: [1, 2]", "enum"},
		{"type", "Port", "type: integer", ""},
	}
	for _, version := range []string{"3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			build := func(spellValue spell) []string {
				properties := make([]string, 0, len(uses))
				for _, u := range uses {
					properties = append(properties, u.name+": "+spellValue(u.target, u.keywords))
				}
				return append(slices.Clone(levels), "Holder: {type: object, properties: {"+strings.Join(properties, ", ")+"}}")
			}
			beside, wrapped, diags := compileSpelled(t, version, build)
			requireSpellingsEqual(t, beside, wrapped, "a value set beside a $ref")

			for _, u := range uses {
				node := kindNode(t, beside, holderWirePropertyType(t, beside, u.name))
				pointer := "/components/schemas/Holder/properties/" + u.name
				if u.kind == "" {
					assert.Equal(t, componentID(u.target), holderWirePropertyType(t, beside, u.name), u.name)
					assert.Empty(t, keptOn(node), u.name)
					assert.Zero(t, degradedInfosUnder(diags, pointer), u.name)
					continue
				}
				assert.Equal(t, ir.TypeKind(u.kind), node.Kind(), "what %s names", u.name)
				kept := keptOn(node)
				require.Len(t, kept, 1, u.name)
				for key, reason := range kept {
					assert.Regexp(t, `^openapi:(allOf/\d+|\$ref)$`, key, u.name)
					assert.Equal(t, ir.ReasonDegradedLowering, reason, u.name)
				}
				assert.Equal(t, 1, degradedInfosUnder(diags, pointer), "the one report of %s", u.name)
			}
		})
	}
}

// TestRefConjunction_ARedactionBesideARefInItemsIsAModelOverItsTarget holds
// that an items writing a password format, or a description, beside a $ref to
// an object, or beside an allOf of it, is a model over the object, redacting in the
// first case, and the two spellings compile alike.
func TestRefConjunction_ARedactionBesideARefInItemsIsAModelOverItsTarget(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			beside, wrapped, _ := compileSpelled(t, version, func(spellValue spell) []string {
				return []string{
					conjTargets["Obj"],
					"Holder: {type: object, properties: {p: {type: array, items: " + spellValue("Obj", "format: password") + "}, " +
						"d: {type: array, items: " + spellValue("Obj", "description: d") + "}}}",
				}
			})
			requireSpellingsEqual(t, beside, wrapped, "a redaction beside a $ref")
			for _, property := range []string{"p", "d"} {
				list, ok := kindNode(t, beside, holderWirePropertyType(t, beside, property)).(*ir.List)
				require.True(t, ok, property)
				element, ok := kindNode(t, beside, list.ElementType.Target).(*ir.Model)
				require.True(t, ok, "the element of %s is a model over the object", property)
				require.NotNil(t, element.Base)
				assert.Equal(t, componentID("Obj"), element.Base.Target)
				assert.Equal(t, property == "p", element.Sensitive, "what %s redacts", property)
			}
		})
	}
}

// TestRefConjunction_AKeywordOnlyAModelHoldsBesideARefAtAParameterIsKeptOnTheModel
// holds that a validation-only keyword, an xml name or an extension written
// beside a $ref to an object at a parameter is kept on the anonymous model over
// the object, where the allOf spelling keeps it, and not on the parameter,
// which names the model: one shape for the two spellings.
func TestRefConjunction_AKeywordOnlyAModelHoldsBesideARefAtAParameterIsKeptOnTheModel(t *testing.T) {
	t.Parallel()
	site := conjSites[2]
	require.Equal(t, "parameter", site.name)
	cases := []struct{ keywords, key string }{
		{"not: {enum: [n]}", "openapi:not"},
		{"x-a: 1", "openapi:x-a"},
		{"xml: {name: x}", ""},
	}
	for _, version := range []string{"3.0.3", "3.1.0"} {
		for _, tc := range cases {
			t.Run(version+"/"+strings.Split(tc.keywords, ":")[0], func(t *testing.T) {
				t.Parallel()
				beside, _ := compileUnrefused(t, site.write(version, besideSpelling("Obj", tc.keywords), conjTargets["Obj"]))
				wrapped, _ := compileUnrefused(t, site.write(version, wrapSpelling("Obj", tc.keywords), conjTargets["Obj"]))
				requireSpellingsEqual(t, beside, wrapped, tc.keywords)

				tree := documentTree(t, beside)
				carrier := requireCarrier(t, tree, site)
				assert.Empty(t, keptReasons(valueAtPath(carrier, "unmodeled")), "the parameter keeps nothing the model holds")
				model := nodeAt(tree, fmt.Sprint(valueAtPath(carrier, "type", "target")))
				require.NotNil(t, model)
				assert.Equal(t, "model", model["kind"])
				assert.Equal(t, true, model["anonymous"])
				if tc.key == "" {
					assert.Equal(t, "x", valueAtPath(model, "xml", "name"))
					return
				}
				assert.Contains(t, keptReasons(valueAtPath(model, "unmodeled")), tc.key)
			})
		}
	}
}

// TestRefConjunction_AUnionBesideARefLowersAsBesideAnAllOfOfIt holds that a
// oneOf or an anyOf written beside a $ref is read as the allOf spelling reads
// it: consumed where the principal already says it (a string and a union of a
// string and a boolean), kept as validation logic where its branches declare
// no shape, and distributed over an object where they declare one.
func TestRefConjunction_AUnionBesideARefLowersAsBesideAnAllOfOfIt(t *testing.T) {
	t.Parallel()
	objBranches := "oneOf: [{type: object, properties: {b: {type: string}}}, {type: object, properties: {c: {type: string}}}]"
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			beside, wrapped, diags := compileSpelled(t, version, func(spellValue spell) []string {
				return []string{
					conjTargets["Str"], conjTargets["Obj"],
					"UnionStr: " + spellValue("Str", "oneOf: [{type: string}, {type: boolean}]"),
					"UnionReq: " + spellValue("Obj", "anyOf: [{required: [a]}, {required: [b]}]"),
					"UnionObj: " + spellValue("Obj", objBranches),
				}
			})
			requireSpellingsEqual(t, beside, wrapped, "a union beside a $ref")
			requireOnlyFindings(t, filterCodes(diags, "openapi/validation-only-keyword", "openapi/composition-lowering"),
				"a union beside a $ref", "openapi/later-version-construct")

			stringType := requireScalarOver(t, beside, "UnionStr", componentID("Str"))
			assert.Empty(t, keptOn(stringType), "a union the principal says is consumed")
			assert.Zero(t, degradedInfosAt(diags, "UnionStr"))

			req := kindNode(t, beside, componentID("UnionReq"))
			assert.Equal(t, ir.ReasonValidationOnly, keptOn(req)["openapi:anyOf"], "a union declaring no shape is validation logic")

			union, ok := kindNode(t, beside, componentID("UnionObj")).(*ir.Union)
			require.True(t, ok, "a union declaring shapes distributes over the object")
			assert.Len(t, union.Variants, 2)
		})
	}
}

// filterCodes returns the diagnostics whose code is none of those given.
func filterCodes(diags []ir.Diagnostic, drop ...string) []ir.Diagnostic {
	var kept []ir.Diagnostic
	for _, diagnostic := range diags {
		if !slices.Contains(drop, diagnostic.Code) {
			kept = append(kept, diagnostic)
		}
	}
	return kept
}

// TestRefConjunction_TwoUnionsOnOneSchemaLowerAsOnTwoEntries holds that a oneOf
// and an anyOf written on one schema beside a body lower as the same two on
// two entries beside the same body do: a union of the models, the oneOf
// distributed and the anyOf kept on each variant.
func TestRefConjunction_TwoUnionsOnOneSchemaLowerAsOnTwoEntries(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, _ := compileUnrefused(t, admissionSpec(version, "", "Use: "+twoUnionShapes["Flat"]))
			requireOneOfDistributed(t, doc)
		})
	}
}

// TestRefConjunction_ADynamicRefReadAsARefIsThatConjunctUnderEitherKeyword holds
// that a $dynamicRef naming no anchor, beside an allOf of a bound, lowers as a
// $ref does at every position: the use of the target holding the bound on the
// carrier or on the node hoisted there, nothing kept and nothing reported.
func TestRefConjunction_ADynamicRefReadAsARefIsThatConjunctUnderEitherKeyword(t *testing.T) {
	t.Parallel()
	const version = "3.1.0"
	target := "T: {type: string}"
	for _, site := range conjSites {
		t.Run(site.name, func(t *testing.T) {
			t.Parallel()
			dynamic, dynDiags := compileUnrefused(t, site.write(version,
				"{$dynamicRef: '#/components/schemas/T', allOf: [{minLength: 1}]}", target))
			plain, _ := compileUnrefused(t, site.write(version, "{"+refTo("T")+", allOf: [{minLength: 1}]}", target))
			requireSpellingsEqual(t, dynamic, plain, "$dynamicRef against $ref")
			requireOnlyFindings(t, dynDiags, "a $dynamicRef read as a $ref")

			tree := documentTree(t, dynamic)
			node := nodeAt(tree, "type/anonymous"+site.pointer)
			if site.name == "component" {
				node = nodeAt(tree, string(componentID("Use")))
			}
			if carrierName(site) != "" {
				node = requireCarrier(t, tree, site)
				assert.Equal(t, string(componentID("T")), valueAtPath(node, "type", "target"))
			} else {
				require.NotNil(t, node)
				assert.Equal(t, "scalar", node["kind"])
				assert.Equal(t, string(componentID("T")), valueAtPath(node, "base", "target"))
			}
			assert.EqualValues(t, 1, valueAtPath(node, "constraints", "minLength"))
			assert.Empty(t, keptReasons(valueAtPath(node, "unmodeled")))
		})
	}
}

// TestRefConjunction_ADynamicRefBesideARefIsKeptOnTheUseInEverySpelling holds
// that a $dynamicRef naming an anchor, written beside a $ref, beside an allOf
// of it, is kept once under its own key on the use of the target with one
// report, at an items, a component and a property, and that no model is
// made over the string target.
func TestRefConjunction_ADynamicRefBesideARefIsKeptOnTheUseInEverySpelling(t *testing.T) {
	t.Parallel()
	const version = "3.1.0"
	others := []string{
		"T: {type: string}",
		"Tree: {$dynamicAnchor: m, type: object, properties: {kids: {type: array, items: {$dynamicRef: '#m'}}}}",
	}
	for _, name := range []string{"component", "property", "items"} {
		site := conjSites[slices.IndexFunc(conjSites, func(conjSiteValue conjSite) bool { return conjSiteValue.name == name })]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			beside, diags := compileUnrefused(t, site.write(version, "{"+refTo("T")+", $dynamicRef: '#m'}", others...))
			wrapped, _ := compileUnrefused(t, site.write(version, "{allOf: [{"+refTo("T")+"}], $dynamicRef: '#m'}", others...))
			requireSpellingsEqual(t, beside, wrapped, "a $dynamicRef beside a $ref")

			holders := 0
			for _, td := range beside.Types {
				if _, kept := keptOn(td)["openapi:$dynamicRef"]; kept {
					holders++
				}
				if model, ok := td.(*ir.Model); ok && model.Base != nil {
					assert.NotEqual(t, componentID("T"), model.Base.Target, "no model over a string")
				}
			}
			for _, c := range carriersNamed(documentTree(t, beside), carrierName(site)) {
				if _, kept := keptReasons(valueAtPath(c, "unmodeled"))["openapi:$dynamicRef"]; kept {
					holders++
				}
			}
			assert.Equal(t, 1, holders, "the keyword is kept once")
			schema := strings.Join(strings.Split(site.pointer, "/")[:4], "/")
			assert.Equal(t, 1, degradedInfosUnder(diags, schema), "one report")
		})
	}
}

// TestRefConjunction_TwoBasesInADiscriminatorHierarchyElectTheLeastReference
// holds that of two $refs to schemas each declaring a discriminator, neither
// of which routes to the composing schema, the base is the one whose reference
// text is the least, whichever is written first, and the other is a mixin.
func TestRefConjunction_TwoBasesInADiscriminatorHierarchyElectTheLeastReference(t *testing.T) {
	t.Parallel()
	base := "{type: object, required: [kind], properties: {kind: {type: string}}, discriminator: {propertyName: kind}}"
	body := "{type: object, properties: {c: {type: string}}}"
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			doc, _ := compileUnrefused(t, admissionSpec(version, "",
				"Zoo: "+base, "Aaa: "+base,
				"Child: {allOf: [{"+refTo("Zoo")+"}, {"+refTo("Aaa")+"}, "+body+"]}",
				"ChildRev: {allOf: [{"+refTo("Aaa")+"}, {"+refTo("Zoo")+"}, "+body+"]}"))
			for _, name := range []string{"Child", "ChildRev"} {
				model := requireModelOf(t, doc, name)
				require.NotNil(t, model.Base, name)
				assert.Equal(t, componentID("Aaa"), model.Base.Target, "the base of %s", name)
				assert.Equal(t, []ir.TypeID{componentID("Zoo")}, mixinTargets(model), "the mixins of %s", name)
			}
		})
	}
}

// mixinTargets lists the types a model names as its mixins, in order.
func mixinTargets(model *ir.Model) []ir.TypeID {
	targets := make([]ir.TypeID, 0, len(model.Mixins))
	for _, typeRef := range model.Mixins {
		targets = append(targets, typeRef.Target)
	}
	return targets
}

// TestRefConjunction_AComponentOverAnObjectIsTheModelThatOwnsIt holds that a
// component that is a $ref to an object, writing nothing beside it, is a
// model whose base is the object, as the component of the same use written
// as an allOf of the $ref is, where it used to be a scalar alias.
func TestRefConjunction_AComponentOverAnObjectIsTheModelThatOwnsIt(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			requireNothingHoisted(t, version, "Obj", conjSites[0], "{"+refTo("Obj")+"}")
		})
	}
}

// besideSpelling writes keywords beside a $ref to target, as `{$ref: T, K}`.
func besideSpelling(target, keywords string) string {
	return "{" + refTo(target) + ", " + keywords + "}"
}

// wrapSpelling writes the same use as `{allOf: [{$ref: T}], K}`.
func wrapSpelling(target, keywords string) string {
	return "{allOf: [{" + refTo(target) + "}], " + keywords + "}"
}

// entryNumbers names no allOf entry in the JSON of a document.
var entryNumbers = regexp.MustCompile(`openapi:allOf/\d+|/allOf/\d+`)

// spellingView is doc as two spellings of one use are held equal: no sources
// table, no position, no provenance pointer, the findings as a multiset of
// severity and code, and no entry of an allOf numbered, in an ID, a pointer or
// an Unmodeled key, since a spelling that writes a $ref as an entry and one
// that writes it beside has no other name for the entry. later-version-construct,
// the warning a 3.0 document draws for a keyword beside a $ref, is left out of
// the findings, and so is the validation-no-effect it stands in for at a
// `nullable` (docs/compilers/openapi.md#nullable).
func spellingView(t *testing.T, doc *ir.Document) map[string]any {
	t.Helper()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	text := entryNumbers.ReplaceAllStringFunc(string(raw), func(m string) string {
		return strings.TrimRight(m, "0123456789/")
	})
	var tree map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &tree))
	findings := []string{}
	if list, ok := tree["diagnostics"].([]any); ok {
		for _, d := range list {
			if statedBesideARef(d) {
				continue
			}
			findings = append(findings, fmt.Sprintf("%v %v", valueAtPath(d, "severity"), valueAtPath(d, "code")))
		}
	}
	slices.Sort(findings)
	delete(tree, "diagnostics")
	view, ok := withoutProvenancePointers(withoutPositions(tree)).(map[string]any)
	require.True(t, ok)
	view["findings"] = findings
	return view
}

// statedBesideARef reports whether the diagnostic d is one a spelling states
// for a keyword beside a $ref at 3.0: the later-version warning, or the
// warning that a `nullable` has no effect, which it stands in for.
func statedBesideARef(d any) bool {
	code := valueAtPath(d, "code")
	pointer := fmt.Sprint(valueAtPath(d, "provenance", "pointer"))
	return code == "openapi/later-version-construct" ||
		code == "openapi/validation/validation-no-effect" && strings.HasSuffix(pointer, "/nullable")
}

// withoutProvenancePointers returns tree with no provenance naming a pointer:
// a $ref kept whole is written at the use in one spelling and at an entry in
// the other.
func withoutProvenancePointers(tree any) any {
	switch x := tree.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, value := range x {
			if p, ok := value.(map[string]any); ok && key == "provenance" {
				p = maps.Clone(p)
				delete(p, "pointer")
				out[key] = p
				continue
			}
			out[key] = withoutProvenancePointers(value)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, value := range x {
			out[i] = withoutProvenancePointers(value)
		}
		return out
	default:
		return tree
	}
}

// requireSpellingsEqual holds the two documents equal up to spellingView, with
// the diff of the first lines where they are not.
func requireSpellingsEqual(t *testing.T, beside, wrapped *ir.Document, what string) {
	t.Helper()
	diff := cmp.Diff(spellingView(t, beside), spellingView(t, wrapped))
	if diff == "" {
		return
	}
	lines := strings.Split(diff, "\n")
	if len(lines) > 40 {
		lines = append(lines[:40], "...")
	}
	t.Errorf("%s: the two spellings of one use compile apart (-beside +wrapped):\n%s", what, strings.Join(lines, "\n"))
}
