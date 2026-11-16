// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// broughtRef is a property's schema that writes a $ref, so a table lists an
// entry at each position a walk reads a copy of it at.
const broughtRef = "{$ref: '#/components/schemas/T'}"

// broughtRefNodes and broughtRefBytes are what one such property names past
// its own pointer taken four times: its key and its schema at that pointer,
// and the schema's `$ref` key and value a token further.
const (
	broughtRefNodes = 4
	broughtRefBytes = 2 * len("/$ref")
)

// broughtLeaf is the schema a chain of bringers brings about: two such
// properties, and with wide a third.
func broughtLeaf(wide bool) string {
	leaf := "{type: object, properties: {uuuuuuuuuuuuuuuuuuuuuuuu: " + broughtRef + ", vvvvvvvvvvvvvvvvvvvvvvvv: " + broughtRef
	if wide {
		leaf += ", wwwwwwwwwwwwwwwwwwwwwwww: " + broughtRef
	}
	return leaf + "}}"
}

// brought is what a document writes to hold a schema at several positions:
// the schema holding them, and the anchors and raw mounts it reads them from.
type brought struct{ defs, mounts, schema string }

// bringer is one way a document brings the schema inner to n positions, its
// anchors and mounts named by id so that one bringer nests in another.
type bringer struct {
	name  string
	bring func(id string, n int, inner string) brought
}

// each writes item for every index below n, joined by sep.
func each(n int, sep string, item func(i string) string) string {
	items := make([]string, n)
	for i := range items {
		items[i] = item(strconv.Itoa(i))
	}
	return strings.Join(items, sep)
}

// anchored returns a bringer whose n positions are properties, each written by
// use around an alias of the anchored schema, beside the anchors more adds.
func anchored(name, more string, use func(alias, i string) string) bringer {
	return bringer{name: name, bring: func(id string, n int, inner string) brought {
		defs := "a" + id + ": &a" + id + " " + inner + strings.ReplaceAll(more, "@", id)
		return brought{defs: defs, schema: "{type: object, properties: {" + each(n, ", ", func(i string) string {
			return strings.ReplaceAll(use("a"+id, i), "@", id)
		}) + "}}"}
	}}
}

// landed returns a bringer whose n positions are raw mounts of the anchored
// schema, each named by the reference site writes around its pointer, the
// sites held as within writes them.
func landed(name string, within func(sites string) string, site func(ref, i string) string) bringer {
	return bringer{name: name, bring: func(id string, n int, inner string) brought {
		mount := func(i string) string { return "x-r" + id + "-" + i }
		return brought{
			defs:   "a" + id + ": &a" + id + " " + inner,
			mounts: each(n, "", func(i string) string { return mount(i) + ": *a" + id + "\n" }),
			schema: within(each(n, ", ", func(i string) string { return site("'#/"+mount(i)+"'", i) })),
		}
	}}
}

// bringers lists the ways: an alias, a `<<` merge of one source, of a list and
// of a source that merges, a key written twice in a mapping and in a merged
// source, a list's entries, and a raw mount each kind of reference lands on,
// a mapping value tagged null among them, which the binder reads by its text.
var bringers = []bringer{
	anchored("alias", "", func(a, i string) string { return "q" + i + ": *" + a }),
	anchored("merge", "", func(a, i string) string { return "q" + i + ": {<<: *" + a + "}" }),
	anchored("twice", "", func(a, i string) string { return "q" + i + ": {type: string}, q" + i + ": *" + a }),
	{name: "list", bring: func(id string, n int, inner string) brought {
		return brought{defs: "a" + id + ": &a" + id + " " + inner,
			schema: "{allOf: [" + each(n, ", ", func(string) string { return "*a" + id }) + "]}"}
	}},
	landed("ref", func(s string) string { return "{allOf: [" + s + "]}" },
		func(ref, _ string) string { return "{$ref: " + ref + "}" }),
	anchored("merge list", ", b@: &b@ {description: b}",
		func(a, i string) string { return "q" + i + ": {<<: [*b@, *" + a + "]}" }),
	anchored("merge of a merge", ", b@: &b@ {<<: *a@, description: b}",
		func(_, i string) string { return "q" + i + ": {<<: *b@}" }),
	anchored("twice in a merged source", ", s@: &s@ {type: object, properties: {p: *a@}, properties: {}}",
		func(_, i string) string { return "q" + i + ": {<<: *s@}" }),
	landed("dynamic ref", func(s string) string { return "{allOf: [" + s + "]}" },
		func(ref, _ string) string { return "{$dynamicRef: " + ref + "}" }),
	landed("mapping", func(s string) string {
		return "{type: object, discriminator: {propertyName: t, mapping: {" + s + "}}}"
	}, func(ref, i string) string { return "m" + i + ": " + ref }),
	landed("default mapping", func(s string) string { return "{anyOf: [" + s + "]}" }, func(ref, _ string) string {
		return "{type: object, discriminator: {propertyName: t, defaultMapping: " + ref + "}}"
	}),
	landed("mapping tagged null", func(s string) string {
		return "{type: object, discriminator: {propertyName: t, mapping: {" + s + "}}}"
	}, func(ref, i string) string { return "m" + i + ": !!null " + ref }),
}

