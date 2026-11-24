// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/v2"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// boundsText is the constraints the node id names holds, and beneath it those
// of the element or the value type it holds, as one line.
func boundsText(t *testing.T, doc *ir.Document, id ir.TypeID) string {
	t.Helper()
	var out []string
	for {
		var bounds *ir.Constraints
		next := ir.TypeID("")
		switch td := doc.Types[id].(type) {
		case *ir.List:
			bounds, next = td.Constraints, td.ElementType.Target
		case *ir.MapT:
			bounds, next = td.Constraints, td.Value.Target
		case *ir.Scalar:
			bounds = td.Constraints
		case *ir.Enum:
			values := make([]string, len(td.Members))
			for i, member := range td.Members {
				values[i] = member.Value.String
			}
			out = append(out, "enum "+strings.Join(values, " "))
		default:
		}
		if bounds != nil {
			raw, err := json.Marshal(bounds, json.Deterministic(true))
			require.NoError(t, err)
			out = append(out, strings.ReplaceAll(string(raw), `,"uniqueItems":false`, ""))
		}
		if next == "" {
			return strings.Join(out, " of ")
		}
		id = next
	}
}

// entryOfM finds an entry of the model M named by its index.
var entryOfM = regexp.MustCompile(`M/allOf/\d+`)

// foldedText is the members f of models as a reader meets them, each its notes,
// its type and what it keeps verbatim (memberText), the bounds of that type,
// and then how many reports the folds of f draw. An entry of M is named by no
// index, which its order decides.
func foldedText(t *testing.T, doc *ir.Document, diags []ir.Diagnostic, models ...string) string {
	t.Helper()
	var out strings.Builder
	for _, name := range models {
		model, isModel := doc.Types[componentID(name)].(*ir.Model)
		require.True(t, isModel, name)
		property, ok := propertyByWire(model, "f")
		require.True(t, ok, "%s holds f", name)
		out.WriteString(strings.TrimSpace(name + ": " + memberText(t, doc, componentID(name)) + " " + boundsText(t, doc, property.Type.Target)))
		out.WriteString("\n")
	}
	reports := 0
	for _, diagnostic := range diags {
		if strings.Contains(diagnostic.Message, `declarations of field "f"`) {
			reports++
		}
	}
	fmt.Fprintf(&out, "reports: %d", reports)
	return entryOfM.ReplaceAllString(out.String(), "M/allOf")
}

// entryOrders returns every order n entries are written in, sorted.
func entryOrders(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	var out [][]int
	for _, order := range entryOrders(n - 1) {
		for at := range n {
			out = append(out, slices.Insert(slices.Clone(order), at, n-1))
		}
	}
	slices.SortFunc(out, slices.Compare[[]int])
	return out
}

