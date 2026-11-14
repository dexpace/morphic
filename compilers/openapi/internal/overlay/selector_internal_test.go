// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
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
		query, diags := readSelector(action{overlay: ir.Provenance{Source: 1}}, "target", selector)
		require.Empty(t, diags, selector)

		steps := budget.New(budget.Unbounded())
		got, err := query.Select(t.Context(), nodeview.New(), &root, &steps)
		require.NoError(t, err, selector)
		assert.Len(t, got, want.count, selector)
		if want.holds == nil {
			continue
		}
		values := make([]string, 0, len(got))
		for _, l := range got {
			values = append(values, l.Node.Value)
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
func TestHintFor_TellsAStringFromTheRest(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		`$.a`:                                  "",
		`$['~']`:                               "",
		`$['a"~"']`:                            "",
		`$["a'~'"]`:                            "",
		`$['it\'s ~']`:                         "",
		`$['a\\r']`:                            "",
		`$['match(']`:                          "",
		`$.amatch`:                             "",
		`$[?@.x == 'search(']`:                 "",
		`$['a`:                                 "",
		`$['a\`:                                "",
		`$.a~`:                                 "uses ~, which RFC 9535 does not define",
		`$['a\rb']`:                            "",
		`$["a\rb"]`:                            "",
		`$.true`:                               "",
		`$..false`:                             "",
		`$.trueish.nullable`:                   "",
		`$[?match(@.a, 'x')]`:                  "",
		`$[?!search(@.a, 'x')]`:                "",
		"$['a\x01']":                           "",
		"$[?@.a ==\t1]":                        "",
		`$.a_b._c.é`:                           "",
		`$.*.a`:                                "",
		`$..*`:                                 "",
		`$..[0]`:                               "",
		`$[?@.a == 1.5]`:                       "",
		`$[?@.a==-1.5e+3]`:                     "",
		`$[?@.a == 2E7].b`:                     "",
		`$[1:-1]`:                              "",
		`$.x-internal`:                         shorthandRefusal("x-internal", `['x-internal']`),
		`$.paths.*.get[?@.x-oai-traits.paged]`: shorthandRefusal("x-oai-traits", `['x-oai-traits']`),
		`$..x-a`:                               shorthandRefusal("x-a", `['x-a']`),
		`$.responses.200`:                      shorthandRefusal("200", `['200']`),
		`$.a.$ref`:                             shorthandRefusal("$ref", `['$ref']`),
		`$.a.true-x`:                           shorthandRefusal("true-x", `['true-x']`),
		`$.a\b`:                                shorthandRefusal(`a\b`, `['a\\b']`),
		`$[?@.a == 1.5].b-c`:                   shorthandRefusal("b-c", `['b-c']`),
		"$.a~[?match(@,'\\r')]":                "uses ~, which RFC 9535 does not define",
	}
	for selector, want := range tests {
		assert.Equal(t, want, hintFor(selector), "%q", selector)
	}
}

// shorthandRefusal is the finding on a member name after a dot that RFC 9535's
// shorthand does not admit, which fix spells as a name selector.
func shorthandRefusal(name, fix string) string {
	return `names the member ` + strconv.Quote(name) + ` after a dot, where RFC 9535 admits only letters, digits, underscores ` +
		`and characters past ASCII, starting with no digit; write it ` + fix
}
