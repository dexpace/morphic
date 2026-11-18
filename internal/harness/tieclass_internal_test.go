// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// unnamed is a report naming no node at pointer, as reportsOf reads one of
// a source without reversed sequences.
func unnamed(code, pointer string) report {
	return reportNaming(code, pointer, "")
}

// reportNaming is a report at pointer naming node, as reportsOf reads one of
// a source without reversed sequences.
func reportNaming(code, pointer, node string) report {
	d := diagnostic(ir.SeverityWarning, code, pointer, node)
	return report{key: diagnosticKey(d), erased: diagnosticKey(d), pointer: pointer, node: node, raw: d}
}

// bothWays compiles src as the oracle does, as re-encoded and reversed, and
// returns both documents and what was reversed.
func bothWays(t *testing.T, src string) (first, second *ir.Document, perm permutation) {
	t.Helper()
	baseline, why := reencodeMappings([]byte(src))
	require.Empty(t, why)
	reversed, perm, why := reverseSequences([]byte(src))
	require.Empty(t, why)
	first, _, err := compile(context.Background(), "spec.yaml", baseline, passes())
	require.NoError(t, err)
	second, _, err = compile(context.Background(), "spec.yaml", reversed, passes())
	require.NoError(t, err)
	require.NotNil(t, first, "the fixture must compile as written")
	require.NotNil(t, second, "the fixture must compile reversed")
	return first, second, perm
}

// tiedOf returns the members src's two compiles tie, given the reports each
// makes, a as written and b reversed.
func tiedOf(t *testing.T, src string, a, b []report) []string {
	t.Helper()
	first, second, perm := bothWays(t, src)
	got, why := tieClassesOf(&formReader{doc: first, seqs: perm.written}, &formReader{doc: second, seqs: perm.moved}, a, b)
	require.Empty(t, why)
	return got.members
}

// TestTieClassesOf_TiesAMemberTheDocumentsShowATieDecided pins which members
// fixtureSwap's two compiles tie, given the reports naming no node each makes.
// p folds alpha's and beta's declarations, takes beta's as written and alpha's
// reversed, and is tied where each order reports at the one it lost, whether
// that report moves or two ties' reports sit at both in either order. q is
// declared alike, so whatever moves between its declarations ties nothing.
func TestTieClassesOf_TiesAMemberTheDocumentsShowATieDecided(t *testing.T) {
	const s = "/components/schemas"
	alphaP, betaP := unnamed("x/swap", s+"/alpha/properties/p"), unnamed("x/swap", s+"/beta/properties/p")
	alphaQ, betaQ := unnamed("x/swap", s+"/alpha/properties/q"), unnamed("x/swap", s+"/beta/properties/q")
	bothP := []string{"p/composed/components/schemas/AB/allOf/properties/p", "p/composed/components/schemas/AB2/allOf/properties/p"}
	for _, tc := range []struct {
		name    string
		a, b    []report
		members []string
	}{
		{name: "a report moving to the declaration p takes as written", a: []report{alphaP}, b: []report{betaP}, members: bothP},
		{name: "a report at each of p's declarations in either order", a: []report{alphaP, betaP}, b: []report{alphaP, betaP}, members: bothP},
		{name: "a report at the declaration p takes in each order", a: []report{betaP}, b: []report{alphaP}},
		{name: "a report at p's declarations naming another member", a: []report{reportNaming("x/swap", s+"/alpha/properties/p", "p/x")}, b: []report{betaP}},
		{name: "a report moving between q's declarations", a: []report{alphaQ}, b: []report{betaQ}},
		{name: "a report at p's declarations as written alone", a: []report{alphaP}, b: []report{betaQ}},
		{name: "no report reversed", a: []report{alphaP}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.members, tiedOf(t, fixtureSwap, tc.a, tc.b))
		})
	}

	first, second, perm := bothWays(t, fixtureSwap)
	reversed := &formReader{doc: second, seqs: perm.moved}
	got, _ := tieClassesOf(&formReader{doc: first, seqs: perm.written}, reversed, []report{alphaP}, []report{betaP})
	least := s + "/AB/allOf/properties/p"
	assert.Equal(t, map[string]string{least: least, s + "/AB2/allOf/properties/p": least,
		s + "/alpha/properties/p": least, s + "/beta/properties/p": least}, got.least,
		"p's declarations, its two models' among them, are one class")
	assert.Equal(t, bothP, got.at[s+"/alpha/properties/p"], "a declaration maps to the members folding it")
	none, why := tieClassesOf(&formReader{seqs: perm.written}, reversed, []report{alphaP}, []report{betaP})
	require.Empty(t, why)
	assert.Empty(t, none.members, "a refused compile shows no tie")
}

