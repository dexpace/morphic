// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"testing"

	soaoverlay "github.com/speakeasy-api/openapi/overlay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/ir"
)

// bookstore is the example document of RFC 9535, section 1.5.
const bookstore = `{"store": {
  "book": [
    {"category": "reference", "author": "Nigel Rees", "title": "Sayings of the Century", "price": 8.95},
    {"category": "fiction", "author": "Evelyn Waugh", "title": "Sword of Honour", "price": 12.99},
    {"category": "fiction", "author": "Herman Melville", "title": "Moby Dick", "isbn": "0-553-21311-3", "price": 8.99},
    {"category": "fiction", "author": "J. R. R. Tolkien", "title": "The Lord of the Rings", "isbn": "0-395-19395-8", "price": 22.99}
  ],
  "bicycle": {"color": "red", "price": 399}
}}`

// TestReadSelector_SelectsWhatRFC9535sExamplesSelect holds the selectors the
// compiler reads to the results RFC 9535's own examples give, over its example
// document (section 1.5, Table 2): each selects the nodes the RFC lists, each
// node by a scalar it holds, in the RFC's order where it gives one.
func TestReadSelector_SelectsWhatRFC9535sExamplesSelect(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(bookstore), &root))
	doc := &soaoverlay.Overlay{Version: "1.0.0", JSONPathVersion: soaoverlay.JSONPathRFC9535}
	tests := map[string]struct {
		count int
		holds []string
	}{
		"$.store.book[*].author":     {4, []string{"Nigel Rees", "Evelyn Waugh", "Herman Melville", "J. R. R. Tolkien"}},
		"$..author":                  {4, []string{"Nigel Rees", "Evelyn Waugh", "Herman Melville", "J. R. R. Tolkien"}},
		"$.store.*":                  {2, nil},
		"$.store..price":             {5, []string{"8.95", "12.99", "8.99", "22.99", "399"}},
		"$..book[2]":                 {1, nil},
		"$..book[2].author":          {1, []string{"Herman Melville"}},
		"$..book[2].publisher":       {0, nil},
		"$..book[-1].title":          {1, []string{"The Lord of the Rings"}},
		"$..book[0,1].title":         {2, []string{"Sayings of the Century", "Sword of Honour"}},
		"$..book[:2].title":          {2, []string{"Sayings of the Century", "Sword of Honour"}},
		"$..book[?@.isbn].title":     {2, []string{"Moby Dick", "The Lord of the Rings"}},
		"$..book[?@.price<10].title": {2, []string{"Sayings of the Century", "Moby Dick"}},
		"$..*":                       {27, nil},
	}
	for selector, want := range tests {
		path, diags := readSelector(doc, action{overlay: ir.Provenance{Source: 1}}, "target", selector)
		require.Empty(t, diags, selector)

		got := path.Query(&root)
		assert.Len(t, got, want.count, selector)
		if want.holds == nil {
			continue
		}
		values := make([]string, 0, len(got))
		for _, n := range got {
			values = append(values, n.Value)
		}
		assert.Equal(t, want.holds, values, selector)
	}
}

// TestScanSelector_TellsAStringFromTheRest pins how the scan reads a
// selector's text: a construct is noted where RFC 9535 places it, outside a
// string literal or inside one as the case is, an escaped quote or backslash
// does not end a literal or begin an escape, a literal of either quote holds
// the other, and an unclosed literal or a trailing backslash ends the text
// without fault.
func TestScanSelector_TellsAStringFromTheRest(t *testing.T) {
	t.Parallel()
	tests := map[string]selectorFacts{
		`$.a`:                   {},
		`$['~']`:                {},
		`$['a"~"']`:             {},
		`$["a'~'"]`:             {},
		`$['it\'s ~']`:          {},
		`$['a\\r']`:             {},
		`$['match(']`:           {},
		`$.amatch`:              {},
		`$[?@.x == 'search(']`:  {},
		`$['a`:                  {},
		`$['a\`:                 {},
		`$.a~`:                  {foreign: "uses ~, which RFC 9535 does not define"},
		`$['a\rb']`:             {misread: `writes the escape \r, which the compiler's JSONPath parser reads as a line feed`},
		`$["a\rb"]`:             {misread: `writes the escape \r, which the compiler's JSONPath parser reads as a line feed`},
		`$.true`:                {unread: `names the member "true" after a dot, which the compiler's JSONPath parser cannot read; write it ['true']`},
		`$..false`:              {unread: `names the member "false" after a dot, which the compiler's JSONPath parser cannot read; write it ['false']`},
		`$.trueish.nullable`:    {},
		`$[?match(@.a, 'x')]`:   {misread: "calls match(), which the compiler's JSONPath evaluator answers true for a node lacking the value it tests"},
		`$[?!search(@.a, 'x')]`: {misread: "calls search(), which the compiler's JSONPath evaluator answers true for a node lacking the value it tests"},
		"$['a\x01']":            {foreign: "holds a control character unescaped in a string, which RFC 9535 refuses"},
		"$[?@.a ==\t1]":         {},
		"$.a~[?match(@,'\\r')]": {
			foreign: "uses ~, which RFC 9535 does not define",
			misread: "calls match(), which the compiler's JSONPath evaluator answers true for a node lacking the value it tests",
		},
	}
	for selector, want := range tests {
		assert.Equal(t, want, scanSelector(selector), "%q", selector)
	}
}
