// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dexpace/morphic/ir"
)

// pairBranches are the branches the union pairs below are written from.
var pairBranches = map[string]string{
	"objp": "{type: object, properties: {x: {type: string}}}", "objq": "{type: object, properties: {y: {type: string}}}",
	"obj": "{type: object}", "str": "{type: string}", "arr": "{type: array}", "nul": "{type: 'null'}",
	"req": "{required: [a]}", "any": "{}", "ea": "{type: string, enum: [a]}", "eb": "{type: string, enum: [b]}",
	"strnul": "{type: [string, 'null']}", "rstr": "{$ref: '#/components/schemas/Kstr'}",
	"rnul": "{$ref: '#/components/schemas/Knul'}", "enul": "{enum: [null]}", "cnul": "{const: null}",
	"uni": "{oneOf: [{type: string}, {type: integer}]}", "runi": "{$ref: '#/components/schemas/Kuni'}",
}

// unionPairs are a oneOf and an anyOf each, by the branches they write: both
// declaring shape, one of them, neither; null alone; sharing null alone, no
// kind, or no value of one kind; a null branch passed over or written beside
// another kind, or through a $ref beside an inline one; an object both share,
// which a scalar body does not; null as a value set; a branch that is a union,
// inline or named; and three branches, one dead. A discriminator on either
// union refuses the pair reading, so the two compose (#2104): none is here.
var unionPairs = []struct{ name, oneOf, anyOf string }{
	{"both declare shape", "objp objq", "obj"},
	{"the anyOf declares shape", "str", "objp str"},
	{"neither declares shape", "req", "any"},
	{"null alone", "nul", "nul"},
	{"null and a ref", "req", "nul rstr"},
	{"refs on both", "objp rstr", "rstr"},
	{"sharing null alone", "objp nul", "str nul"},
	{"sharing no kind", "str", "arr"},
	{"sharing no value of a kind", "ea", "eb"},
	{"a null branch passed over", "obj", "objp rnul"},
	{"shapeless sharing no value", "nul", "str"},
	{"a branch admitting null", "strnul objp", "any"},
	{"null through a ref and inline", "objp rnul", "obj nul"},
	{"sharing a kind the body does not", "objp str", "obj nul"},
	{"a value set of null", "enul", "str"},
	{"a const of null", "cnul", "arr"},
	{"a branch that is a union", "uni", "str nul"},
	{"a branch naming a union", "runi", "str"},
	{"three branches, one dead beside an object", "objp objq str", "obj"},
}

// pairBodies are the bodies the pairs are written beside, none among them.
var pairBodies = []struct{ name, body string }{
	{"none", ""},
	{"object", "type: object, properties: {a: {type: string}}"},
	{"ref to an object", "$ref: '#/components/schemas/T'"},
	{"object or null", "type: [object, 'null'], properties: {a: {type: string}}"},
	{"string or null", "type: [string, 'null']"},
	{"array", "type: array, items: {type: string}"},
	{"string", "type: string"},
	{"ref to an object or null", "$ref: '#/components/schemas/TN'"},
}

// pairUnion writes the branch list names name, in reverse where reversed.
func pairUnion(names string, reversed bool) string {
	branches := strings.Fields(names)
	if reversed {
		slices.Reverse(branches)
	}
	for i, b := range branches {
		branches[i] = pairBranches[b]
	}
	return "[" + strings.Join(branches, ", ") + "]"
}

// pairSpellings writes a oneOf and an anyOf beside body in the flat spelling,
// beside an allOf of the body, on one entry and on two in either order, and
// each one allOf deeper; reversed reverses the entries and the keywords.
func pairSpellings(body, oneOf, anyOf string, reversed bool) map[string]string {
	unions := []string{"oneOf: " + oneOf, "anyOf: " + anyOf}
	if reversed {
		slices.Reverse(unions)
	}
	both := strings.Join(unions, ", ")
	order := func(entries ...string) string {
		if reversed {
			slices.Reverse(entries)
		}
		return "{allOf: [" + strings.Join(entries, ", ") + "]}"
	}
	withBody := func(entries ...string) string {
		if body == "" {
			return order(entries...)
		}
		return order(append([]string{"{" + body + "}"}, entries...)...)
	}
	spellings := map[string]string{
		"one":    withBody("{" + both + "}"),
		"nest":   withBody("{allOf: [{" + both + "}]}"),
		"two":    withBody("{oneOf: "+oneOf+"}", "{anyOf: "+anyOf+"}"),
		"tworev": withBody("{anyOf: "+anyOf+"}", "{oneOf: "+oneOf+"}"),
		"deep":   withBody(order("{oneOf: "+oneOf+"}", "{anyOf: "+anyOf+"}")),
	}
	if body == "" {
		spellings["flat"] = "{" + both + "}"
		return spellings
	}
	spellings["flat"] = "{" + body + ", " + both + "}"
	spellings["beside"] = "{allOf: [{" + body + "}], " + both + "}"
	return spellings
}