// TestTieClassesOf_TiesWhatEachEvidenceShows pins each evidence a member is
// tied on alone, and the same input with it taken away. In three, a third
// declaration gives p its description, so p takes what neither of the others
// says: a report moving between them ties it, unless it sits where p takes
// the type. In desctrade, two families trade declarations, one giving a
// description both keep, and the families take the same members in either
// order, unless one's member says something else.
func TestTieClassesOf_TiesWhatEachEvidenceShows(t *testing.T) {
	const s = "/components/schemas"
	a, b := unnamed("x/swap", s+"/A/properties/p"), unnamed("x/swap", s+"/B/properties/p")
	m := []string{"p/composed/components/schemas/M1/allOf/properties/p", "p/composed/components/schemas/M2/allOf/properties/p"}

	assert.Equal(t, m, tiedOf(t, fixtureThree, []report{a}, []report{b}), "a report moving from the declaration that lost")
	assert.Empty(t, tiedOf(t, fixtureThree, []report{b}, []report{a}), "a report moving to the declaration whose type p takes")

	traded := append(slices.Clone(m), "p/composed/components/schemas/N1/allOf/properties/p", "p/composed/components/schemas/N2/allOf/properties/p")
	assert.Equal(t, traded, tiedOf(t, fixtureDescTrade, []report{a, b}, []report{a, b}), "two families trading declarations")

	first, second, perm := bothWays(t, fixtureDescTrade)
	model, ok := second.Types["t/openapi/components/schemas/M1"].(*ir.Model)
	require.True(t, ok, "the fixture must lower M1 to a model")
	model.Properties[0].Docs.Description = "reversed"
	got, why := tieClassesOf(&formReader{doc: first, seqs: perm.written}, &formReader{doc: second, seqs: perm.moved},
		[]report{a, b}, []report{a, b})
	require.Empty(t, why)
	assert.Empty(t, got.members, "a family whose outcomes differ between the orders")
}

// TestTiedMembers_NeedsReportsAtTwoDeclarations pins that a member whose
// reports change where it takes no declaration's type is tied only where they
// sit at two of its declarations across the orders, as a tie moves its report
// from the declaration that lost: one changing at a single declaration ties
// nothing.
func TestTiedMembers_NeedsReportsAtTwoDeclarations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		reversed report
		tied     []string
	}{
		{name: "a report moving to the other declaration", reversed: reportNaming("x/swap", "/b", "m"), tied: []string{"m"}},
		{name: "a report naming the member at the same declaration", reversed: reportNaming("x/swap", "/a", "m")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			members := map[string]folds{"m": {at: []string{"/a", "/b"}, family: []string{"/a", "/b"}}}
			asWritten := map[string][]memberForm{"m": {{model: "M", shape: "as written"}}}
			asReversed := map[string][]memberForm{"m": {{model: "M", shape: "reversed"}}}
			written := map[string][]report{"m": {unnamed("x/swap", "/a")}}
			reversed := map[string][]report{"m": {tc.reversed}}

			assert.Equal(t, tc.tied, tiedMembers(members, asWritten, asReversed, written, reversed, nil))
		})
	}
}

// TestTaken_IsWhatEveryFormTakes pins the declarations a member takes where
// several models hold it: those every one of its forms takes, and none where
// it has no form.
func TestTaken_IsWhatEveryFormTakes(t *testing.T) {
	t.Parallel()
	assert.Nil(t, taken(nil))
	assert.Equal(t, []string{"/b"}, taken([]memberForm{{takes: []string{"/a", "/b"}}, {takes: []string{"/b", "/c"}}}))
}

