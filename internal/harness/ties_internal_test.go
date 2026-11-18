// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// conflictAt is a report at lost saying it kept what the declaration at each
// of kept says and decided each of members, as reportsOf reads one of a
// source whose reversed sequences are seqs.
func conflictAt(seqs sequences, lost string, kept []string, members ...string) report {
	d := diagnostic(ir.SeverityWarning, "x/conflict", lost, "")
	for _, at := range kept {
		d.Related = append(d.Related, ir.Related{Role: ir.RelatedKept, Provenance: ir.Provenance{Pointer: jsontext.Pointer(at)}})
	}
	for _, member := range members {
		d.Related = append(d.Related, ir.Related{Role: ir.RelatedDecided, Provenance: ir.Provenance{Source: ir.NoSource, Node: member}})
	}
	return reportsOf([]ir.Diagnostic{d}, nil, seqs)[0]
}

// decisionsOf is the decisions facts state, in order.
func decisionsOf(facts []fact) []decision {
	out := make([]decision, len(facts))
	for i, f := range facts {
		out[i] = f.decision
	}
	return out
}

// TestFactsOf_ReadsAFactPerMemberAReportDecided pins what states a fact: a
// report naming one kept place and the members it decided, one fact for each,
// its member and both places with a reversed sequence's index erased. A
// report naming no kept place, two, no member, or an entry of a role the
// reading does not know, states none and stays a report.
func TestFactsOf_ReadsAFactPerMemberAReportDecided(t *testing.T) {
	t.Parallel()
	seqs := sequences{"/M/allOf": 2}
	plain := conflictAt(seqs, "/A/p", nil)
	twoKept := conflictAt(seqs, "/A/p", []string{"/B/p", "/C/p"}, "p/M/p")
	noMember := conflictAt(seqs, "/A/p", []string{"/B/p"})
	unknown := conflictAt(seqs, "/A/p", nil)
	unknown.rebased.Related = []ir.Related{{Role: "other", Provenance: ir.Provenance{Pointer: "/B/p"}}}
	pair := conflictAt(seqs, "/M/allOf/0/properties/p", []string{"/B/p"}, "p/M/allOf/1/p", "p/N/p")

	facts, rest := factsOf([]report{plain, twoKept, pair, noMember, unknown}, seqs)

	assert.Equal(t, []report{plain, twoKept, noMember, unknown}, rest)
	assert.Equal(t, []decision{
		{member: "p/M/allOf/-/p", lost: "/M/allOf/-/properties/p\x000\x000", kept: "/B/p\x000\x000"},
		{member: "p/N/p", lost: "/M/allOf/-/properties/p\x000\x000", kept: "/B/p\x000\x000"},
	}, decisionsOf(facts))
	assert.Equal(t, "/M/allOf/0/properties/p", pointerOf(facts[0].lostAt), "the place as written tells one entry from another")
	assert.Equal(t, pair, facts[1].report)
}