// pairView is what a spelling's position is held to: its node's kind, each
// variant as what it is, the keys it keeps with their reasons, and the codes
// and severities of the findings at it, infos counted once.
type pairView struct {
	Kind     ir.TypeKind
	Variants []variantView
	Kept     map[string]ir.UnmodeledReason
	Findings []string
}

// variantView is what a variant is: whether its reference admits null, its
// target's kind, the properties and required ones a model holds with those
// of what it composes, a scalar's base and an enum's members.
type variantView struct {
	Nullable   bool
	Kind       ir.TypeKind
	Properties []string
	Required   []string
	Base       string
	Members    []string
}

// viewOfVariant reads what the variant ref names in doc is.
func viewOfVariant(doc *ir.Document, ref ir.TypeRef) variantView {
	variantViewValue := variantView{Nullable: ref.Nullable}
	td := doc.Types[ref.Target]
	if td == nil {
		variantViewValue.Base = string(ref.Target)
		return variantViewValue
	}
	variantViewValue.Kind = td.Kind()
	switch n := td.(type) {
	case *ir.Model:
		models := []*ir.Model{n}
		for _, typeRef := range slices.Concat(n.Mixins, baseOf(n.Base)) {
			if model, ok := doc.Types[typeRef.Target].(*ir.Model); ok {
				models = append(models, model)
			}
		}
		for _, model := range models {
			for _, property := range model.Properties {
				variantViewValue.Properties = append(variantViewValue.Properties, property.Name.Source)
				if property.Required {
					variantViewValue.Required = append(variantViewValue.Required, property.Name.Source)
				}
			}
		}
		slices.Sort(variantViewValue.Properties)
		slices.Sort(variantViewValue.Required)
	case *ir.Scalar:
		variantViewValue.Base = strings.TrimPrefix(string(n.Base.Target), "type/primitive/")
	case *ir.Enum:
		for _, enumMember := range n.Members {
			variantViewValue.Members = append(variantViewValue.Members, fmt.Sprint(enumMember.Value))
		}
	}
	return variantViewValue
}

// baseOf is base as a list, empty where there is none.
func baseOf(base *ir.TypeRef) []ir.TypeRef {
	if base == nil {
		return nil
	}
	return []ir.TypeRef{*base}
}

// viewOfPair reads the view of the component name in doc.
func viewOfPair(doc *ir.Document, diags []ir.Diagnostic, name string) pairView {
	pointer := "/components/schemas/" + name
	td := doc.Types[ir.TypeID("type/openapi"+pointer)]
	if td == nil {
		return pairView{}
	}
	pairViewValue := pairView{Kind: td.Kind(), Kept: map[string]ir.UnmodeledReason{}}
	if union, ok := td.(*ir.Union); ok {
		for _, variant := range union.Variants {
			pairViewValue.Variants = append(pairViewValue.Variants, viewOfVariant(doc, variant.Type))
		}
	}
	for key, entry := range td.Common().Unmodeled {
		pairViewValue.Kept[entryNodes.ReplaceAllString(key, "")] = entry.Reason
	}
	infos := map[string]bool{}
	for _, diagnostic := range diags {
		at := string(diagnostic.Provenance.Pointer)
		if at != pointer && !strings.HasPrefix(at, pointer+"/") {
			continue
		}
		finding := string(diagnostic.Severity) + " " + diagnostic.Code
		if diagnostic.Severity == ir.SeverityInfo {
			infos[finding] = true
			continue
		}
		pairViewValue.Findings = append(pairViewValue.Findings, finding)
	}
	slices.Sort(pairViewValue.Findings)
	pairViewValue.Findings = slices.Concat(slices.Sorted(maps.Keys(infos)), pairViewValue.Findings)
	return pairViewValue
}

// isInfo reports whether finding, a severity and a code, is an info.
func isInfo(finding string) bool { return strings.HasPrefix(finding, string(ir.SeverityInfo)+" ") }

// infosOf is the infos of pairViewValue's findings.
func infosOf(pairViewValue pairView) []string {
	return slices.DeleteFunc(slices.Clone(pairViewValue.Findings), func(f string) bool { return !isInfo(f) })
}