// coreBringers is how many of bringers, from the first, nest three deep.
const coreBringers = 5

// broughtPositions names where a document holds the schema a chain of
// bringers writes, in the order broughtDocument fills them: an ordinary schema
// position, beneath an Encoding Object another nests, under an extension a
// $ref lands on, and in a raw parameter mounted twice, a $ref landing on each.
var broughtPositions = []string{"a schema", "a nested encoding", "an extension", "a parameter mounted twice"}

// chainName names chain, the outermost bringer first.
func chainName(chain []bringer) string {
	names := make([]string, len(chain))
	for i, b := range chain {
		names[i] = b.name
	}
	return strings.Join(names, " of a ")
}

// broughtChains lists each bringer alone, each pair, and each triple of the
// core ones, the first of a chain the outermost.
func broughtChains() [][]bringer {
	chains := make([][]bringer, 0, (1+len(bringers))*len(bringers)+coreBringers*coreBringers*coreBringers)
	for _, a := range bringers {
		chains = append(chains, []bringer{a})
		for _, b := range bringers {
			chains = append(chains, []bringer{a, b})
		}
	}
	for _, a := range bringers[:coreBringers] {
		for _, b := range bringers[:coreBringers] {
			for _, c := range bringers[:coreBringers] {
				chains = append(chains, []bringer{a, b, c})
			}
		}
	}
	return chains
}

// broughtCopies is how many positions each bringer of a chain brings its
// schema to.
const broughtCopies = 2

// broughtDocument writes the document holding, at the position at indexes
// (broughtPositions), the leaf brought to broughtCopies positions by each
// bringer of chain in turn, and a string schema at every other.
func broughtDocument(chain []bringer, at int, wide bool) string {
	var defs, mounts []string
	schema := broughtLeaf(wide)
	for depth, by := range slices.Backward(chain) {
		b := by.bring(strconv.Itoa(depth), broughtCopies, schema)
		defs, mounts, schema = append(defs, b.defs), append(mounts, b.mounts), b.schema
	}
	held := []string{"{type: string}", "{type: string}", "{type: string}", "{type: string}"}
	held[at] = schema
	return "openapi: 3.2.0\ninfo: {title: t, version: '1'}\nx-defs: {" + strings.Join(defs, ", ") + "}\n" +
		strings.Join(mounts, "") + "x-held: " + held[2] + "\n" +
		"x-param: &param {name: n, in: query, schema: " + held[3] + "}\nx-p0: *param\nx-p1: *param\n" +
		"paths: {/p: {post: {requestBody: {content: {multipart/form-data: {schema: {type: object}, encoding: " +
		"{f: {encoding: {g: {headers: {H: {schema: " + held[1] + "}}}}}}}}}, " +
		"responses: {'200': {description: ok}}}}}\n" +
		"components: {schemas: {T: {type: string}, S: " + held[0] + ", E: {$ref: '#/x-held'}}, " +
		"parameters: {P0: {$ref: '#/x-p0'}, P1: {$ref: '#/x-p1'}}}\n"
}

// broughtTotals returns what NamedPointerBytes totals for text, and what a
// table of it spells for the references it lists: how many entries, and the
// pointers of their sites, counted as the total counts one, a '/' and the
// bytes of each token.
func broughtTotals(t *testing.T, text string) (weighed, entries, spelled int) {
	t.Helper()
	doc := openapitest.YAMLDocument(t, text)
	weighed, complete := bind.NamedPointerBytes(t.Context(), doc, namedSteps)
	require.True(t, complete, "the total is whole")
	table := bind.Bind(t.Context(), doc, self)
	require.False(t, table.Exhausted() || table.Truncated(), "the table is whole")
	for _, e := range table.Entries() {
		entries++
		for token := range e.Site.Pointer.Tokens() {
			spelled += len(token) + 1
		}
	}
	return weighed, entries, spelled
}

// TestNamedPointerBytes_TotalsEachPositionATableReads holds the total to the
// walk it bounds, over documents built from every way of bringing a schema to
// several positions, nested up to three deep at each kind of position. A table
// lists an entry wherever it reads a $ref, so a property more in the schema
// brought shows each position the table reads a copy at, and the total grows
// by no less than the property's four nodes spell at each of them. A total
// that counts a copy the table reads less often than the table reads it fails.
func TestNamedPointerBytes_TotalsEachPositionATableReads(t *testing.T) {
	for _, chain := range broughtChains() {
		for at, position := range broughtPositions {
			name := chainName(chain) + " at " + position
			weighed, entries, spelled := broughtTotals(t, broughtDocument(chain, at, false))
			wideWeighed, wideEntries, wideSpelled := broughtTotals(t, broughtDocument(chain, at, true))

			read := wideEntries - entries
			require.GreaterOrEqual(t, read, broughtCopies, "%s: the table reads the copies brought", name)
			assert.GreaterOrEqual(t, wideWeighed-weighed, broughtRefNodes*(wideSpelled-spelled)+broughtRefBytes*read,
				"%s: the total names the property at each position the table reads it at", name)
		}
	}
}