// TestAllOfFold_NotesOfAFoldedMemberDecideItsTypeAsTheirNodesHoldThem pins what
// a member several declarations of a composition decide folds to in every order
// of the composing model's entries: each declaration is judged with its notes on
// the node its schema builds, brought from another model or not, so no bound,
// type, note or example is lost to a note's home. The last row's two notes are
// of brought declarations whose types compose, and a reference names one.
func TestAllOfFold_NotesOfAFoldedMemberDecideItsTypeAsTheirNodesHoldThem(t *testing.T) {
	t.Parallel()
	const str = "{type: string}"
	const strs = "type: array, items: " + str
	const nonEmpty = "type: array, items: {type: string, minLength: 1}"
	const some = "type: array, items: {type: string, enum: [a, b]}"
	entry := func(schema string) string { return "{properties: {f: {" + schema + "}}}" }
	naming := func(pointer string) string {
		return "{properties: {f: {$ref: '#/components/schemas/" + pointer + "'}}}"
	}
	ref := func(name string) string { return "{$ref: '#/components/schemas/" + name + "'}" }
	chain := "    A: " + entry(nonEmpty+", externalDocs: {url: 'https://e.test/a'}") + "\n" +
		"    B: {allOf: [" + ref("A") + ", " + entry(strs+", maxItems: 3, title: B") + "]}\n" +
		"    SC: " + entry(strs+", maxItems: 3") + "\n" +
		"    C: {allOf: [" + ref("SC") + ", " + entry(strs+", externalDocs: {url: 'https://e.test/c'}") + ", " + ref("B") + "]}\n"
	onlyB := entry("type: array, items: {type: string, enum: [b]}")
	const refused = "    A: {properties: {f: {type: string, enum: [a, b], description: DA}}}\n" +
		"    B: {allOf: [{properties: {f: {type: string, enum: [a, b, c], description: DB}}}, {$ref: '#/components/schemas/A'}]}\n"
	const objects = "    A: {properties: {f: {type: object, properties: {p: {type: string}}, description: DA}}}\n" +
		"    B: {properties: {f: {type: object, properties: {q: {type: string}}, description: DB}}}\n"
	const strMap = "type: object, additionalProperties: {type: string, minLength: 1}"
	maps := "    A: {allOf: [" + entry(strMap+", title: A") + ", " + entry(strMap) + "]}\n" +
		"    B: {allOf: [" + naming("A/allOf/0/properties/f") + "]}\n" +
		"    SC: " + entry("type: object, additionalProperties: {}, minProperties: 1, title: SC") + "\n" +
		"    C: {allOf: [" + ref("SC") + ", " + ref("B") + ", " + entry("type: object, additionalProperties: "+str) + "]}\n"
	settled := []string{ref("B"), entry("type: string, enum: [b, c]"), entry("type: string")}
	for _, tc := range []struct {
		name, version string
		rest          string   // the components beside M
		entries       []string // M's, in one order
		models        []string // whose f is read
		want          []string // in each order, or alike in all
	}{
		{"a bound beside a list a reference names", "3.1.0", "    A: " + entry(nonEmpty) + "\n",
			[]string{entry(nonEmpty + ", maxItems: 3"), naming("A/properties/f"), ref("A")}, []string{"M"},
			[]string{"M: composed/M/allOf/properties/f list {\"maxItems\":3} of {\"minLength\":1}\nreports: 0"}},
		{"a bound beside a list a reference names, at 3.0", "3.0.3", "    A: " + entry(nonEmpty) + "\n",
			[]string{entry(nonEmpty + ", maxItems: 3"), naming("A/properties/f"), ref("A")}, []string{"M"},
			[]string{"M: composed/M/allOf/properties/f list {\"maxItems\":3} of {\"minLength\":1}\nreports: 0"}},
		{"two bounds, one a component holds", "3.1.0", "    A: " + entry(some+", maxItems: 9") + "\n    CMP: {" + some + ", minItems: 1}\n",
			[]string{naming("A/properties/f"), naming("CMP"), ref("A")}, []string{"M"},
			[]string{"M: composed/M/allOf/properties/f list {\"minItems\":1,\"maxItems\":9} of enum a b\nreports: 0"}},
		{"a description a brought member settled", "3.1.0", refused,
			settled, []string{"B", "M"},
			[]string{"B: A/properties/f enum a b [description DA] | verbatim B/allOf/0/properties/f enum a b\nM: composed/M/allOf/properties/f enum b [description DA] | verbatim B/allOf/0/properties/f enum b\nreports: 1"}},
		{"a description a reference names", "3.1.0", refused + "    U: " + naming("B/allOf/0/properties/f") + "\n",
			settled, []string{"B", "M"},
			[]string{"B: A/properties/f enum a b [description DA] | verbatim B/allOf/0/properties/f enum a b\nM: composed/M/allOf/properties/f enum b [description DA] | verbatim B/allOf/0/properties/f enum b\nreports: 1"}},
		{"an enum beside a member two models settled", "3.1.0", chain,
			[]string{onlyB, naming("C/allOf/1/properties/f"), ref("C")}, []string{"C", "M"},
			[]string{"C: composed/C/allOf/1/properties/f list [link https://e.test/c] {\"maxItems\":3}\nM: [link https://e.test/c] composed/M/allOf/properties/f list [link https://e.test/c] {\"maxItems\":3} of enum b\nreports: 2"}},
		{"an enum beside a member two models settled, its position named", "3.1.0", chain + "    U: " + naming("C/allOf/1/properties/f") + "\n",
			[]string{onlyB, ref("C")}, []string{"C", "M"},
			[]string{"C: composed/C/allOf/1/properties/f list [link https://e.test/c] {\"maxItems\":3}\nM: composed/M/allOf/properties/f list [link https://e.test/c] {\"maxItems\":3} of enum b\nreports: 2"}},
		{"a bound of a map a chain settles", "3.1.0", maps,
			[]string{ref("C"), naming("A/allOf/1/properties/f")}, []string{"C", "M"},
			[]string{"C: [title A] composed/C/allOf/2/properties/f map [title SC] {\"uniqueItems\":false,\"minProperties\":1}\nM: [title A] composed/M/allOf/properties/f map [title SC] {\"uniqueItems\":false,\"minProperties\":1} of {\"minLength\":1}\nreports: 2"}},
		{"a null example every declaration admits", "3.1.0", "    A: " + entry("oneOf: [{type: [string, 'null']}, {type: integer}]") + "\n",
			[]string{ref("A"), entry("enum: [a, null], example: null")}, []string{"M"},
			[]string{"M: M/allOf/properties/f enum a [example {\"kind\":\"null\"}] enum a\nreports: 0"}},
		{"two brought descriptions of objects", "3.1.0", objects,
			[]string{ref("B"), ref("A")}, []string{"A", "B", "M"},
			[]string{
				"A: A/properties/f model [description DA]\nB: B/properties/f model [description DB]\nM: composed/M/allOf/properties/f model of (B/properties/f model [description DB]; A/properties/f model [description DA])\nreports: 0",
				"A: A/properties/f model [description DA]\nB: B/properties/f model [description DB]\nM: composed/M/allOf/properties/f model of (A/properties/f model [description DA]; B/properties/f model [description DB])\nreports: 0",
			}},
		{"two brought descriptions of objects, one a reference names", "3.1.0", objects + "    U: " + naming("A/properties/f") + "\n",
			[]string{ref("B"), ref("A")}, []string{"A", "B", "M"},
			[]string{
				"A: A/properties/f model [description DA]\nB: B/properties/f model [description DB]\nM: composed/M/allOf/properties/f model of (B/properties/f model [description DB]; A/properties/f model [description DA])\nreports: 0",
				"A: A/properties/f model [description DA]\nB: B/properties/f model [description DB]\nM: composed/M/allOf/properties/f model of (A/properties/f model [description DA]; B/properties/f model [description DB])\nreports: 0",
			}},
	} {
		orders := entryOrders(len(tc.entries))
		for i, order := range orders {
			entries := make([]string, len(order))
			for at, k := range order {
				entries[at] = tc.entries[k]
			}
			spec := tc.rest + "    M: {allOf: [" + strings.Join(entries, ", ") + "]}\n"
			doc, diags := parseVerified(t, openapitest.ComponentSpecVer(tc.version, spec))
			want := tc.want[0]
			if len(tc.want) > 1 {
				require.Len(t, tc.want, len(orders), tc.name)
				want = tc.want[i]
			}
			assert.Equal(t, want, foldedText(t, doc, diags, tc.models...), "%s, entries in order %v", tc.name, order)
		}
	}
}

