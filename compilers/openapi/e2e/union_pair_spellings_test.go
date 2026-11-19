// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"maps"
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
	"rnul": "{$ref: '#/components/schemas/Knul'}",
}

// unionPairs are a oneOf and an anyOf each, by the branches they write: both
// declaring shape, one of them, neither; null alone; sharing null alone, no
// kind, or no value of one kind; a null branch passed over or written beside
// another kind.
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
// variant's nullability, the keys it keeps with their reasons, and the codes
// and severities of the findings at it, infos counted once.
type pairView struct {
	Kind     ir.TypeKind
	Variants []bool
	Kept     map[string]ir.UnmodeledReason
	Findings []string
}

// viewOfPair reads the view of the component name in doc.
func viewOfPair(doc *ir.Document, diags []ir.Diagnostic, name string) pairView {
	pointer := "/components/schemas/" + name
	td := doc.Types[ir.TypeID("t/openapi"+pointer)]
	if td == nil {
		return pairView{}
	}
	v := pairView{Kind: td.Kind(), Kept: map[string]ir.UnmodeledReason{}}
	if u, ok := td.(*ir.Union); ok {
		for _, variant := range u.Variants {
			v.Variants = append(v.Variants, variant.Type.Nullable)
		}
	}
	for key, entry := range td.Common().Unmodeled {
		v.Kept[entryNodes.ReplaceAllString(key, "")] = entry.Reason
	}
	infos := map[string]bool{}
	for _, d := range diags {
		at := string(d.Provenance.Pointer)
		if at != pointer && !strings.HasPrefix(at, pointer+"/") {
			continue
		}
		finding := string(d.Severity) + " " + d.Code
		if d.Severity == ir.SeverityInfo {
			infos[finding] = true
			continue
		}
		v.Findings = append(v.Findings, finding)
	}
	slices.Sort(v.Findings)
	v.Findings = slices.Concat(slices.Sorted(maps.Keys(infos)), v.Findings)
	return v
}

// pairExcused are the bodies beside which a union written alone lowers apart
// in its own spellings, each naming that mechanism. A pair beside one of them
// is excused where its split is one of its unions' (singleExplains); each body
// must still excuse one, so a fix elsewhere names it stale.
var pairExcused = map[string]string{
	"object":         "a union sharing no kind with the body warns beside an allOf of it, not beside its keywords",
	"object or null": "a union beside a type list with properties lowers apart beside the list's keywords",
	"string or null": "#2108: a union beside a scalar body is the body's Scalar beside its keywords, a Union beside an allOf",
	"array":          "#2108: a union beside an array body is the body's node beside its keywords, a Union beside an allOf",
	"string":         "#2108: a union beside a scalar body is the body's Scalar beside its keywords, a Union beside an allOf",
}

// pairOpen are the groups, by body and pair, whose spellings split where
// neither union alone does: a pair beside a scalar body, whose node #2108
// decides as it does one union's. Each cites it and must still split.
var pairOpen = map[[2]string]string{
	{"string", "refs on both"}:           "#2108: the flat spelling is the body's Scalar, the others a Union of the oneOf",
	{"string or null", "refs on both"}:   "#2108: the flat spelling is the body's Scalar, the others a Union of the oneOf",
	{"string or null", "null and a ref"}: "#2108: the flat spelling is the body's Scalar, the others a Union of the $ref",
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
// coarser.
func singleExplains(view func(int, string) pairView, i int) bool {
	for _, spelling := range []string{"two", "tworev", "deep"} {
		if !cmp.Equal(view(i, "one"), view(i, spelling)) {
			return false
		}
	}
	pair := pairPartition(singleSpellings, func(s string) pairView { return view(i, s) })
	for _, keyword := range []string{"O", "A"} {
		single := pairPartition(singleSpellings, func(s string) pairView { return view(i, keyword+s) })
		if len(single) > 1 && within(single, pair) {
			return true
		}
	}
	return false
}

// TestElection_AUnionPairLowersAlikeInEverySpelling holds that a oneOf and an
// anyOf, of each pair above, beside each body above or none, lower alike in
// the flat spelling and the six allOf spellings, in either order of the
// entries, the branches, the keywords and the components: the same kind of
// node, the same variants and their nullability, the same keys kept with the
// same reasons and the same findings, wherever each stands (GitHub #2011).
func TestElection_AUnionPairLowersAlikeInEverySpelling(t *testing.T) {
	t.Parallel()
	for group, why := range pairOpen {
		if !strings.Contains(why, "#2108") {
			t.Errorf("%v is open for no question #2108 holds: %s", group, why)
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
		"Kstr: {type: string}", "Knul: {type: 'null'}"}
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