// facets names the facets of two views that differ, the warnings and errors
// apart from the infos, a view with no finding left in either alike with one
// whose findings are infos alone.
func facets(a, b pairView) []string {
	var out []string
	for _, f := range []struct {
		name string
		same bool
	}{
		{"kind", a.Kind == b.Kind}, {"variants", cmp.Equal(a.Variants, b.Variants)},
		{"kept", cmp.Equal(a.Kept, b.Kept)}, {"infos", slices.Equal(infosOf(a), infosOf(b))},
		{"warnings", slices.Equal(slices.DeleteFunc(slices.Clone(a.Findings), isInfo), slices.DeleteFunc(slices.Clone(b.Findings), isInfo))},
	} {
		if !f.same {
			out = append(out, f.name)
		}
	}
	return out
}

// pairExcused are the bodies beside which a union written alone lowers apart
// in its own spellings, each naming that mechanism and the open issue holding
// it (pairIssues). A pair beside one of them is excused where its split is
// one of its unions' (singleExplains); each body must still excuse one, so a
// fix elsewhere names it stale.
var pairExcused = map[string]string{
	"object or null": "#2111, #2112: a union of null alone beside a type list holding an object is its Model, an " +
		"Enum of null beside an allOf of the list (#2111), and the list on an allOf entry is kept where beside the " +
		"union it is read (#2112)",
	"string or null": "#2108: a union beside a scalar body is the body's Scalar beside its keywords, a Union beside an allOf",
	"string":         "#2108: a union beside a scalar body is the body's Scalar beside its keywords, a Union beside an allOf",
}

// pairIssues matches the reason of an excuse or an open group: the open issues
// holding the mechanism, each a question #2011 leaves to another, then why.
var pairIssues = regexp.MustCompile(`^#(2108|2109|2111|2112)(, #(2108|2109|2111|2112))*: `)

// pairOpen are the groups, by body and pair, whose spellings split where
// neither union alone does, each by a mechanism an open issue holds for one
// union too: a pair beside a scalar body, whose node #2108 decides, and a
// pair's null beside a $ref, which the nullable-inference policy widens
// beside the $ref alone (#2109). Each cites its issue and must still split.
var pairOpen = map[[2]string]string{
	{"ref to an object", "null through a ref and inline"}: "#2109: the flat spelling's $ref is widened to admit null, the others' not",
	{"string", "refs on both"}:                            "#2108: the flat spelling is the body's Scalar, the others a Union of the oneOf",
	{"string or null", "refs on both"}:                    "#2108: the flat spelling is the body's Scalar, the others a Union of the oneOf",
	{"string or null", "null and a ref"}:                  "#2108: the flat spelling is the body's Scalar, the others a Union of the $ref",
	{"string", "null and a ref"}:                          "#2108: the flat spelling is the body's Scalar, the others a Union of the $ref",
	{"string", "a branch that is a union"}:                "#2108: the flat spelling is the body's Scalar, the others a Union of the union",
	{"string or null", "a branch that is a union"}: "#2108: the flat spelling is the body's Scalar, the others a Union " +
		"of the union",
}

// singleSpellings are the spellings a union written alone has.
var singleSpellings = []string{"flat", "beside", "one", "nest"}

// pairSingles writes each union of a pair alone beside body in the spellings
// singleSpellings names, keyed by keyword initial and spelling.
func pairSingles(body, oneOf, anyOf string) map[string]string {
	out := map[string]string{}
	for keyword, union := range map[string]string{"O": "oneOf: " + oneOf, "A": "anyOf: " + anyOf} {
		out[keyword+"flat"] = "{" + body + ", " + union + "}"
		out[keyword+"beside"] = "{allOf: [{" + body + "}], " + union + "}"
		out[keyword+"one"] = "{allOf: [{" + body + "}, {" + union + "}]}"
		out[keyword+"nest"] = "{allOf: [{" + body + "}, {allOf: [{" + union + "}]}]}"
	}
	return out
}

// pairPartition groups spellings by the view each gives.
func pairPartition(spellings []string, view func(string) pairView) [][]string {
	var classes [][]string
	for _, spelling := range spellings {
		at := slices.IndexFunc(classes, func(c []string) bool { return cmp.Equal(view(c[0]), view(spelling)) })
		if at < 0 {
			classes = append(classes, []string{spelling})
			continue
		}
		classes[at] = append(classes[at], spelling)
	}
	return classes
}

// within reports whether every class of fine lies within one class of coarse.
func within(fine, coarse [][]string) bool {
	return !slices.ContainsFunc(fine, func(f []string) bool {
		return !slices.ContainsFunc(coarse, func(c []string) bool {
			return !slices.ContainsFunc(f, func(s string) bool { return !slices.Contains(c, s) })
		})
	})
}