// TestTiedBy_TiesAMemberBothOrdersDecideTheOtherWayRound pins the one thing a
// tie is: a member one order says lost a place and kept another, and the
// other order says lost that other and kept the first.
func TestTiedBy_TiesAMemberBothOrdersDecideTheOtherWayRound(t *testing.T) {
	t.Parallel()
	seqs := sequences{"/M/allOf": 3, "/X/allOf": 2}
	facts := func(rs ...report) []fact {
		out, rest := factsOf(rs, seqs)
		require.Empty(t, rest)
		return out
	}
	for name, tc := range map[string]struct {
		written, reversed []fact
		want              []string
	}{
		"mirrored": {
			written:  facts(conflictAt(seqs, "/A/p", []string{"/B/p"}, "p/M/p", "p/N/p")),
			reversed: facts(conflictAt(seqs, "/B/p", []string{"/A/p"}, "p/N/p", "p/M/p")),
			want:     []string{"p/M/p", "p/N/p"},
		},
		"the same way round in both orders": {
			written:  facts(conflictAt(seqs, "/A/p", []string{"/B/p"}, "p/M/p")),
			reversed: facts(conflictAt(seqs, "/A/p", []string{"/B/p"}, "p/M/p")),
		},
		"mirrored for another member": {
			written:  facts(conflictAt(seqs, "/A/p", []string{"/B/p"}, "p/M/p")),
			reversed: facts(conflictAt(seqs, "/B/p", []string{"/A/p"}, "p/N/p")),
		},
		"another place kept": {
			written:  facts(conflictAt(seqs, "/A/p", []string{"/B/p"}, "p/M/p")),
			reversed: facts(conflictAt(seqs, "/B/p", []string{"/C/p"}, "p/M/p")),
		},
		"one order alone": {
			written: facts(conflictAt(seqs, "/A/p", []string{"/B/p"}, "p/M/p")),
		},
		"a member another model brings, named by its first declaration in either order": {
			written:  facts(conflictAt(seqs, "/X/allOf/0/properties/p", []string{"/B/p"}, "p/M/p")),
			reversed: facts(conflictAt(seqs, "/B/p", []string{"/X/allOf/1/properties/p"}, "p/M/p")),
			want:     []string{"p/M/p"},
		},
		"two entries of one allOf, mirrored as written": {
			written:  facts(conflictAt(seqs, "/M/allOf/0/properties/p", []string{"/M/allOf/1/properties/p"}, "p/M/p")),
			reversed: facts(conflictAt(seqs, "/M/allOf/1/properties/p", []string{"/M/allOf/0/properties/p"}, "p/M/p")),
			want:     []string{"p/M/p"},
		},
		"two entries of one allOf, the same one lost in both orders": {
			written:  facts(conflictAt(seqs, "/M/allOf/0/properties/p", []string{"/M/allOf/2/properties/p"}, "p/M/p")),
			reversed: facts(conflictAt(seqs, "/M/allOf/0/properties/p", []string{"/M/allOf/1/properties/p"}, "p/M/p")),
		},
	} {
		assert.Equal(t, tc.want, tiedBy(tc.written, tc.reversed), name)
	}
}

// TestUnpairedFacts_PairsAnUntiedMembersFactsByWhatLost pins what of the
// facts is a difference: a fact of a member no tie decided that the other
// order does not state, by its member and the place that lost, one to one. The
// place kept is not compared there, a tied member's facts are none, and a
// report stating two unpaired facts is one report.
func TestUnpairedFacts_PairsAnUntiedMembersFactsByWhatLost(t *testing.T) {
	t.Parallel()
	seqs := sequences{}
	lone := conflictAt(seqs, "/A/p", []string{"/B/p"}, "p/M/p", "p/N/p", "p/T/p")
	other := conflictAt(seqs, "/A/p", []string{"/C/p"}, "p/M/p")
	twice := conflictAt(seqs, "/A/q", []string{"/B/q"}, "p/M/q")
	written, _ := factsOf([]report{lone, twice, twice}, seqs)
	reversed, _ := factsOf([]report{other, twice}, seqs)

	left, right := unpairedFacts(written, reversed, []string{"p/T/p"})

	assert.Equal(t, []report{lone, twice}, left, "N's fact has no pair, T is tied, M's pairs though it kept another place, and q is said once more")
	assert.Empty(t, right)

	left, right = unpairedFacts(written, written, nil)
	assert.Empty(t, left)
	assert.Empty(t, right)

	info := conflictAt(seqs, "/A/q", []string{"/B/q"}, "p/M/q")
	info.rebased.Severity = ir.SeverityInfo
	restated, _ := factsOf([]report{info}, seqs)
	once, _ := factsOf([]report{twice}, seqs)
	left, right = unpairedFacts(once, restated, nil)
	assert.Equal(t, []report{twice}, left, "a report saying the same of a member at another severity is no pair")
	assert.Equal(t, []report{info}, right)
}

