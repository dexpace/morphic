// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// numberTextPrincipals carry a number as text: a string of a numeric format,
// alone and as a type list's variant.
var numberTextPrincipals = []string{"type: string, format: double", "type: [string, boolean], format: int64"}

// textBounds are the value bounds a number carried as text holds none of,
// each a printf format of its value, unique to the schema writing it.
var textBounds = map[string]string{
	"minimum": "minimum: %d", "maximum": "maximum: %d", "exclusiveMinimum": "exclusiveMinimum: %d",
	"exclusiveMaximum": "exclusiveMaximum: %d", "multipleOf": "multipleOf: %d", "minLength": "minLength: %d",
	"maxLength": "maxLength: %d", "pattern": "pattern: '^c%d$'", "minProperties": "minProperties: %d",
	"maxProperties": "maxProperties: %d",
}

// textSpellings write a bound beside a principal: beside its allOf, on
// another entry after it and before it, on its own entry, and flat.
var textSpellings = map[string]func(p, b string) string{
	"beside":      func(p, b string) string { return "{allOf: [{" + p + "}], " + b + "}" },
	"entry":       func(p, b string) string { return "{allOf: [{" + p + "}, {" + b + "}]}" },
	"entry first": func(p, b string) string { return "{allOf: [{" + b + "}, {" + p + "}]}" },
	"own":         func(p, b string) string { return "{allOf: [{" + p + ", " + b + "}, {description: d}]}" },
	"flat":        func(p, b string) string { return "{" + p + ", " + b + "}" },
}

// textCell is one schema of the grid and the bound it writes.
type textCell struct {
	component, keyword, value, spelling string
	principal                           int
	property                            bool
}

// TestNumberText_ABoundBesideAPrincipalIsKeptAsFlat pins each value bound
// written beside an allOf principal carrying a number as text, on another
// entry in either order or on the principal's own entry, at a component and a
// property, in both declaration orders: kept once, under no_ir_home with the
// number-as-text info, on the node the flat spelling keeps it on, a type
// list's text variant where its own entry writes it and else the Union
// (docs/compilers/openapi.md#string-formats).
func TestNumberText_ABoundBesideAPrincipalIsKeptAsFlat(t *testing.T) {
	t.Parallel()
	n := len(numberTextPrincipals) * len(textBounds) * len(textSpellings) * 2
	cells, lines := make([]textCell, 0, n), make([]string, 0, n)
	for i, p := range numberTextPrincipals {
		for _, keyword := range slices.Sorted(maps.Keys(textBounds)) {
			for _, spelling := range slices.Sorted(maps.Keys(textSpellings)) {
				for _, property := range []bool{false, true} {
					value := len(cells) + 3
					name := fmt.Sprintf("T%d%s%s%t", i, keyword, strings.ReplaceAll(spelling, " ", ""), property)
					schema := textSpellings[spelling](p, fmt.Sprintf(textBounds[keyword], value))
					if property {
						schema = "{type: object, properties: {p: " + schema + "}}"
					}
					cells = append(cells, textCell{component: name, keyword: keyword, value: fmt.Sprint(value),
						spelling: spelling, principal: i, property: property})
					lines = append(lines, "    "+name+": "+schema)
				}
			}
		}
	}
	reversed := slices.Clone(lines)
	slices.Reverse(reversed)
	forward, backward := textBoundsKept(t, lines, cells), textBoundsKept(t, reversed, cells)
	for _, cell := range cells {
		if cell.spelling == "flat" {
			continue
		}
		name := fmt.Sprintf("%s %s %s%s", numberTextPrincipals[cell.principal], cell.keyword, cell.spelling,
			map[bool]string{true: " at a property"}[cell.property])
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := forward[cell.component]
			assert.Equal(t, got, backward[cell.component], "the declaration order decides nothing")
			flat := forward[strings.Replace(cell.component, strings.ReplaceAll(cell.spelling, " ", ""), "flat", 1)]
			require.Len(t, flat.kept, 1, "the flat spelling keeps it once")
			if cell.principal == 1 && cell.spelling != "own" {
				// The type list's text variant holds what its own entry writes, so
				// one written elsewhere is kept on the Union, where a second
				// writer of the keyword cannot overwrite it.
				owner := "doc.Types[t/openapi/components/schemas/X].Unmodeled"
				if cell.property {
					owner = "doc.Types[t/anon/components/schemas/X/properties/p].Unmodeled"
				}
				flat.kept = []string{owner + " " + string(ir.ReasonNoIRHome)}
			}
			assert.Equal(t, flat, got, "kept and reported as the flat spelling is")
		})
	}
}

// keptText is where a cell's bound is kept, by the node holding it below the
// cell and its reason, and whether the number-as-text info names it.
type keptText struct {
	kept     []string
	reported bool
}

// textBoundsKept compiles the schemas lines declares, in their order, and
// returns where each cell's bound is kept.
func textBoundsKept(t *testing.T, lines []string, cells []textCell) map[string]keptText {
	t.Helper()
	doc, diags := parseFull(t, openapitest.ComponentSpec(strings.Join(lines, "\n")+"\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	sites := unmodeledSites(doc)
	out := make(map[string]keptText, len(cells))
	for _, cell := range cells {
		var k keptText
		want := cell.value
		if cell.keyword == "pattern" {
			want = `"^c` + cell.value + `$"`
		}
		for _, site := range sites {
			if site.key == "openapi:"+cell.keyword && string(site.entry.Value) == want {
				k.kept = append(k.kept, strings.ReplaceAll(site.path, cell.component, "X")+" "+string(site.entry.Reason))
			}
		}
		for _, d := range diags {
			if d.Code == diag.DegradedConstruct && strings.HasPrefix(string(d.Provenance.Pointer), "/components/schemas/"+cell.component+"/") &&
				strings.HasPrefix(d.Message, cell.keyword+" bounds no number written as text") {
				k.reported = true
			}
		}
		out[cell.component] = k
	}
	return out
}

// TestNumberText_TwoWritersOfABoundKeepBoth pins a bound two conjuncts write
// apart from the principal carrying a number as text, beside the allOf and on
// an entry, or on two entries, and one a type list's own entry and the
// position both write: each value is kept once, none overwriting another.
func TestNumberText_TwoWritersOfABoundKeepBoth(t *testing.T) {
	t.Parallel()
	const text = "type: string, format: double"
	doc, diags := parseFull(t, openapitest.ComponentSpec(
		"    E: {allOf: [{"+text+", minimum: 11}, {minimum: 12}]}\n"+
			"    T: {allOf: [{"+text+"}, {minimum: 21}, {minimum: 22}]}\n"+
			"    B: {allOf: [{"+text+"}, {minimum: 31}], minimum: 32}\n"+
			"    L: {allOf: [{type: [string, boolean], format: int64, minimum: 41}], minimum: 42}\n"))
	openapitest.RequireNoErrorDiags(t, diags)
	sites := unmodeledSites(doc)
	for _, want := range []string{"11", "12", "21", "22", "31", "32", "41", "42"} {
		var held int
		for _, site := range sites {
			if strings.Contains(string(site.entry.Value), `"minimum":`+want) || string(site.entry.Value) == want {
				held++
			}
		}
		assert.Equal(t, 1, held, "minimum %s is kept once", want)
	}
}
