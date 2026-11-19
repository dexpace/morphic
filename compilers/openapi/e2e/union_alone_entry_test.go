// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"cmp"
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// aloneTargets are the components the unions below name: two objects, a
// string, and a schema of null alone in each spelling a version allows.
var aloneTargets = []string{
	"X: {type: object, properties: {id: {type: string}}}",
	"Y: {type: object, properties: {n: {type: integer}}}",
	"Z: {type: object, properties: {z: {type: integer}}}",
	"Str: {type: string}",
}

// aloneRef is a $ref to the component named.
func aloneRef(name string) string { return "{$ref: '#/components/schemas/" + name + "'}" }

// aloneUnion is a union a schema writes: its keyword and branches, and what
// is written beside them on the same schema.
type aloneUnion struct {
	name     string
	keyword  string
	branches []string
	beside   string
	// kind is the one kind but null the branches admit, which a `type` every
	// branch says names, object where it is empty, and nullless that it names
	// no null beside it.
	kind     string
	nullless bool
	// untyped marks a union no `type` is written beside: its branches declare
	// no shape, so a type they say is a body (hasUnionSiblings), or it writes a
	// 3.0 `nullable`, which applies to a type written beside it.
	untyped bool
	// respelt marks a union differing from another only in how its null is
	// spelt, which is what lifts it, so it is written in a few placements.
	respelt bool
}

// written is u as one schema, its branches in reverse where reversed, with
// more written beside it.
func (u aloneUnion) written(reversed bool, more ...string) string {
	branches := slices.Clone(u.branches)
	if reversed {
		slices.Reverse(branches)
	}
	parts := []string{u.keyword + ": [" + strings.Join(branches, ", ") + "]"}
	if u.beside != "" {
		parts = append(parts, u.beside)
	}
	return "{" + strings.Join(append(parts, more...), ", ") + "}"
}

// aloneNulls are the branches admitting null alone a version writes, with the
// component each $ref to one names.
func aloneNulls(version string) (map[string]string, []string) {
	if strings.HasPrefix(version, "3.0") {
		return map[string]string{
			"idiom":     "{type: object, enum: [null], nullable: true}",
			"ref idiom": aloneRef("NI"),
		}, []string{"NI: {type: object, enum: [null], nullable: true}"}
	}
	return map[string]string{
		"type":         "{type: 'null'}",
		"enum":         "{enum: [null]}",
		"const":        "{const: null}",
		"ref type":     aloneRef("NT"),
		"ref enum":     aloneRef("NE"),
		"ref const":    aloneRef("NC"),
		"with a note":  "{type: 'null', description: n}",
		"with a title": "{type: 'null', title: n}",
	}, []string{"NT: {type: 'null'}", "NE: {enum: [null]}", "NC: {const: null}"}
}