// TestHeldBy_ReadsWhatAMemberTakesAndWhatItsDeclarationsSay pins the reading
// the winner check rests on: a member its model declares over declarations it
// overrides, by its erased ID, with its type and each overridden one's by
// pointer; no member a model only inherits or declares alone, no node that is
// no model, and nothing of a document that did not compile.
func TestHeldBy_ReadsWhatAMemberTakesAndWhatItsDeclarationsSay(t *testing.T) {
	t.Parallel()
	first, _, perm := bothWays(t, fixtureSwap)

	got := heldBy(first, perm.written)

	assert.Equal(t, map[string]held{
		"p/composed/components/schemas/AB/allOf/properties/p": {takes: "t/prim/integer false", said: map[string]string{
			"/components/schemas/alpha/properties/p": "t/prim/string false", "/components/schemas/beta/properties/p": "t/prim/integer false",
		}},
		"p/composed/components/schemas/AB2/allOf/properties/p": {takes: "t/prim/integer false", said: map[string]string{
			"/components/schemas/alpha/properties/p": "t/prim/string false", "/components/schemas/beta/properties/p": "t/prim/integer false",
		}},
		"p/composed/components/schemas/AB/allOf/properties/q": {takes: "t/prim/string false", said: map[string]string{
			"/components/schemas/alpha/properties/q": "t/prim/string false", "/components/schemas/beta/properties/q": "t/prim/string false",
		}},
		"p/composed/components/schemas/AB2/allOf/properties/q": {takes: "t/prim/string false", said: map[string]string{
			"/components/schemas/alpha/properties/q": "t/prim/string false", "/components/schemas/beta/properties/q": "t/prim/string false",
		}},
	}, got)
	assert.Empty(t, heldBy(nil, perm.written))
}

