// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package overlay

import (
	"strconv"
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
		`$.a_b._c.é`:            {},
		`$.*.a`:                 {},
		`$..*`:                  {},
		`$..[0]`:                {},
		`$[?@.a == 1.5]`:        {},
		`$[?@.a==-1.5e+3]`:      {},
		`$[?@.a == 2E7].b`:      {},
		`$[1:-1]`:               {},
		`$.x-internal`:          {foreign: shorthandRefusal("x-internal", `['x-internal']`)},
		`$.paths.*.get[?@.x-oai-traits.paged]`: {
			foreign: shorthandRefusal("x-oai-traits", `['x-oai-traits']`),
		},
		`$..x-a`:             {foreign: shorthandRefusal("x-a", `['x-a']`)},
		`$.responses.200`:    {foreign: shorthandRefusal("200", `['200']`)},
		`$.a.$ref`:           {foreign: shorthandRefusal("$ref", `['$ref']`)},
		`$.a.true-x`:         {foreign: shorthandRefusal("true-x", `['true-x']`)},
		`$.a\b`:              {foreign: shorthandRefusal(`a\b`, `['a\\b']`)},
		`$[?@.a == 1.5].b-c`: {foreign: shorthandRefusal("b-c", `['b-c']`)},
		"$.a~[?match(@,'\\r')]": {
			foreign: "uses ~, which RFC 9535 does not define",
			misread: "calls match(), which the compiler's JSONPath evaluator answers true for a node lacking the value it tests",
		},
	}
	for selector, want := range tests {
		assert.Equal(t, want, scanSelector(selector), "%q", selector)
	}
}

// shorthandRefusal is the finding on a member name after a dot that RFC 9535's
// shorthand does not admit, which fix spells as a name selector.
func shorthandRefusal(name, fix string) string {
	return `names the member ` + strconv.Quote(name) + ` after a dot, where RFC 9535 admits only letters, digits, underscores ` +
		`and characters past ASCII, starting with no digit; write it ` + fix
}

// TestScanSelector_TellsAWellFormedEscapeFromAMalformedOne pins the
// hexchar grammar the scan holds an escape of a character to: four
// hexadecimal digits of either case naming no surrogate, or a high
// surrogate's beside a low one's, which RFC 9535 reads and the parser cannot,
// and anything else, which RFC 9535 refuses. The escapes are assembled, so the
// source holds none.
func TestScanSelector_TellsAWellFormedEscapeFromAMalformedOne(t *testing.T) {
	t.Parallel()
	u := `\` + "u"
	unread := func(fix string) selectorFacts {
		return selectorFacts{unread: `writes a ` + u + ` escape, which the compiler's JSONPath parser cannot read; ` + fix}
	}
	foreign := selectorFacts{foreign: `writes a ` + u + ` escape of other than four hexadecimal digits naming a character, ` +
		`which RFC 9535 refuses`}
	tests := map[string]selectorFacts{
		u + "00e9":               unread("write the character it names, U+00E9 'é', itself"),
		u + "263A":               unread("write the character it names, U+263A '☺', itself"),
		u + "D834" + u + "DD1E":  unread("write the character it names, U+1D11E '𝄞', itself"),
		u + "D7FF" + u + "E000":  unread("write the character it names, U+D7FF, itself"),
		u:                        foreign,
		u + "12":                 foreign,
		u + "+123":               foreign,
		u + "{1234}":             foreign,
		u + "D800":               foreign,
		u + "DC00":               foreign,
		u + "D800" + u + "1234":  foreign,
		u + "D800" + u + "D800":  foreign,
		u + "D800x" + u + "DC00": foreign,
		u + "D800" + u + "DC0":   foreign,
	}
	for escape, want := range tests {
		selector := `$["` + escape + `"]`
		got := scanSelector(selector)
		if want.foreign != "" {
			// A well-formed escape after a malformed one is noted too; the
			// malformed one refuses the selector first.
			got.unread = ""
		}
		assert.Equal(t, want, got, "%q", selector)
	}
}

// TestSpelledOtherwise_NamesAFixForEachCharacter pins the fix a refusal of a
// \u escape names: the character itself where a string may hold it, the
// two-character escape where RFC 9535 gives one, the closing quote and the
// backslash escaped, and no fix for a control character with no escape the
// parser reads, the carriage return among them.
func TestSpelledOtherwise_NamesAFixForEachCharacter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		r     rune
		quote byte
		want  string
	}{
		{'é', '\'', "write the character it names, U+00E9 'é', itself"},
		{'"', '\'', `write the character it names, U+0022 '"', itself`},
		{'\'', '\'', `write \' instead`},
		{'"', '"', `write \" instead`},
		{'\\', '"', `write \\ instead`},
		{'\n', '\'', `write \n instead`},
		{'\t', '"', `write \t instead`},
		{'\b', '"', `write \b instead`},
		{'\f', '"', `write \f instead`},
		{'\r', '"', "no other spelling of that character is one it reads"},
		{0x01, '"', "no other spelling of that character is one it reads"},
		{0x200B, '"', "write the character it names, U+200B, itself"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, spelledOtherwise(tc.r, tc.quote), "%U in %c", tc.r, tc.quote)
	}
}