// aloneUnions are the unions the grid writes at version: one object branch
// beside each spelling of the null, as a oneOf and an anyOf; two object
// branches beside the null; the null alone; a string, an inline object; one
// and two object branches with no null, the two routed by a discriminator
// carrying an extension and a key no field holds; two branches narrowing an
// object, which a `type: object` beside them makes a body; where the version
// reads it, a `nullable` beside the union; and a oneOf beside an anyOf on one
// schema.
func aloneUnions(version string) []aloneUnion {
	nulls, _ := aloneNulls(version)
	null := nulls["type"]
	if null == "" {
		null = nulls["idiom"]
	}
	var out []aloneUnion
	for _, keyword := range []string{"oneOf", "anyOf"} {
		for _, name := range slices.Sorted(maps.Keys(nulls)) {
			out = append(out, aloneUnion{name: fmt.Sprintf("%s[X, %s]", keyword, name), keyword: keyword,
				branches: []string{aloneRef("X"), nulls[name]}, respelt: nulls[name] != null})
		}
		out = append(out,
			aloneUnion{name: keyword + "[X, Y, null]", keyword: keyword, branches: []string{aloneRef("X"), aloneRef("Y"), null}},
			aloneUnion{name: keyword + "[null]", keyword: keyword, branches: []string{null}, untyped: true})
	}
	out = append(out,
		aloneUnion{name: "oneOf[Str, null]", keyword: "oneOf", branches: []string{aloneRef("Str"), null}, kind: "string"},
		aloneUnion{name: "oneOf[inline, null]", keyword: "oneOf", branches: []string{"{type: object, properties: {a: {type: string}}}", null}},
		aloneUnion{name: "oneOf[X]", keyword: "oneOf", branches: []string{aloneRef("X")}},
		aloneUnion{name: "oneOf[X, Y]", keyword: "oneOf", branches: []string{aloneRef("X"), aloneRef("Y")}},
		aloneUnion{name: "oneOf[X, null] beside anyOf[Y, Z]", keyword: "oneOf", branches: []string{aloneRef("X"), null},
			beside: "anyOf: [" + aloneRef("Y") + ", " + aloneRef("Z") + "]"},
		aloneUnion{name: "oneOf[X, Y] routed", keyword: "oneOf", branches: []string{aloneRef("X"), aloneRef("Y")},
			beside: "discriminator: {propertyName: id, x-d: 1, mapped: D}"},
		aloneUnion{name: "oneOf[narrowing objects]", keyword: "oneOf", nullless: true,
			branches: []string{"{type: object, required: [a]}", "{type: object, required: [b]}"}})
	if strings.HasPrefix(version, "3.0") {
		out = append(out,
			aloneUnion{name: "oneOf[X] nullable", keyword: "oneOf", branches: []string{aloneRef("X")}, beside: "nullable: true",
				untyped: true},
			aloneUnion{name: "oneOf[X, Y] nullable", keyword: "oneOf", branches: []string{aloneRef("X"), aloneRef("Y")},
				beside: "nullable: true", untyped: true})
	}
	return out
}

// aloneSpelling is one way to write a union alone on an allOf entry, and the
// schema writing the same union with no allOf it is compared to.
type aloneSpelling struct {
	name, entry, flat string
}

// aloneSpellings writes aloneUnionValue on an allOf entry beside nothing a
// variant would carry, at version: alone, one allOf deeper, beside a `{}` entry
// and, from 3.1, a `true` one, with a note or, unless aloneUnionValue is
// untyped, a type every branch says, null included, on its own entry, on
// another before or after it, or beside the allOf, and with a bound, an
// annotation or an extension on an entry after it or beside the allOf; each in
// both orders of the branches. A respelt union is written in four of these.
func aloneSpellings(version string, aloneUnionValue aloneUnion) []aloneSpelling {
	all := aloneSpellingsOf(version, aloneUnionValue)
	if !aloneUnionValue.respelt {
		return all
	}
	return slices.DeleteFunc(all, func(sp aloneSpelling) bool {
		_, placement, _ := strings.Cut(sp.name, "/")
		return !slices.Contains([]string{"alone", "one allOf deeper", "a note on an entry before",
			"a type every branch says on an entry after"}, placement)
	})
}

// aloneSpellingsOf is every spelling aloneSpellings writes of a union not
// respelt.
func aloneSpellingsOf(version string, aloneUnionValue aloneUnion) []aloneSpelling {
	legacy := strings.HasPrefix(version, "3.0")
	kind := cmp.Or(aloneUnionValue.kind, "object")
	typed := "type: [" + kind + ", 'null']"
	switch {
	case aloneUnionValue.nullless:
		typed = "type: " + kind
	case legacy:
		typed = "type: " + kind + ", nullable: true"
	}
	beside := []struct {
		name, keyword string
		everywhere    bool
	}{
		{"a bound", "minProperties: 1", false},
		{"an annotation", "readOnly: true", false},
		{"an extension", "x-a: 1", false},
		{"a note", "description: d", true},
		{"a type every branch says", typed, true},
	}
	if aloneUnionValue.untyped {
		beside = beside[:len(beside)-1]
	}
	var out []aloneSpelling
	for _, reversed := range []bool{false, true} {
		order := map[bool]string{false: "as written", true: "branches reversed"}[reversed]
		bare := aloneUnionValue.written(reversed)
		out = append(out,
			aloneSpelling{order + "/alone", "{allOf: [" + bare + "]}", bare},
			aloneSpelling{order + "/one allOf deeper", "{allOf: [{allOf: [" + bare + "]}]}", bare},
			aloneSpelling{order + "/beside {}", "{allOf: [{}, " + bare + "]}", bare})
		if !legacy {
			out = append(out, aloneSpelling{order + "/beside true", "{allOf: [" + bare + ", true]}", bare})
		}
		for _, b := range beside {
			flat := aloneUnionValue.written(reversed, b.keyword)
			out = append(out,
				aloneSpelling{order + "/" + b.name + " on an entry after", "{allOf: [" + bare + ", {" + b.keyword + "}]}", flat},
				aloneSpelling{order + "/" + b.name + " beside the allOf", "{allOf: [" + bare + "], " + b.keyword + "}", flat})
			if b.everywhere {
				out = append(out,
					aloneSpelling{order + "/" + b.name + " on its entry", "{allOf: [" + flat + "]}", flat},
					aloneSpelling{order + "/" + b.name + " on an entry before", "{allOf: [{" + b.keyword + "}, " + bare + "]}", flat})
			}
		}
	}
	return out
}