// TestContradicted_FindsATiedMemberTakingWhatItsReportsSayLost pins the winner
// check's one finding and each thing it needs before it speaks: a tied member,
// two declarations its flattening overrides at the report's two places, naming
// different types, the one that lost taken as written and the one kept taken
// reversed.
func TestContradicted_FindsATiedMemberTakingWhatItsReportsSayLost(t *testing.T) {
	t.Parallel()
	const (
		member = "p/composed/components/schemas/AB/allOf/properties/p"
		alpha  = "/components/schemas/alpha/properties/p"
		beta   = "/components/schemas/beta/properties/p"
		q      = "/components/schemas/alpha/properties/q"
	)
	first, second, perm := bothWays(t, fixtureSwap)
	swapped := func(doc *ir.Document, target ir.TypeID) *ir.Document {
		out := *doc
		out.Types = maps.Clone(doc.Types)
		m := *doc.Types[abID].(*ir.Model)
		m.Properties = slices.Clone(m.Properties)
		for i := range m.Properties {
			if m.Properties[i].WireName == "p" {
				m.Properties[i].Type.Target = target
			}
		}
		out.Types[abID] = &m
		return &out
	}
	wrongWritten, wrongReversed := swapped(first, "t/prim/string"), swapped(second, "t/prim/integer")
	stated := func(lost, kept, of string) []fact {
		facts, _ := factsOf([]report{conflictAt(perm.written, lost, []string{kept}, of)}, perm.written)
		return facts
	}
	tied := []string{member}
	for name, tc := range map[string]struct {
		first, second *ir.Document
		stated        []fact
		tied          []string
		found         bool
	}{
		"the loser's type in each order":   {first: wrongWritten, second: wrongReversed, stated: stated(alpha, beta, member), tied: tied, found: true},
		"the kept type in each order":      {first: first, second: second, stated: stated(alpha, beta, member), tied: tied},
		"the loser's type as written only": {first: wrongWritten, second: second, stated: stated(alpha, beta, member), tied: tied},
		"no tie":                           {first: wrongWritten, second: wrongReversed, stated: stated(alpha, beta, member)},
		"a member no tie decided":          {first: wrongWritten, second: wrongReversed, stated: stated(alpha, beta, member), tied: []string{"p/other"}},
		"a place no declaration is at":     {first: wrongWritten, second: wrongReversed, stated: stated("/elsewhere", beta, member), tied: tied},
		"a kept place no declaration is at": {
			first: wrongWritten, second: wrongReversed, stated: stated(alpha, "/elsewhere", member), tied: tied,
		},
		"two declarations of one type": {
			first: first, second: second, tied: []string{"p/composed/components/schemas/AB/allOf/properties/q"},
			stated: stated(q, "/components/schemas/beta/properties/q", "p/composed/components/schemas/AB/allOf/properties/q"),
		},
		"a document that did not compile": {second: wrongReversed, stated: stated(alpha, beta, member), tied: tied},
	} {
		got := contradicted(tc.first, tc.second, tc.stated, tc.tied, perm)
		if !tc.found {
			assert.Empty(t, got, name)
			continue
		}
		require.Len(t, got, 1, name)
		assert.Equal(t, member+` takes in each order the type of the declaration its report says lost: "t/prim/string false" as written, at `+
			alpha+`, and "t/prim/integer false" reversed, at `+beta, got[0], name)
	}
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

// generatedRedeclarations is how many documents
// TestSequenceOrderInvariant_FindsOnlyARealDependenceInGeneratedRedeclarations
// writes, and generatedSeed the seed it writes them from.
const (
	generatedRedeclarations = 200
	generatedSeed           = 1996
)

// TestSequenceOrderInvariant_FindsOnlyARealDependenceInGeneratedRedeclarations
// holds the oracle, over documents several models redeclare members in
// (redeclarations), to finding only what a second reading of the two compiles
// shows is no reported tie: a member naming, by the order of its entries, one
// of two nodes written alike (namesAlikeNodesByOrder), which no rule lets
// follow that order (GitHub #2099). Every other document passes, the ties
// their conflicts report forgiven. It also holds that most documents draw a
// conflict and that some write a member alike at two nodes, so they reach
// both readings.
func TestSequenceOrderInvariant_FindsOnlyARealDependenceInGeneratedRedeclarations(t *testing.T) {
	rng := rand.New(rand.NewPCG(generatedSeed, generatedSeed))
	conflicting, alike := 0, 0
	for i := range generatedRedeclarations {
		doc, twins := redeclarations(rng, i)
		data, err := json.Marshal(doc, json.Deterministic(true))
		require.NoError(t, err)
		_, diags, err := compile(context.Background(), "spec.json", data, passes())
		require.NoError(t, err)
		if len(diagnosticsAt(diags, "openapi/conflicting-redeclaration")) > 0 {
			conflicting++
		}
		if twins {
			alike++
		}

		got := sequenceOrderInvariant(context.Background(), "spec.json", data, true)

		// A document reading the same reversed has nothing to permute.
		if got == passed(OracleSequenceOrder) || got == declined(OracleSequenceOrder, DeclineNothingToPermute) {
			continue
		}
		require.True(t, got.Found(), "document %d: %+v", i, got)
		first, second, perm := bothWays(t, string(data))
		assert.True(t, namesAlikeNodesByOrder(first, second, perm), "document %d is found and holds no member naming alike nodes by order: %s\n%s",
			i, got.Detail, data)
	}
	assert.Greater(t, conflicting, generatedRedeclarations/2, "most documents must draw a conflict between declarations")
	assert.Greater(t, alike, generatedRedeclarations/10, "the documents must write members alike at two nodes")
}

// namesAlikeNodesByOrder reports whether a member of one of first's models
// names, in second, the reversed compile, another node than in first, the two
// saying the same (said): an order dependence no tie between declarations
// that disagree explains.
func namesAlikeNodesByOrder(first, second *ir.Document, perm permutation) bool {
	for id, td := range first.Types {
		m, isModel := td.(*ir.Model)
		other, alsoModel := second.Types[id].(*ir.Model)
		if !isModel || !alsoModel {
			continue
		}
		for _, p := range m.Properties {
			i := slices.IndexFunc(other.Properties, func(q ir.Property) bool { return q.WireName == p.WireName })
			if i < 0 {
				continue
			}
			a, b := p.Type.Target, other.Properties[i].Type.Target
			if erase(string(a), perm.written) != erase(string(b), perm.moved) && said(first, a, 0) == said(second, b, 0) {
				return true
			}
		}
	}
	return false
}

// said is what the node id names says, without where it is written: a
// model's members and a list's elements read through, any other node by its
// ID, which a primitive and a component share between declarations.
func said(doc *ir.Document, id ir.TypeID, depth int) string {
	if depth > 8 {
		return "deep"
	}
	switch td := doc.Types[id].(type) {
	case *ir.Model:
		members := make([]string, len(td.Properties))
		for i, p := range td.Properties {
			members[i] = p.WireName + ":" + said(doc, p.Type.Target, depth+1)
		}
		slices.Sort(members)
		return "model{" + strings.Join(members, ",") + "}"
	case *ir.List:
		return "list<" + said(doc, td.Elem.Target, depth+1) + ">"
	default:
		return string(id)
	}
}

// redeclarations writes the i-th generated document, and whether it writes a
// member alike at two nodes: two to five components declaring p, some q and r
// too, each of a random kind (kindOf), some through an alias and some
// requiring a member, and two to eight models composing two to four entries
// each, one in five the entries of an earlier model reversed, so two models
// trade the declarations a tie decides between.
func redeclarations(rng *rand.Rand, i int) (map[string]any, bool) {
	schemas := map[string]any{
		"Obj":  map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}},
		"List": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	}
	inline := map[string]int{}
	components := make([]string, 2+rng.IntN(4))
	for j := range components {
		properties := map[string]any{}
		for _, member := range []string{"p", "q", "r"}[:1+rng.IntN(3)] {
			kind := kinds[rng.IntN(len(kinds))]
			inline[member+kind]++
			properties[member] = kindOf(kind)
			if rng.Float64() < 0.15 {
				schemas[fmt.Sprintf("Alias%d%s", j, member)] = properties[member]
				properties[member] = refTo(fmt.Sprintf("Alias%d%s", j, member))
				inline[member+kind]--
			}
		}
		component := map[string]any{"type": "object", "properties": properties}
		if rng.Float64() < 0.2 {
			component["required"] = []string{"p"}
		}
		components[j] = fmt.Sprintf("C%d", j)
		schemas[components[j]] = component
	}
	count := 2 + rng.IntN(7)
	models, composed := make([]string, 0, count), make([][]any, 0, count)
	for m := range count {
		entries := make([]any, 2+rng.IntN(3))
		for e := range entries {
			entries[e] = entryOf(rng, schemas, components)
		}
		switch r := rng.Float64(); {
		case len(models) > 0 && r < 0.2:
			entries = slices.Clone(composed[rng.IntN(len(composed))])
			slices.Reverse(entries)
		case len(models) > 0 && r < 0.4:
			entries[rng.IntN(len(entries))] = refTo(models[rng.IntN(len(models))])
		default:
		}
		model := map[string]any{"allOf": entries}
		if rng.Float64() < 0.1 {
			model["properties"] = map[string]any{"r": map[string]any{"type": "string"}}
		}
		if rng.Float64() < 0.15 {
			names := []string{"p", "q", "r"}
			rng.Shuffle(len(names), func(a, b int) { names[a], names[b] = names[b], names[a] })
			model["required"] = names[:2]
		}
		models, composed = append(models, fmt.Sprintf("M%d", m)), append(composed, entries)
		schemas[models[m]] = model
	}
	twins := false
	for _, kind := range []string{"object", "array"} {
		for _, member := range []string{"p", "q", "r"} {
			twins = twins || inline[member+kind] > 1
		}
	}
	return map[string]any{"openapi": "3.0.3", "info": map[string]any{"title": fmt.Sprintf("g%d", i), "version": "1"},
		"paths": map[string]any{}, "components": map[string]any{"schemas": schemas}}, twins
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
		return map[string]any{"type": "object", "properties": map[string]any{"p": kindOf(kinds[rng.IntN(len(kinds))])}}
	case r < 0.45:
		pair := rng.Perm(len(components)) // two at least
		return map[string]any{"allOf": []any{refTo(components[pair[0]]), refTo(components[pair[1]])}}
	default:
		return refTo(c)
	}
}

