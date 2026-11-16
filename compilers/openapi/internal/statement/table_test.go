// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement_test

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// atoms returns what st's schema and entries say, one line each: the target
// of each reference, and each key but allOf and $ref with the text of its
// value. Two spellings of one meaning say the same lines.
func atoms(st *statement.Statement) []string {
	var out []string
	for _, s := range append([]*statement.Schema{st.Schema}, entrySchemas(st)...) {
		if s.Ref != nil {
			out = append(out, "ref "+string(s.Ref.Entry.Hop.Target.Pointer))
		}
		for _, k := range s.Keys {
			if k.Name != "allOf" && k.Name != "$ref" {
				out = append(out, k.Name+" "+k.Node.Value)
			}
		}
	}
	slices.Sort(out)
	return out
}

// TestFlatten_FiveSpellingsShareTheirAtoms reads the five spellings of two
// keywords said of a $ref use, beside it, beside an allOf of it, on an inline
// entry after it, on one before it, and one allOf deeper, and holds each to
// the same atoms: a statement differs between them only in where it places
// each, never in what they say.
func TestFlatten_FiveSpellingsShareTheirAtoms(t *testing.T) {
	t.Parallel()
	const said = "minLength: 3, description: x"
	table, c := tableOver(t, document("3.1.0",
		"T: {type: string}",
		"Beside: {$ref: '#/components/schemas/T', "+said+"}",
		"BesideAllOf: {allOf: [{$ref: '#/components/schemas/T'}], "+said+"}",
		"After: {allOf: [{$ref: '#/components/schemas/T'}, {"+said+"}]}",
		"Before: {allOf: [{"+said+"}, {$ref: '#/components/schemas/T'}]}",
		"Deeper: {allOf: [{allOf: [{$ref: '#/components/schemas/T'}], "+said+"}]}",
	), rulesAt(t, "3.1.0", ""))
	want := []string{"description x", "minLength 3", "ref /components/schemas/T"}
	for _, name := range []string{"Beside", "BesideAllOf", "After", "Before", "Deeper"} {
		assert.Equal(t, want, atoms(table.At(component(c, name))), name)
	}
}

// nest returns a schema writing n allOf lists one inside another, the
// innermost holding leaf.
func nest(n int, leaf string) string {
	return strings.Repeat("{allOf: [", n) + leaf + strings.Repeat("]}", n)
}

// listing renders each entry of st as its pointer and whether it is a wrapper
// or capped.
func listing(st *statement.Statement) []string {
	out := make([]string, len(st.Entries))
	for i, e := range st.Entries {
		out[i] = fmt.Sprintf("%s wrapper=%v capped=%v", e.Rel, e.Wrapper, e.Capped)
	}
	return out
}

// TestFlatten_ListsWhatWalkAllOfLists pins the flattened list to the
// lowering's (docs/compilers/openapi.md#allof-classification): each wrapper
// after its own entries, MaxFlattenDepth lists flattened and the entry one
// list deeper capped, a $ref entry writing an allOf beside it, a boolean
// entry and one writing an empty allOf listed as they stand.
func TestFlatten_ListsWhatWalkAllOfLists(t *testing.T) {
	t.Parallel()
	table, c := tableOver(t, document("3.1.0",
		"T: {type: string}",
		"Flat: "+nest(statement.MaxFlattenDepth+1, "{type: string}"),
		"Capped: "+nest(statement.MaxFlattenDepth+2, "{type: string}"),
		"Mixed: {allOf: [{$ref: '#/components/schemas/T', allOf: [{type: string}]}, true, {allOf: []}, "+
			"{allOf: [{minLength: 1}, {maxLength: 2}], description: w}]}",
	), rulesAt(t, "3.1.0", ""))
	deepest := func(n int) string { return strings.Repeat("/allOf/0", n) }

	var flat []string
	flat = append(flat, deepest(statement.MaxFlattenDepth+1)+" wrapper=false capped=false")
	for n := statement.MaxFlattenDepth; n >= 1; n-- {
		flat = append(flat, deepest(n)+" wrapper=true capped=false")
	}
	assert.Equal(t, flat, listing(table.At(component(c, "Flat"))), "every list up to the depth flattens")

	var capped []string
	capped = append(capped, deepest(statement.MaxFlattenDepth+1)+" wrapper=false capped=true")
	for n := statement.MaxFlattenDepth; n >= 1; n-- {
		capped = append(capped, deepest(n)+" wrapper=true capped=false")
	}
	assert.Equal(t, capped, listing(table.At(component(c, "Capped"))), "the entry one list deeper is capped")

	mixed := table.At(component(c, "Mixed"))
	assert.Equal(t, []string{
		"/allOf/0 wrapper=false capped=false",
		"/allOf/1 wrapper=false capped=false",
		"/allOf/2 wrapper=false capped=false",
		"/allOf/3/allOf/0 wrapper=false capped=false",
		"/allOf/3/allOf/1 wrapper=false capped=false",
		"/allOf/3 wrapper=true capped=false",
	}, listing(mixed))
	require.Len(t, mixed.Entries, 6)
	assert.NotNil(t, mixed.Entries[0].Ref, "a $ref entry")
	assert.Equal(t, statement.Anything, mixed.Entries[1].Truth, "a boolean entry")
	assert.Empty(t, table.At(component(c, "T")).Entries, "a schema writing no allOf lists none")
}

