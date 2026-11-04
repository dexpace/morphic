// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// basesSpec is a source whose schemas start resources an $id names at several
// depths, relative and absolute, beside an $id naming its own document, a
// property named $id, an $id a merge brings in, raw YAML holding one and a key
// a pointer escapes; o.yaml is a document of schemas holding them too.
const basesSpec = `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-raw: {$id: 'https://x.test/raw', k: {type: string}}
x-base: &base {$id: 'https://x.test/merged', type: object}
components:
  schemas:
    A:
      $id: 'https://x.test/a/'
      properties:
        b:
          $id: 'b.json'
          properties:
            c: {$id: '/abs.json', items: {type: string}}
            d: {type: string}
        $id: {type: string}
        a~/b: {$id: 'esc.json'}
    Same: {$id: 'spec.yaml', properties: {q: {type: string}}}
    M: {<<: *base, properties: {r: {type: string}}}
    O: {$ref: 'o.yaml#/S'}
    L: {allOf: [{$id: 'https://x.test/l', not: {$ref: '#/$defs/n'}}, {properties: {x: {type: string}}}]}
`

// basesTable binds basesSpec, o.yaml beside it.
func basesTable(t *testing.T) *Table {
	t.Helper()
	docs := map[string]string{"o.yaml": "S: {$id: 'https://x.test/o', a: {b: {$id: 'p.json', c: {type: string}}}}\nT: [{$id: u.json}]\n"}
	cfg := Config{Self: "spec.yaml", Open: func(name string) (*yaml.Node, error) {
		src, ok := docs[name]
		if !ok {
			return nil, errors.New("no such document")
		}
		var n yaml.Node
		return &n, yaml.Unmarshal([]byte(src), &n)
	}}
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(basesSpec), &root))
	table := Bind(t.Context(), &root, cfg)
	require.Len(t, table.docs, 2, "o.yaml is read")
	return table
}

// positionsOf is every position of document id as a pointer walk reads it,
// merged keys among them, then, for each of the first ones, a position past it
// and its pointer spelled with no leading '/', which is not valid.
func positionsOf(table *Table, id DocID) []Position {
	view := nodeview.New()
	var out []Position
	var walk func(n *yaml.Node, at jsontext.Pointer, depth int)
	walk = func(n *yaml.Node, at jsontext.Pointer, depth int) {
		n = nodeview.Deref(n)
		if n == nil || depth > 12 {
			return
		}
		out = append(out, Position{Doc: id, Pointer: at})
		switch n.Kind {
		case yaml.MappingNode:
			for _, p := range view.MappingPairs(n) {
				walk(p.Val, at.AppendToken(p.Key), depth+1)
			}
		case yaml.SequenceNode:
			for i, c := range n.Content {
				walk(c, at.AppendToken(strconv.Itoa(i)), depth+1)
			}
		default:
		}
	}
	walk(table.docs[id].Root, "", 0)
	for _, p := range slices.Clone(out[:min(len(out), 12)]) {
		out = append(out, Position{Doc: id, Pointer: p.Pointer.AppendToken("missing").AppendToken("deeper")},
			Position{Doc: id, Pointer: jsontext.Pointer(strings.TrimPrefix(string(p.Pointer), "/"))})
	}
	return out
}

// reversed is a copy of ps, last first.
func reversed(ps []Position) []Position {
	out := slices.Clone(ps)
	slices.Reverse(out)
	return out
}