// TestAllOfFold_ADeprecationAFoldTakesIsItsOwn pins that a deprecation a fold
// takes of a declaration is a copy: a message naming a member beside it is read
// into each model's by that model's members, so a base two models compose holds
// its own and each of theirs names its own, whichever sorts first; and the node
// a fold builds of one node's deprecation and another's replacement key resolves
// the key it keeps, the node it took the deprecation from its own.
func TestAllOfFold_ADeprecationAFoldTakesIsItsOwn(t *testing.T) {
	t.Parallel()
	const reason = "{type: string, deprecated: true, x-deprecated-reason: 'Use `g` instead.'}"
	for _, names := range [][2]string{{"Cat", "Dog"}, {"Aa", "Zz"}} {
		src := "    Base: {properties: {f: " + reason + ", g: {type: array, items: {type: string}}}}\n"
		for i, name := range names {
			src += fmt.Sprintf("    %s: {allOf: [{$ref: '#/components/schemas/Base'}, {properties: {f: {type: string}, "+
				"g: {type: array, items: {type: string}, maxItems: %d}}}]}\n", name, 5+i)
		}
		doc, _ := parseVerified(t, openapitest.ComponentSpec(src))
		for _, name := range []string{"Base", names[0], names[1]} {
			model, isModel := doc.Types[componentID(name)].(*ir.Model)
			require.True(t, isModel, name)
			field, ok := propertyByWire(model, "f")
			require.True(t, ok, name)
			sibling, ok := propertyByWire(model, "g")
			require.True(t, ok, name)
			require.NotNil(t, field.Deprecation, name)
			assert.Equal(t, []ir.EntityRef{{Property: sibling.ID}}, field.Deprecation.ReplacedBy, "%v: %s", names, name)
		}
	}

	doc, _ := replacementsOf(t, "    New: {type: string}\n    Newer: {type: string}\n    M: {allOf: ["+
		"{properties: {f: {type: string, enum: [a, b], deprecated: true, x-replaced-by: New}}}, "+
		"{properties: {f: {type: string, enum: [a, c], x-replaced-by: Newer}}}, "+
		"{properties: {f: {type: string, enum: [a, d]}}}]}\n")
	assert.Equal(t, "composed/M/allOf/1/properties/f enum a [deprecated, replaced by type/openapi/components/schemas/Newer] | verbatim M/allOf/0/properties/f",
		memberText(t, doc, componentID("M")))
	assert.Equal(t, "M/allOf/0/properties/f enum a b [deprecated, replaced by type/openapi/components/schemas/New]",
		typeText(t, doc, "type/anonymous/components/schemas/M/allOf/0/properties/f"))
}