// TestTraded_NeedsAnotherDeclarationTakenInEachOrder pins that a member taking
// one declaration in both orders is no trade, whatever reports sit beside it.
func TestTraded_NeedsAnotherDeclarationTakenInEachOrder(t *testing.T) {
	t.Parallel()
	at := [2][]report{{unnamed("x/swap", "/a"), unnamed("x/swap", "/b")}, {unnamed("x/swap", "/a"), unnamed("x/swap", "/b")}}
	took := []memberForm{{takes: []string{"/a"}}}
	assert.True(t, sides{forms: [2][]memberForm{took, {{takes: []string{"/b"}}}}, reports: at}.traded(nil))
	assert.False(t, sides{forms: [2][]memberForm{took, {{takes: []string{"/a", "/b"}}}}, reports: at}.traded(nil))
}

// TestTieClasses_JoinKeepsEachClassAtItsLeastPointer pins the classes joined
// declarations make: one member's declarations are one class, two classes
// sharing a declaration are one, transitively, whichever was joined first,
// and each declaration maps to its class's least pointer.
func TestTieClasses_JoinKeepsEachClassAtItsLeastPointer(t *testing.T) {
	t.Parallel()
	c := tieClasses{least: map[string]string{}}

	c.join([]string{"/c", "/d"})
	c.join([]string{"/e", "/f"})
	c.join([]string{"/b", "/e"})
	c.join([]string{"/d", "/f"})
	c.join([]string{"/x", "/y"})
	c.settle()

	assert.Equal(t, map[string]string{"/b": "/b", "/c": "/b", "/d": "/b", "/e": "/b", "/f": "/b", "/x": "/x", "/y": "/x"}, c.least)
}

// TestUnmatched_DropsWhatTheOtherCompileSaysWithinAClass pins what a class
// forgives: a report at its declarations, naming no node or a member folding
// the declaration, that one of the other compile's reports matches by its key
// at the class's least pointer, however many there are. A report naming
// another node, one of another key and one at no class's declaration stay.
func TestUnmatched_DropsWhatTheOtherCompileSaysWithinAClass(t *testing.T) {
	t.Parallel()
	c := tieClasses{least: map[string]string{"/a": "/a", "/b": "/a"}, at: map[string][]string{"/a": {"p/m"}, "/b": {"p/m"}}}
	other := reportNaming("x/swap", "/a", "p/x")
	rs := []report{unnamed("x/swap", "/b"), unnamed("x/swap", "/b"), reportNaming("x/swap", "/a", "p/m"), other,
		unnamed("x/swap", "/c"), unnamed("x/other", "/b")}

	got := c.unmatched(rs, []report{unnamed("x/swap", "/a")}, nil)

	assert.Equal(t, []report{other, unnamed("x/swap", "/c"), unnamed("x/other", "/b")}, got)
	assert.Len(t, rs, 6, "the reports given are left as they were")
}

// generatedRedeclarations is how many documents
// TestSequenceOrderInvariant_PassesGeneratedRedeclarations writes, and
// generatedSeed the seed it writes them from.
const (
	generatedRedeclarations = 150
	generatedSeed           = 1996
)

// TestSequenceOrderInvariant_PassesGeneratedRedeclarations holds the oracle to
// finding nothing in documents several models redeclare a member in, through
// components composed in random orders, aliases, $ref and allOf chains,
// inline and nested entries and models composing models (redeclarations). An
// object or array type is one shared node, so no two declarations say the same
// through two nodes, and the only order dependence they hold is the tie the
// compiler reports. It also holds that most of them draw a conflict, so the
// documents reach the tie reading.
func TestSequenceOrderInvariant_PassesGeneratedRedeclarations(t *testing.T) {
	rng := rand.New(rand.NewPCG(generatedSeed, generatedSeed))
	conflicting := 0
	for i := range generatedRedeclarations {
		data, err := json.Marshal(redeclarations(rng, i), json.Deterministic(true))
		require.NoError(t, err)
		_, diags, err := compile(context.Background(), "spec.json", data, passes())
		require.NoError(t, err)
		if len(diagnosticsAt(diags, "openapi/conflicting-redeclaration")) > 0 {
			conflicting++
		}

		got := sequenceOrderInvariant(context.Background(), "spec.json", data, true)

		assert.Equal(t, passed(OracleSequenceOrder), got, "document %d: %s", i, data)
	}
	assert.Greater(t, conflicting, generatedRedeclarations/2, "most documents must draw a conflict between declarations")
}