// singleExplains reports whether the split of pair i is one of its unions':
// the two-entry spellings lower as one entry does, and over the spellings a
// union alone has, the pair splits as one of its unions alone does, or
// coarser, each spelling apart from the flat one differing from it only in
// facets that union's same spelling differs from its flat one in.
func singleExplains(view func(int, string) pairView, i int) bool {
	for _, spelling := range []string{"two", "tworev", "deep"} {
		if !cmp.Equal(view(i, "one"), view(i, spelling)) {
			return false
		}
	}
	pair := pairPartition(singleSpellings, func(s string) pairView { return view(i, s) })
	for _, keyword := range []string{"O", "A"} {
		single := pairPartition(singleSpellings, func(s string) pairView { return view(i, keyword+s) })
		if len(single) > 1 && within(single, pair) && sameFacets(view, i, keyword) {
			return true
		}
	}
	return false
}

// sameFacets reports whether each spelling of pair i differs from its flat
// one only in facets the union keyword alone differs in at that spelling.
func sameFacets(view func(int, string) pairView, i int, keyword string) bool {
	for _, spelling := range singleSpellings[1:] {
		single := facets(view(i, keyword+"flat"), view(i, keyword+spelling))
		for _, f := range facets(view(i, "flat"), view(i, spelling)) {
			if !slices.Contains(single, f) {
				return false
			}
		}
	}
	return true
}

// TestElection_AUnionPairLowersAlikeInEverySpelling holds that a oneOf and an
// anyOf, of each pair above, beside each body above or none, lower alike in
// the flat spelling and the six allOf spellings, in either order of the
// entries, the branches, the keywords and the components: the same kind of
// node, the same variants, each as what it is, the same keys kept with the
// same reasons and the same findings, wherever each stands (GitHub #2011).
// A spelling writing one union flat and the other on an entry is #2110's.
func TestElection_AUnionPairLowersAlikeInEverySpelling(t *testing.T) {
	t.Parallel()
	for group, why := range pairOpen {
		if !pairIssues.MatchString(why) {
			t.Errorf("%v is open for no question an open issue holds: %s", group, why)
		}
	}
	for body, why := range pairExcused {
		if !pairIssues.MatchString(why) {
			t.Errorf("%s is excused by no mechanism an open issue holds: %s", body, why)
		}
	}
	for _, body := range pairBodies {
		t.Run(body.name, func(t *testing.T) {
			t.Parallel()
			excuses := 0
			for _, reversed := range []bool{false, true} {
				excuses += pairGroupsAlike(t, body.name, body.body, reversed)
			}
			if why, ok := pairExcused[body.name]; ok && excuses == 0 {
				t.Errorf("excused (%s) but no pair needs it: stale", why)
			}
		})
	}
}

// pairGroupsAlike compiles every pair beside body in every spelling, holds
// each spelling to the flat one, and returns how many pairs the body's excuse
// explains.
func pairGroupsAlike(t *testing.T, name, body string, reversed bool) int {
	t.Helper()
	schemas := []string{"TN: {type: [object, 'null'], properties: {a: {type: string}}}",
		"Kstr: {type: string}", "Knul: {type: 'null'}", "Kuni: {oneOf: [{type: string}, {type: integer}]}"}
	for i, pair := range unionPairs {
		oneOf, anyOf := pairUnion(pair.oneOf, reversed), pairUnion(pair.anyOf, reversed)
		spellings := pairSpellings(body, oneOf, anyOf, reversed)
		if body != "" {
			maps.Copy(spellings, pairSingles(body, oneOf, anyOf))
		}
		for _, spelling := range slices.Sorted(maps.Keys(spellings)) {
			schemas = append(schemas, fmt.Sprintf("P%d%s: %s", i, spelling, spellings[spelling]))
		}
	}
	doc, diags := compileAnnotationSpec(t, "pairs", bothKeywordsDoc("3.1.0", "", reversed, schemas...))
	view := func(i int, spelling string) pairView {
		return viewOfPair(doc, diags, fmt.Sprintf("P%d%s", i, spelling))
	}
	excused := 0
	for i, pair := range unionPairs {
		apart := map[string]string{}
		for _, spelling := range slices.Sorted(maps.Keys(pairSpellings(body, "[]", "[]", false))) {
			if diff := cmp.Diff(view(i, "flat"), view(i, spelling)); diff != "" {
				apart[spelling] = diff
			}
		}
		why, open := pairOpen[[2]string{name, pair.name}]
		switch {
		case open && len(apart) == 0:
			t.Errorf("%s, reversed %t: open (%s) but lowers alike: stale", pair.name, reversed, why)
			continue
		case open, len(apart) == 0:
			continue
		}
		if _, ok := pairExcused[name]; ok && singleExplains(view, i) {
			excused++
			continue
		}
		t.Errorf("%s, reversed %t: spellings differ from the flat (-flat +spelling): %v", pair.name, reversed, apart)
	}
	return excused
}