// kinds are the kinds a generated declaration takes (kindOf).
var kinds = []string{"string", "integer", "boolean", "shared object", "shared array", "object", "array"}

// kindOf is a schema of kind: a primitive, the one shared node of its kind,
// or an object or an array written where it is declared, so two declarations
// of the kind say the same at two nodes.
func kindOf(kind string) map[string]any {
	switch kind {
	case "shared object":
		return refTo("Obj")
	case "shared array":
		return refTo("List")
	case "object":
		return map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}}
	case "array":
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	default:
		return map[string]any{"type": kind}
	}
}

// refTo is a $ref to the component name.
func refTo(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }

// TestStatedAlone_LeavesOutWhatAnotherTieReaches pins which tied members keep
// their docs compared: one a stated tie decided and nothing else reaches. A
// member a moved report names or sits over, or one overriding a declaration a
// tie decided, follows that tie too, and a document that did not compile
// removes none.
func TestStatedAlone_LeavesOutWhatAnotherTieReaches(t *testing.T) {
	t.Parallel()
	stated := []string{"p/M/p", "p/Q/q"}
	for name, tc := range map[string]struct {
		moved tieSites
		tied  []string
		doc   *ir.Document
		want  []string
	}{
		"nothing else":                    {tied: stated, doc: settledDoc(), want: stated},
		"a moved report names one":        {moved: tieSites{nodes: []string{"p/Q/q"}}, tied: stated, doc: settledDoc(), want: []string{"p/M/p"}},
		"a moved report sits over one":    {moved: tieSites{pointers: []string{"/Q"}}, tied: stated, doc: settledDoc(), want: []string{"p/M/p"}},
		"one overrides a tied member":     {tied: []string{"p/AB/p", "p/M/p", "p/Q/q"}, doc: settledDoc(), want: []string{"p/Q/q"}},
		"a document that did not compile": {moved: tieSites{nodes: []string{"p/Q/q"}}, tied: stated, want: stated},
	} {
		assert.Equal(t, tc.want, statedAlone(stated, tc.moved, tc.tied, tc.doc, nil), name)
	}
	assert.Empty(t, statedAlone(nil, tieSites{}, nil, settledDoc(), nil))
	assert.Equal(t, []string{"p/M/p", "p/Q/q"}, stated, "the members handed in are left as they were")
}