// hierarchy is n models, each composing the one before it and, where wide, the
// first too, each redeclaring x and declaring a member of its own.
func hierarchy(n int, wide bool) string {
	var b strings.Builder
	b.WriteString("    A0: {type: object, properties: {x: {type: integer}}}\n")
	for i := 1; i < n; i++ {
		entries := fmt.Sprintf("{$ref: '#/components/schemas/A%d'}", i-1)
		if wide {
			entries += ", {$ref: '#/components/schemas/A0'}"
		}
		fmt.Fprintf(&b, "    A%d: {allOf: [%s, {type: object, properties: {x: {type: number}, m%d: {type: string}}}]}\n",
			i, entries, i)
	}
	return b.String()
}

// clique is n models each composing every other, each declaring x.
func clique(n int) string {
	var b strings.Builder
	for i := range n {
		var entries []string
		for j := range n {
			if j != i {
				entries = append(entries, fmt.Sprintf("{$ref: '#/components/schemas/A%d'}", j))
			}
		}
		fmt.Fprintf(&b, "    A%d: {allOf: [%s, {type: object, properties: {x: {type: integer}}}]}\n", i, strings.Join(entries, ", "))
	}
	return b.String()
}

// readAll reads, of the n components A0 to An-1 of spec, each component, each
// entry as a position of its own, and each property of either, and returns
// the objects that read and a second asking of the same read.
func readAll(t *testing.T, spec string, n int) (first, again int) {
	t.Helper()
	table, c := tableOver(t, spec, rulesAt(t, "3.1.0", ""))
	ask := func() {
		for i := range n {
			st := table.At(component(c, fmt.Sprintf("A%d", i)))
			require.Equal(t, statement.Stated, st.Truth)
			for _, s := range append([]*statement.Schema{st.Schema}, entrySchemas(st)...) {
				table.Read(s)
				if s.Object != nil {
					for _, p := range s.Object.Properties {
						table.Sub(p)
					}
				}
			}
		}
	}
	ask()
	first = statement.Reads(table)
	ask()
	return first, statement.Reads(table) - first
}

// entrySchemas returns the schema of each of st's entries.
func entrySchemas(st *statement.Statement) []*statement.Schema {
	out := make([]*statement.Schema, len(st.Entries))
	for i, e := range st.Entries {
		out[i] = e.Schema
	}
	return out
}