// redeclarations writes the i-th generated document: two to five components
// declaring p, of a random kind each, some through an alias and some with a q
// as well, and two to eight models composing two to four entries each.
func redeclarations(rng *rand.Rand, i int) map[string]any {
	schemas := map[string]any{
		"Obj":  map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}},
		"List": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}
	kinds := make([]string, 2+rng.IntN(4))
	for j := range kinds {
		kinds[j] = []string{"string", "integer", "boolean", "object", "array"}[rng.IntN(5)]
	}
	if rng.Float64() < 0.3 {
		kinds[rng.IntN(len(kinds))] = kinds[rng.IntN(len(kinds))]
	}
	components := make([]string, len(kinds))
	for j, kind := range kinds {
		p := kindOf(kind)
		if rng.Float64() < 0.2 {
			schemas[fmt.Sprintf("Alias%d", j)] = p
			p = refTo(fmt.Sprintf("Alias%d", j))
		}
		properties := map[string]any{"p": p}
		if rng.Float64() < 0.4 {
			properties["q"] = map[string]any{"type": []string{"string", "string", "integer"}[rng.IntN(3)]}
		}
		components[j] = fmt.Sprintf("C%d", j)
		schemas[components[j]] = map[string]any{"type": "object", "properties": properties}
	}
	models := make([]string, 0, 8)
	for m := range 2 + rng.IntN(7) {
		entries := make([]any, 2+rng.IntN(3))
		for e := range entries {
			entries[e] = entryOf(rng, schemas, components)
		}
		if len(models) > 0 && rng.Float64() < 0.2 {
			entries[rng.IntN(len(entries))] = refTo(models[rng.IntN(len(models))])
		}
		model := map[string]any{"allOf": entries}
		if rng.Float64() < 0.1 {
			model["properties"] = map[string]any{"r": map[string]any{"type": "string"}}
		}
		if rng.Float64() < 0.1 {
			names := []string{"p", "q", "r"}
			rng.Shuffle(len(names), func(a, b int) { names[a], names[b] = names[b], names[a] })
			model["required"] = names[:2]
		}
		models = append(models, fmt.Sprintf("M%d", m))
		schemas[models[m]] = model
	}
	return map[string]any{"openapi": "3.0.3", "info": map[string]any{"title": fmt.Sprintf("g%d", i), "version": "1"},
		"paths": map[string]any{}, "components": map[string]any{"schemas": schemas}}
}

// entryOf writes one allOf entry: a component, through a $ref chain or an
// allOf chain it adds to schemas or directly, an inline schema redeclaring p,
// or a nested allOf of two components.
func entryOf(rng *rand.Rand, schemas map[string]any, components []string) map[string]any {
	c := components[rng.IntN(len(components))]
	switch r := rng.Float64(); {
	case r < 0.15:
		schemas["Via"+c] = refTo(c)
		return refTo("Via" + c)
	case r < 0.25:
		schemas["Wrap"+c] = map[string]any{"allOf": []any{refTo(c)}}
		return refTo("Wrap" + c)
	case r < 0.35:
		kind := []string{"string", "integer", "boolean", "object", "array"}[rng.IntN(5)]
		return map[string]any{"type": "object", "properties": map[string]any{"p": kindOf(kind)}}
	case r < 0.45:
		pair := rng.Perm(len(components)) // two at least
		return map[string]any{"allOf": []any{refTo(components[pair[0]]), refTo(components[pair[1]])}}
	default:
		return refTo(c)
	}
}

// kindOf is a schema of kind, an object or an array naming the one shared
// node of its kind.
func kindOf(kind string) map[string]any {
	switch kind {
	case "object":
		return refTo("Obj")
	case "array":
		return refTo("List")
	default:
		return map[string]any{"type": kind}
	}
}

// refTo is a $ref to the component name.
func refTo(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