// aloneSites are a component, a property, an array's items, a parameter's
// schema and a request body's.
var aloneSites = []string{"component", "property", "items", "parameter", "body"}

// aloneSite returns conjSites' entry for the site named.
func aloneSite(t *testing.T, name string) conjSite {
	t.Helper()
	i := slices.IndexFunc(conjSites, func(conjSiteValue conjSite) bool { return conjSiteValue.name == name })
	require.GreaterOrEqual(t, i, 0, "a site of conjSites: %s", name)
	return conjSites[i]
}

// aloneView compiles spec and returns its document as entryTree reads it, and
// its findings each as its severity, code and pointer, sorted: two spellings of
// one schema read alike where they hold the same facts and report the same
// findings at the same places, whatever a message says of the route taken.
func aloneView(t *testing.T, spec string) string {
	t.Helper()
	doc, diags := compileUnrefused(t, spec)
	tree, ok := documentTree(t, doc).(map[string]any)
	require.True(t, ok, "a document encodes as an object")
	delete(tree, "sources")
	delete(tree, "diagnostics")
	findings := make([]string, 0, len(diags))
	for _, diagnostic := range diags {
		findings = append(findings, fmt.Sprint(diagnostic.Severity, " ", diagnostic.Code, " ", entryIndexless(string(diagnostic.Provenance.Pointer))))
	}
	slices.Sort(findings)
	return fmt.Sprint(entryIndexless(tree)) + "\n" + strings.Join(findings, "\n")
}

// TestUnionAlone_OnAnEntryLowersAsWrittenAtThePosition holds a union written
// on an allOf entry beside nothing a variant would carry to lower as the same
// union written at the position does (docs/compilers/openapi.md#union-beside-structure):
// at every version and site, for every union aloneUnions writes and every
// spelling aloneSpellings does, the document and its findings are the flat
// spelling's up to the allOf entries a pointer passes through (aloneView).
func TestUnionAlone_OnAnEntryLowersAsWrittenAtThePosition(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"3.0.3", "3.1.0", "3.2.0"} {
		_, nullTargets := aloneNulls(version)
		others := slices.Concat(aloneTargets, nullTargets)
		for _, siteName := range aloneSites {
			for _, aloneUnion := range aloneUnions(version) {
				t.Run(version+"/"+siteName+"/"+aloneUnion.name, func(t *testing.T) {
					t.Parallel()
					site := aloneSite(t, siteName)
					flats := map[string]string{}
					for _, sp := range aloneSpellings(version, aloneUnion) {
						want, ok := flats[sp.flat]
						if !ok {
							want = aloneView(t, site.write(version, sp.flat, others...))
							flats[sp.flat] = want
						}
						assert.Equal(t, want, aloneView(t, site.write(version, sp.entry, others...)),
							"%s: %s lowers as %s", sp.name, sp.entry, sp.flat)
					}
				})
			}
		}
	}
}

