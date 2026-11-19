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

// draftPrincipal is a kind an allOf entry can write, as draft 7 spells it and
// as 2020-12 does.
type draftPrincipal struct{ name, draft, twin string }

// draftPrincipals are the principal kinds an entry declaring draft 7 can write.
var draftPrincipals = []draftPrincipal{
	{"list", "type: array, items: {type: string}", "type: array, items: {type: string}"},
	{"tuple", "type: array, items: [{type: string}]", "type: array, prefixItems: [{type: string}]"},
	{"type list", "type: [string, integer]", "type: [string, integer]"},
	{"oneOf", "oneOf: [{type: string}, {type: integer}]", "oneOf: [{type: string}, {type: integer}]"},
	{"anyOf", "anyOf: [{type: string}, {type: integer}]", "anyOf: [{type: string}, {type: integer}]"},
	{"scalar", "type: string", "type: string"},
	{"number as text", "type: string, format: double", "type: string, format: double"},
	{"enum", "enum: [a, b]", "enum: [a, b]"},
	{"const", "const: a", "const: a"},
	{"object", "type: object, properties: {a: {type: string}}", "type: object, properties: {a: {type: string}}"},
	{"map", "type: object, additionalProperties: {type: string}", "type: object, additionalProperties: {type: string}"},
}

// draftSpellings write a draft 7 `dependencies` beside a principal: on the
// principal's own entry, on another entry after it and before it, and flat.
// Each takes the resource keywords naming the dialect, the principal and the
// keyword.
var draftSpellings = map[string]func(resource string, p draftPrincipal, keyword string) string{
	"own": func(r string, p draftPrincipal, k string) string {
		return "{allOf: [{" + r + ", " + p.draft + ", " + k + "}, {description: d}]}"
	},
	"entry": func(r string, p draftPrincipal, k string) string {
		return "{allOf: [{" + p.twin + "}, {" + r + ", " + k + "}]}"
	},
	"entry first": func(r string, p draftPrincipal, k string) string {
		return "{allOf: [{" + r + ", " + k + "}, {" + p.twin + "}]}"
	},
	"flat": func(r string, p draftPrincipal, k string) string { return "{" + r + ", " + p.draft + ", " + k + "}" },
}

// draftCell is one schema of the grid: where it is declared and the value its
// `dependencies` writes, unique to it.
type draftCell struct {
	component, spelling, value string
	kind                       draftPrincipal
	property                   bool
}

// TestEntryDialect_ADraftsDependenciesIsKeptAsFlat pins a draft 7 allOf entry's
// `dependencies` beside an inline principal of each kind, on the principal's
// own entry or another, at a component and a property, in both declaration
// orders: kept once, on the node the position lowers to or the carrier holding
// what it keeps, as validation_only with the validation-only report, as the
// flat spelling of the schema in draft 7 keeps it
// (docs/compilers/openapi.md#validation-only).
func TestEntryDialect_ADraftsDependenciesIsKeptAsFlat(t *testing.T) {
	t.Parallel()
	n := len(draftPrincipals) * len(draftSpellings) * 2
	cells, lines := make([]draftCell, 0, n), make([]string, 0, n)
	for i, p := range draftPrincipals {
		for _, spelling := range slices.Sorted(maps.Keys(draftSpellings)) {
			for _, property := range []bool{false, true} {
				name := fmt.Sprintf("K%d%s%t", i, strings.ReplaceAll(spelling, " ", ""), property)
				value := "n" + strings.ToLower(name)
				resource := "$id: 'https://example.com/" + value + "', $schema: " + draft07
				schema := draftSpellings[spelling](resource, p, "dependencies: {"+value+": [b]}")
				if property {
					schema = "{type: object, properties: {p: " + schema + "}}"
				}
				cells = append(cells, draftCell{component: name, spelling: spelling, value: value, kind: p, property: property})
				lines = append(lines, "    "+name+": "+schema)
			}
		}
	}
	forward := keptDependencies(t, lines, cells)
	reversed := slices.Clone(lines)
	slices.Reverse(reversed)
	backward := keptDependencies(t, reversed, cells)
	for _, cell := range cells {
		t.Run(cell.kind.name+" "+cell.spelling+map[bool]string{true: " at a property"}[cell.property], func(t *testing.T) {
			t.Parallel()
			got := forward[cell.component]
			assert.Equal(t, got, backward[cell.component], "the declaration order decides nothing")
			assert.Equal(t, []string{"position validation_only"}, got.kept, "kept once, where the position keeps it")
			assert.True(t, got.reported, "reported as a validation-only keyword")
		})
	}
}

// keptDependency is where a cell's `dependencies` is kept, by what holds it
// and its reason, and whether the validation-only report names it.
type keptDependency struct {
	kept     []string
	reported bool
}

// keptDependencies compiles the schemas lines declares, in their order, and
// returns where each cell's `dependencies` is kept.
func keptDependencies(t *testing.T, lines []string, cells []draftCell) map[string]keptDependency {
	t.Helper()
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
	doc, diags := parseFull(t, openapitest.ComponentSpec(b.String()))
	openapitest.RequireNoErrorDiags(t, diags)
	sites := unmodeledSites(doc)
	out := make(map[string]keptDependency, len(cells))
	for _, cell := range cells {
		var k keptDependency
		owner := positionOwner(t, doc, cell)
		for _, site := range sites {
			if site.key != "openapi:dependencies" || !strings.Contains(string(site.entry.Value), `"`+cell.value+`"`) {
				continue
			}
			where := "elsewhere " + site.path
			if e, ok := owner["openapi:dependencies"]; ok && string(e.Value) == string(site.entry.Value) {
				where = "position"
			}
			k.kept = append(k.kept, where+" "+string(site.entry.Reason))
		}
		at := "/components/schemas/" + cell.component
		for _, d := range diags {
			if d.Code == diag.ValidationOnlyKeyword && strings.HasPrefix(string(d.Provenance.Pointer), at) &&
				strings.Contains(d.Message, `"dependencies"`) {
				k.reported = true
			}
		}
		out[cell.component] = k
	}
	return out
}

// positionOwner returns the Unmodeled of what keeps what the cell's position
// writes that no field holds: the node it lowers to, where it owns one, else
// the property carrying it.
func positionOwner(t *testing.T, doc *ir.Document, cell draftCell) ir.Unmodeled {
	t.Helper()
	if !cell.property {
		return doc.Types[componentID(cell.component)].Common().Unmodeled
	}
	m, ok := doc.Types[componentID(cell.component)].(*ir.Model)
	require.True(t, ok, "%s is a model", cell.component)
	p, ok := propByWire(m, "p")
	require.True(t, ok)
	if p.Type.Target == ir.TypeID("t/anon/components/schemas/"+cell.component+"/properties/p") {
		return doc.Types[p.Type.Target].Common().Unmodeled
	}
	return p.Unmodeled
}