// TestDiagnosticKey_ReadsWhatElseADiagnosticConcerns pins that both order
// oracles compare a diagnostic's related entries: two findings alike but for
// a role, a place, a node, a source or a writer there are two keys, and the
// entries' order is part of it.
func TestDiagnosticKey_ReadsWhatElseADiagnosticConcerns(t *testing.T) {
	t.Parallel()
	base := func() ir.Diagnostic {
		d := diagnostic(ir.SeverityWarning, "x/conflict", "/A/p", "")
		d.Related = []ir.Related{
			{Role: ir.RelatedKept, Provenance: ir.Provenance{Pointer: "/B/p"}},
			{Role: ir.RelatedDecided, Provenance: ir.Provenance{Source: ir.NoSource, Node: "p/M/p"}},
		}
		return d
	}
	same := base()
	assert.Equal(t, diagnosticKey(base()), diagnosticKey(same))
	for name, change := range map[string]func(*ir.Diagnostic){
		"no entries": func(d *ir.Diagnostic) { d.Related = nil },
		"role":       func(d *ir.Diagnostic) { d.Related[0].Role = ir.RelatedDecided },
		"pointer":    func(d *ir.Diagnostic) { d.Related[0].Provenance.Pointer = "/C/p" },
		"node":       func(d *ir.Diagnostic) { d.Related[1].Provenance.Node = "p/N/p" },
		"source":     func(d *ir.Diagnostic) { d.Related[0].Provenance.Source = 1 },
		"writer":     func(d *ir.Diagnostic) { d.Related[0].Provenance.WrittenBy = 1 },
		"positioned": func(d *ir.Diagnostic) { d.Related[0].Provenance.Position = ir.Position{Line: 1} },
		"inferred":   func(d *ir.Diagnostic) { d.Related[1].Provenance.Inferred = "h" },
		"order":      func(d *ir.Diagnostic) { d.Related[0], d.Related[1] = d.Related[1], d.Related[0] },
	} {
		changed := base()
		change(&changed)
		assert.NotEqual(t, diagnosticKey(base()), diagnosticKey(changed), name)
	}
}