// aloneBodies are conjuncts beside a union on an allOf entry that keep it from
// standing alone: a property set, a $ref and a `type` the null branch does not
// say, each paired with the schema writing the same keywords beside the union;
// a discriminator beside the allOf or on another entry, paired with itself, as
// the union with the discriminator written beside it stands alone; and an
// allOf the union's own entry composes beside it, paired with the same allOf
// beside the union.
var aloneBodies = []struct {
	name, entry, flat string
}{
	{"properties on another entry", "{allOf: [{properties: {z: {type: string}}}, {oneOf: [" + aloneRef("X") + ", {type: 'null'}]}]}",
		"{properties: {z: {type: string}}, oneOf: [" + aloneRef("X") + ", {type: 'null'}]}"},
	{"a $ref entry", "{allOf: [" + aloneRef("Y") + ", {oneOf: [" + aloneRef("X") + ", {type: 'null'}]}]}",
		"{allOf: [" + aloneRef("Y") + "], oneOf: [" + aloneRef("X") + ", {type: 'null'}]}"},
	{"type object on another entry", "{allOf: [{type: object}, {oneOf: [" + aloneRef("X") + ", {type: 'null'}]}]}",
		"{type: object, oneOf: [" + aloneRef("X") + ", {type: 'null'}]}"},
	{"type object on its entry", "{allOf: [{type: object, oneOf: [" + aloneRef("X") + ", {type: 'null'}]}]}",
		"{type: object, oneOf: [" + aloneRef("X") + ", {type: 'null'}]}"},
	{"type object beside the allOf", "{type: object, allOf: [{oneOf: [" + aloneRef("X") + ", {type: 'null'}]}]}",
		"{type: object, oneOf: [" + aloneRef("X") + ", {type: 'null'}]}"},
	{"a discriminator beside the allOf", "{discriminator: {propertyName: id}, allOf: [{oneOf: [" + aloneRef("X") + ", {type: 'null'}]}]}",
		"{discriminator: {propertyName: id}, allOf: [{oneOf: [" + aloneRef("X") + ", {type: 'null'}]}]}"},
	{"a discriminator on another entry", "{allOf: [{discriminator: {propertyName: id}}, {oneOf: [" + aloneRef("X") + ", {type: 'null'}]}]}",
		"{allOf: [{discriminator: {propertyName: id}}, {oneOf: [" + aloneRef("X") + ", {type: 'null'}]}]}"},
	{"an allOf on its entry", "{allOf: [{oneOf: [" + aloneRef("X") + ", {type: 'null'}], allOf: [{description: d}]}]}",
		"{oneOf: [" + aloneRef("X") + ", {type: 'null'}], allOf: [{description: d}]}"},
}

// TestUnionAlone_ABodyBesideItStillDistributes holds a union written on an
// allOf entry beside a conjunct that keeps it from standing alone to stay
// distributed, one variant included (docs/compilers/openapi.md#union-beside-structure):
// at a component and a property both spellings of each row of aloneBodies are
// a Union of one model composed over X.
func TestUnionAlone_ABodyBesideItStillDistributes(t *testing.T) {
	t.Parallel()
	for _, siteName := range []string{"component", "property"} {
		for _, b := range aloneBodies {
			t.Run(siteName+"/"+b.name, func(t *testing.T) {
				t.Parallel()
				site := aloneSite(t, siteName)
				for _, schema := range []string{b.entry, b.flat} {
					doc, _ := compileUnrefused(t, site.write("3.1.0", schema, aloneTargets...))
					variants := distributedOver(t, doc, entryPositionID(jsontext.Pointer(site.pointer)))
					require.Len(t, variants, 1, "%s: one variant", schema)
					var only *ir.Model
					for _, model := range variants {
						only = model
					}
					assert.Contains(t, slices.Concat([]ir.TypeID{only.Base.Target}, refTargets(only.Mixins)), componentID("X"),
						"%s: the variant composes X", schema)
				}
			})
		}
	}
}

// refTargets returns the targets refs name.
func refTargets(refs []ir.TypeRef) []ir.TypeID {
	out := make([]ir.TypeID, 0, len(refs))
	for _, typeRef := range refs {
		out = append(out, typeRef.Target)
	}
	return out
}