// TestTable_ReadsEachObjectOnce pins the memo: a hierarchy, a wide one and a
// clique, at n, 2n and 4n models, read each schema object they write once,
// so the objects read are the document's own count, linear in its models for
// a hierarchy however deep it nests, and asking every position again reads
// none.
func TestTable_ReadsEachObjectOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		spec    func(int) string
		n       int
		objects func(int) int
	}{
		// A0 and its property, then each model, its entries and its two
		// properties.
		{"a hierarchy", func(n int) string { return hierarchy(n, false) }, 50, func(n int) int { return 2 + 5*(n-1) }},
		{"a wide hierarchy", func(n int) string { return hierarchy(n, true) }, 50, func(n int) int { return 2 + 6*(n-1) }},
		// Each model, its n-1 references, its inline entry and its property.
		{"a clique", clique, 6, func(n int) int { return n * (n + 2) }},
	} {
		for _, n := range []int{tc.n, 2 * tc.n, 4 * tc.n} {
			first, again := readAll(t, openapitest.ComponentSpecVer("3.1.0", tc.spec(n)), n)
			assert.Equal(t, tc.objects(n), first, "%s of %d: each object read once", tc.name, n)
			assert.Zero(t, again, "%s of %d: asking again reads nothing", tc.name, n)
		}
	}
}

// TestTable_AtFindsWhatOfReads pins that a statement is the parser's value's,
// not its node's: an alias placing one node at two positions is decoded once
// for each (docs/parser-library.md#alias-expansion), so each position has a
// statement at it, its keys in the rows of where it is and its entries
// relative to it.
func TestTable_AtFindsWhatOfReads(t *testing.T) {
	t.Parallel()
	c := servicetest.Context(t, document("3.1.0",
		"A: &shared {allOf: [{type: object}], additionalItems: false}",
		"B: *shared",
	), service.Policy{}).Ctx
	draft07, own := rulesAt(t, "3.1.0", "http://json-schema.org/draft-07/schema#"), rulesAt(t, "3.1.0", "")
	rows := dialect.NewBuilder(own)
	rows.Root(component(c, "A"), nil, draft07)
	table := statement.NewTable(statement.Source{Bindings: c.Bindings(), Rules: rows.Table()})
	a, b := table.At(component(c, "A")), table.At(component(c, "B"))
	assert.NotSame(t, a, b)
	assert.Same(t, a.Node, b.Node, "one node")
	for _, tc := range []struct {
		name string
		st   *statement.Statement
		slot oas.Slot
	}{{"A", a, oas.SlotArray}, {"B", b, oas.SlotUnknown}} {
		assert.Equal(t, component(c, tc.name), tc.st.At, tc.name)
		k, _ := tc.st.Key("additionalItems")
		assert.Equal(t, tc.slot, k.Slot, "%s: read in the rows of where it is", tc.name)
		require.Len(t, tc.st.Entries, 1, tc.name)
		assert.Equal(t, jsontext.Pointer("/allOf/0"), tc.st.Entries[0].Rel, tc.name)
		at := component(c, tc.name)
		at.Pointer += "/allOf/0"
		assert.Equal(t, at, tc.st.Entries[0].At, tc.name)
		assert.Same(t, tc.st, table.Read(tc.st.Schema), "%s: read again, the same statement", tc.name)
	}
}

// TestTable_ReadsABuiltStatementWithoutALookup pins that a schema keeps the
// statement Of built over it: once each of a schema and its three inline
// entries has been read, reading them again, a hundred times each, returns
// that statement and looks none up.
func TestTable_ReadsABuiltStatementWithoutALookup(t *testing.T) {
	t.Parallel()
	table, c := tableOver(t, document("3.1.0",
		`S: {allOf: [{type: object}, {required: [a]}, {properties: {a: {type: string}}}]}`,
	), rulesAt(t, "3.1.0", ""))
	s := table.At(component(c, "S"))
	require.Len(t, s.Entries, 3)
	built := make([]*statement.Statement, 0, 1+len(s.Entries))
	built = append(built, s)
	for _, e := range s.Entries {
		built = append(built, table.Read(e.Schema))
	}
	before := statement.Lookups(table)
	for range 100 {
		for _, st := range built {
			assert.Same(t, st, table.Read(st.Schema))
		}
	}
	assert.Equal(t, before, statement.Lookups(table), "no lookup once each is built")
	for i, e := range s.Entries {
		assert.Same(t, built[i+1].Schema, e.Schema, "entry %d: the statement Read built is the entry's", i)
	}
}