// TestBases_AnswerAsAWalkFromTheRoot holds what a remembered walk answers to a
// walk from the document's root (resources.holding), at every position of each
// fixture and asked in several orders, since what is remembered depends on what
// was asked before: every resource, its root and its URI, and whether it is
// known. The fixtures add, to basesTable's, an $id merged past the merge bound,
// one past the pointer segments a walk follows, and a root that is no node.
func TestBases_AnswerAsAWalkFromTheRoot(t *testing.T) {
	t.Parallel()
	for name, fixture := range map[string]func(*testing.T) (*Table, []Position){
		"resources": func(t *testing.T) (*Table, []Position) {
			table := basesTable(t)
			return table, append(positionsOf(table, Source), positionsOf(table, 1)...)
		},
		"merged past the bound": func(t *testing.T) (*Table, []Position) {
			var b strings.Builder
			b.WriteString("x-c0: &c0 {$id: 'https://x.test/m', properties: {q: {type: string}}}\n")
			for i := 1; i <= 70; i++ {
				fmt.Fprintf(&b, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
			}
			b.WriteString("components:\n  schemas:\n    A: {<<: *c70, properties: {q: {type: string}}}\n    B: {<<: *c10}\n")
			table := sourceTable(t, b.String())
			return table, positionsOf(table, Source)
		},
		"past the segment bound": func(t *testing.T) (*Table, []Position) {
			const levels = nodeview.MaxPointerSegments/2 + 10
			table := sourceTable(t, "components: {schemas: {A: "+strings.Repeat("{properties: {p: ", levels)+
				"{$id: 'deep.json'}"+strings.Repeat("}}", levels)+"}}\n")
			var out []Position
			at := jsontext.Pointer("/components/schemas/A")
			for i := range levels {
				if at = at.AppendToken("properties").AppendToken("p"); i%100 == 0 || i == levels-1 {
					out = append(out, Position{Pointer: at})
				}
			}
			return table, out
		},
		"no root": func(t *testing.T) (*Table, []Position) {
			return Bind(t.Context(), &yaml.Node{Kind: yaml.AliasNode}, Config{Self: "spec.yaml"}),
				[]Position{{}, {Pointer: "/a"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			probe, all := fixture(t)
			require.NotEmpty(t, probe.docs)
			const shuffles = 4
			orders := make([][]Position, 0, 2+shuffles)
			orders = append(orders, all, reversed(all))
			rng := rand.New(rand.NewPCG(1, 2))
			for range shuffles {
				shuffled := slices.Clone(all)
				rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
				orders = append(orders, shuffled)
			}
			for _, order := range orders {
				table, _ := fixture(t)
				for _, p := range order {
					path, _ := table.bases.res.docs.path(p)
					want, wantKnown := table.bases.res.holding(p, path, true)
					got, known := table.bases.holding(p)
					assert.Equal(t, want, got, p)
					assert.Equal(t, wantKnown, known, p)
				}
			}
		})
	}
}

// sourceTable binds src, an OpenAPI 3.1 document holding nothing but it.
func sourceTable(t *testing.T, src string) *Table {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n"+src), &root))
	return Bind(t.Context(), &root, Config{Self: "spec.yaml"})
}

// TestBases_TheFixturesReachWhatTheyName pins that the agreement above compares
// what its fixtures are for: resources an $id starts, one the merge bound leaves
// unknown, and an $id past the segment bound that a walk never reaches.
func TestBases_TheFixturesReachWhatTheyName(t *testing.T) {
	t.Parallel()
	table := basesTable(t)
	var owned int
	for _, p := range append(positionsOf(table, Source), positionsOf(table, 1)...) {
		if res, _ := table.bases.holding(p); res.id {
			owned++
		}
	}
	assert.Greater(t, owned, 10, "an $id starts many of the resources compared")

	var b strings.Builder
	b.WriteString("x-c0: &c0 {properties: {q: {type: string}}}\n")
	for i := 1; i <= 70; i++ {
		fmt.Fprintf(&b, "x-c%d: &c%d {<<: *c%d}\n", i, i, i-1)
	}
	b.WriteString("components:\n  schemas:\n    A: {<<: *c70, properties: {q: {type: string}}}\n")
	_, known := sourceTable(t, b.String()).bases.holding(Position{Pointer: "/components/schemas/A/properties/q"})
	assert.False(t, known, "below a schema past the merge bound")

	const levels = nodeview.MaxPointerSegments/2 + 10
	deep := sourceTable(t, "components: {schemas: {A: "+strings.Repeat("{properties: {p: ", levels)+
		"{$id: 'deep.json'}"+strings.Repeat("}}", levels)+"}}\n")
	res, _ := deep.bases.holding(Position{Pointer: jsontext.Pointer("/components/schemas/A" + strings.Repeat("/properties/p", levels))})
	assert.False(t, res.id, "the $id lies past the tokens a walk follows")
}

// TestBases_WalkEachPositionOnce pins what remembering buys: asking at every
// level of a deep schema walks each position on the way once, in whatever
// order the levels ask, where a walk from the root at each would walk the
// square of the depth.
func TestBases_WalkEachPositionOnce(t *testing.T) {
	t.Parallel()
	const levels = 200
	src := "a: " + strings.Repeat("{p: ", levels) + "{}" + strings.Repeat("}", levels) + "\n"
	spine := make([]Position, 0, levels+2)
	at := jsontext.Pointer("/a")
	spine = append(spine, Position{}, Position{Pointer: at})
	for range levels {
		at = at.AppendToken("p")
		spine = append(spine, Position{Pointer: at})
	}
	for name, order := range map[string][]Position{"down": spine, "up": reversed(spine)} {
		var root yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(src), &root))
		table := Bind(t.Context(), &root, Config{Self: "spec.yaml"})
		before := table.bases.res.docs.work.steps.Spent()
		for _, p := range order {
			_, known := table.bases.holding(p)
			require.True(t, known)
		}
		assert.Equal(t, len(spine), table.bases.res.docs.work.steps.Spent()-before, name)
	}
}