// TestAllOfFold_AMemberSeveralModelsDeclareReadsNoReplacementOffItsNode pins a
// policy mapping deprecation.replacedBy at a member a model's entries bring
// several declarations of: each declaration's deprecation stays on its node
// beside its key, so the node holds the replacement, and the member, at its own
// declaration or at none, reads no node's key
// (docs/compilers/openapi.md#extension-promotion).
func TestAllOfFold_AMemberSeveralModelsDeclareReadsNoReplacementOffItsNode(t *testing.T) {
	t.Parallel()
	const brought = "    A: {properties: {f: {type: string, enum: [a, b], x-replaced-by: New}}}\n" +
		"    B: {properties: {f: {type: string, enum: [b, c]}}}\n    C: {properties: {f: {type: string, deprecated: true}}}\n"
	entries := "{$ref: '#/components/schemas/A'}, {$ref: '#/components/schemas/B'}, {$ref: '#/components/schemas/C'}"
	for _, tc := range []struct{ name, spec, want string }{
		{"a key a built node holds", brought + "    M: {allOf: [" + entries + "]}\n", "[deprecated] composed/M/allOf/properties/f enum b"},
		{"a deprecation its node holds", "    A: {properties: {f: {type: string, enum: [a, b], deprecated: true, x-replaced-by: New}}}\n" +
			"    M: {allOf: [{$ref: '#/components/schemas/A'}], properties: {f: {type: string}}}\n",
			"A/properties/f enum a b [deprecated, replaced by type/openapi/components/schemas/New]"},
	} {
		doc, _ := replacementsOf(t, "    New: {type: string}\n"+tc.spec)
		assert.Equal(t, tc.want, memberText(t, doc, componentID("M")), tc.name)
	}
}

// replacementsOf compiles a document of the component schemas, reading a
// replacement from x-replaced-by.
func replacementsOf(t *testing.T, schemas string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	return compileVerified(t, openapitest.SourceOf(openapitest.ComponentSpec(schemas)), openapi.Options{Promotions: openapi.ExtensionPromotions{
		Targets: map[openapi.ExtensionTarget][]openapi.ExtensionSource{openapi.TargetDeprecationReplacedBy: {{Key: "x-replaced-by"}}},
	}})
}
