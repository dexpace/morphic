// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsontree_test

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/jsontree"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/internal/ctxtest"
)

// esc spells a \uXXXX escape from its four hex digits. The tests build every
// escape through it so that the surrogate halves a row writes are never
// adjacent in the source, where a tool or an editor may fold a pair into the
// character it names.
func esc(hex string) string { return `\u` + hex }

// pair is the escape of U+1F600 as JSON writes it: a surrogate pair.
var pair = esc("d83d") + esc("de00")

// yamlTree is the tree yaml.v3 builds for text, which it must read.
func yamlTree(t testing.TB, text string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.NewDecoder(strings.NewReader(text)).Decode(&doc), "yaml.v3 reads %q", text)
	return &doc
}

// readTree is the tree Read builds for text, which it must read.
func readTree(t testing.TB, text string) *yaml.Node {
	t.Helper()
	doc, err := jsontree.Read(t.Context(), []byte(text))
	require.NoError(t, err, "Read reads %q", text)
	return doc
}

// scalars lists the scalars under n in document order, each as its value at
// line:column, so a row states what it reads and where without a whole tree.
func scalars(n *yaml.Node) []string {
	var out []string
	if n.Kind == yaml.ScalarNode {
		out = append(out, fmt.Sprintf("%s@%d:%d", n.Value, n.Line, n.Column))
	}
	for _, c := range n.Content {
		out = append(out, scalars(c)...)
	}
	return out
}

// firstDifference returns where the trees a and b first differ, in document
// order and every field a node has, or the empty string when they match. It
// walks with a stack of its own, where cmp.Diff over a tree takes time
// exponential in its depth, and a nesting thousands deep is a case worth holding.
func firstDifference(a, b *yaml.Node) string {
	type step struct {
		a, b *yaml.Node
		path string
	}
	stack := []step{{a, b, ""}}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := *p.a, *p.b
		x.Content, y.Content = nil, nil
		if diff := cmp.Diff(x, y); diff != "" {
			return fmt.Sprintf("at %q (-yaml.v3 +Read):\n%s", p.path, diff)
		}
		if len(p.a.Content) != len(p.b.Content) {
			return fmt.Sprintf("at %q: %d children, then %d", p.path, len(p.a.Content), len(p.b.Content))
		}
		for i, child := range slices.Backward(p.a.Content) {
			stack = append(stack, step{child, p.b.Content[i], fmt.Sprintf("%s/%d", p.path, i)})
		}
	}
	return ""
}

// TestRead_BuildsTheTreeYAMLBuilds holds Read to the tree yaml.v3 builds for
// texts both read, in every field a node has: kind, tag, style, value, line and
// column among them. A row is a spelling of the JSON text a field of the tree
// depends on: a line break, a byte order mark, indentation, characters wider
// than a byte, a number yaml.v3 tags apart from another, a name written twice,
// an escape both read, a root that is no object.
func TestRead_BuildsTheTreeYAMLBuilds(t *testing.T) {
	t.Parallel()
	const object = "{\n  \"a\": 1,\n  \"b\": [true, null, \"s\"]\n}\n"
	cases := map[string]string{
		"lf":                          object,
		"crlf":                        strings.ReplaceAll(object, "\n", "\r\n"),
		"lone cr":                     strings.ReplaceAll(object, "\n", "\r"),
		"a byte order mark":           "\xef\xbb\xbf" + object,
		"a byte order mark and lines": "\xef\xbb\xbf\n\n  " + object,
		"tab indentation":             "{\n\t\"a\":\t1,\n\t\"b\": [\t2]\n}",
		"leading blank lines":         "\n\n  \n   {\"a\": 1}",
		"trailing whitespace":         "{\"a\": 1} \n\r\n ",
		"minified":                    `{"a":{"b":[1,2,{"c":null}]},"d":"e"}`,
		"characters wider than one byte before a node": `{"é日本": "ü", "😀": "ß😀😀", "k": 1, "z": [2]}`,
		"characters wider than one byte on a line":     "{\n\"é日本😀\": 1,\n\"é日本😀\": [2]}",
		"integers":                                  `[0, -0, 1, -1, 9223372036854775807, 9223372036854775808, 18446744073709551615]`,
		"an integer past 64 bits":                   `[18446744073709551616, -9223372036854775808, -9223372036854775809]`,
		"floats":                                    `[1.0, 0.5, -1.5e+3, 1E2, 1e2, 1e-4, 100e-2, 1e-400, 0e0]`,
		"a float yaml.v3 reads as a string":         `[1e400, -1e400]`,
		"keywords":                                  `[true, false, null]`,
		"a name written twice":                      `{"a": 1, "a": 2, "b": {"c": 1, "c": 2}}`,
		"a merge key spelled as a name":             `{"<<": {"a": 1}, "b": 2}`,
		"empty collections":                         `{"a": {}, "b": [], "c": [{}, []], "d": ""}`,
		"a root array":                              `[1, "a", null, [true], {"k": "v"}]`,
		"a root array of arrays":                    "[\n[1,2],\n[3,[4]]\n]",
		"a root object":                             `{}`,
		"a root number":                             `42`,
		"a root string":                             `"s"`,
		"a root keyword":                            `true`,
		"a root null":                               `null`,
		"a root number and a newline":               "-1.5\n",
		"escapes both read":                         `["a\"b\\c\nd\r\te\b\f", "` + esc("00e9") + esc("0000") + esc("FFFD") + `"]`,
		"an escaped backslash before u":             `["\\ud83d", "\\u0041"]`,
		"a key with a colon, a comma and a bracket": `{"a:b": 1, "c,d": 2, "[e]": 3, "{f}": 4, "": 5}`,
		"a space and a long string":                 `{"k": "` + strings.Repeat("0123456789 ", 500) + `"}`,
		"a key of 1022 characters":                  `{"` + strings.Repeat("k", 1022) + `": 1}`,
		"nesting by arrays at 2000":                 strings.Repeat("[", 2000) + strings.Repeat("]", 2000),
		"nesting by objects at 2000":                strings.Repeat(`{"a":`, 2000) + "1" + strings.Repeat("}", 2000),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := yamlTree(t, text)

			got := readTree(t, text)

			assert.Empty(t, firstDifference(want, got), "Read builds the tree yaml.v3 builds")
		})
	}
}

// TestRead_ReadsWhatYAMLRefusesOrMisreads pins the texts Read exists for: each
// row is one JSON text that yaml.v3 refuses or reads as another, with the
// scalars Read gives it and where it writes them. The test holds each row to
// what it claims of yaml.v3 as well, so a row yaml.v3 comes to read alike has
// no reason left.
func TestRead_ReadsWhatYAMLRefusesOrMisreads(t *testing.T) {
	t.Parallel()
	const refuses, misreads = true, false
	cases := []struct {
		name    string
		text    string
		refused bool
		want    []string
	}{
		{"a surrogate pair", `{"k":"x` + pair + `y","n":1}`, refuses, []string{"k@1:2", "x😀y@1:6", "n@1:23", "1@1:27"}},
		{"a surrogate pair in upper case", `["` + esc("D83D") + esc("DE00") + `",2]`, refuses, []string{"😀@1:2", "2@1:17"}},
		{"a surrogate pair in mixed case", `["` + esc("d83D") + esc("De00") + `"]`, refuses, []string{"😀@1:2"}},
		{"a surrogate pair of the last plane", `["` + esc("DBFF") + esc("DFFF") + `"]`, refuses, []string{"\U0010FFFF@1:2"}},
		{"a surrogate pair of the first astral plane", `["` + esc("d800") + esc("dc00") + `"]`, refuses, []string{"\U00010000@1:2"}},
		{"two pairs in a row", `["` + pair + pair + `"]`, refuses, []string{"😀😀@1:2"}},
		{"a surrogate pair in a name", `{"k` + pair + `":1,"z":2}`, refuses, []string{"k😀@1:2", "1@1:18", "z@1:20", "2@1:24"}},
		{"a surrogate pair after wide characters", "{\"é日\":\n\"ü" + pair + "\",\"z\":3}", refuses,
			[]string{"é日@1:2", "ü😀@2:1", "z@2:17", "3@2:21"}},
		{"a solidus escape in a value", `["a\/b","c"]`, refuses, []string{"a/b@1:2", "c@1:9"}},
		{"a solidus escape in a name", `{"\/pets":1,"z":2}`, refuses, []string{"/pets@1:2", "1@1:11", "z@1:13", "2@1:17"}},
		{"a name of 1023 characters", `{"` + strings.Repeat("k", 1023) + `": 1, "z": 2}`, refuses,
			[]string{strings.Repeat("k", 1023) + "@1:2", "1@1:1029", "z@1:1032", "2@1:1037"}},
		{"a name of 1100 characters", `{"` + strings.Repeat("k", 1100) + `": 1, "z": 2}`, refuses,
			[]string{strings.Repeat("k", 1100) + "@1:2", "1@1:1106", "z@1:1109", "2@1:1114"}},
		{"a raw DEL", "[\"a\u007fb\",1]", refuses, []string{"a\u007fb@1:2", "1@1:8"}},
		{"a raw C1 control", "[\"a\u0080b\",\"\u009f\"]", refuses, []string{"a\u0080b@1:2", "\u009f@1:8"}},
		{"a raw U+FFFE", "[\"a\ufffeb\"]", refuses, []string{"a\ufffeb@1:2"}},
		{"a raw U+FFFF", "[\"a\uffffb\"]", refuses, []string{"a\uffffb@1:2"}},
		{"a tab before the root", "\t{\"a\": 1}", refuses, []string{"a@1:3", "1@1:8"}},
		{"a tab after the root", "{\"a\": 1}\n\t", refuses, []string{"a@1:2", "1@1:7"}},
		{"a tab after a space before the root", " \t{\"a\": 1}", refuses, []string{"a@1:4", "1@1:9"}},
		{"tabs and blank lines before the root", "\n\t\n\t {\"a\": 1}", refuses, []string{"a@3:4", "1@3:9"}},
		{"a raw NEL", "[\"a\u0085b\",1]", misreads, []string{"a\u0085b@1:2", "1@1:8"}},
		{"a raw line separator", "[\"a\u2028b\",1]", misreads, []string{"a\u2028b@1:2", "1@1:8"}},
		{"a raw paragraph separator", "[\"a\u2029b\",1]", misreads, []string{"a\u2029b@1:2", "1@1:8"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := readTree(t, tc.text)

			assert.Equal(t, tc.want, scalars(got), "what Read reads, and where")

			var theirs yaml.Node
			err := yaml.NewDecoder(strings.NewReader(tc.text)).Decode(&theirs)
			if tc.refused {
				assert.Error(t, err, "yaml.v3 refuses it")
				return
			}
			require.NoError(t, err, "yaml.v3 reads it")
			assert.NotEmpty(t, firstDifference(&theirs, got), "yaml.v3 reads it as another tree")
		})
	}
}

// TestRead_ReportsALoneSurrogateWhereItIsWritten holds a text with an escape
// that names half of a surrogate pair to a *LoneSurrogateError that quotes the
// escape as written and the line and column it begins at, in characters. The
// halves of a pair that is whole are not reported: only a half beside no
// other, or beside a half out of its order.
func TestRead_ReportsALoneSurrogateWhereItIsWritten(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		text         string
		escape       string
		line, column int
	}{
		{"a high half before a character", `{"k":"x` + esc("d83d") + `y"}`, `\ud83d`, 1, 8},
		{"a half that ends the text", `["` + esc("de00"), `\ude00`, 1, 3},
		{"a high half at the end of the string", `["` + esc("d83d") + `"]`, `\ud83d`, 1, 3},
		{"a low half alone", `["a` + esc("de00") + `y"]`, `\ude00`, 1, 4},
		{"a high half before an escape that is no low half", `["` + esc("d83d") + esc("0041") + `"]`, `\ud83d`, 1, 3},
		{"a high half before a high half", `["` + esc("d83d") + esc("d83d") + esc("de00") + `"]`, `\ud83d`, 1, 3},
		{"a pair in its reverse order", `["` + esc("de00") + esc("d83d") + `"]`, `\ude00`, 1, 3},
		{"a half in upper case", `["` + esc("D83D") + `"]`, `\uD83D`, 1, 3},
		{"a half in the first and the last position", `["` + esc("d800") + `"]`, `\ud800`, 1, 3},
		{"a half of the last position", `["` + esc("dfff") + `"]`, `\udfff`, 1, 3},
		{"a half in a name", `{"k` + esc("d83d") + `":1}`, `\ud83d`, 1, 4},
		{"a half after a pair", `["` + pair + esc("d83d") + `"]`, `\ud83d`, 1, 15},
		{"a half after another escape", `["` + esc("0041") + `\n\\` + esc("d83d") + `"]`, `\ud83d`, 1, 13},
		{"a half after an escaped backslash", `["\\` + esc("d83d") + `"]`, `\ud83d`, 1, 5},
		{"a half after earlier values", "[\"x\",\n 1, [\"" + esc("d83d") + "\"]]", `\ud83d`, 2, 7},
		{"a half after wide characters on a later line", "{\"é日\":\n\"ü😀\u0080" + esc("d83d") + "\"}", `\ud83d`, 2, 5},
		{"a half after a carriage return and a line feed", "[\r\n\"x" + esc("d83d") + "\"]", `\ud83d`, 2, 3},
		{"a half after a lone carriage return", "[\r\"x" + esc("d83d") + "\"]", `\ud83d`, 2, 3},
		{"a half after a byte order mark", "\xef\xbb\xbf[\"x" + esc("d83d") + "\"]", `\ud83d`, 1, 4},
		{"a half in a root string", `"` + esc("d83d") + `"`, `\ud83d`, 1, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, err := jsontree.Read(t.Context(), []byte(tc.text))

			var lone *jsontree.LoneSurrogateError
			require.ErrorAs(t, err, &lone)
			assert.Nil(t, doc)
			assert.NotErrorIs(t, err, jsontree.ErrNotJSON, "a refusal, not a decline")
			assert.Equal(t, jsontree.LoneSurrogateError{Escape: tc.escape, Line: tc.line, Column: tc.column}, *lone)
			assert.Equal(t, fmt.Sprintf("line %d, column %d: the escape %s is half of a UTF-16 surrogate pair, "+
				"written without its other half, so it names no character", tc.line, tc.column, tc.escape), err.Error())
		})
	}
}

// TestRead_ReportsExactlyTheSurrogateEscapesAsHalves holds every code unit a
// \uXXXX escape can name, in either case of its hex, to what Read makes of a
// string that holds it alone: the character it names for a unit outside D800 to
// DFFF, and a *LoneSurrogateError quoting the escape for one inside. It pins the
// range of a half at both ends: the decoder decides which escapes it refuses,
// and Read's own check must report every one of them.
func TestRead_ReportsExactlyTheSurrogateEscapesAsHalves(t *testing.T) {
	t.Parallel()
	for unit := range 0x10000 {
		for _, escape := range []string{fmt.Sprintf(`\u%04x`, unit), fmt.Sprintf(`\u%04X`, unit)} {
			doc, err := jsontree.Read(t.Context(), []byte(`["`+escape+`"]`))

			if utf16.IsSurrogate(rune(unit)) {
				var lone *jsontree.LoneSurrogateError
				if !errors.As(err, &lone) || *lone != (jsontree.LoneSurrogateError{Escape: escape, Line: 1, Column: 3}) {
					t.Fatalf("%s: want its escape reported at 1:3, got %v", escape, err)
				}
				continue
			}
			if err != nil || doc.Content[0].Content[0].Value != string(rune(unit)) {
				t.Fatalf("%s: want the character it names, got %v", escape, err)
			}
		}
	}
}

// TestRead_ReadsAnEscapeBesideAFaultAsALoneOneDoes holds a string that holds an
// escape of a code unit outside D800 to DFFF, then a fault the decoder gives up
// at, to the verdict of the same string with the escape written as one
// character: the escape is never the reason, so it is never what Read quotes.
func TestRead_ReadsAnEscapeBesideAFaultAsALoneOneDoes(t *testing.T) {
	t.Parallel()
	faults := []string{`\q"]`, esc("d83d") + `"]`, "\t\"]", esc("ZZZZ") + `"]`, esc("d83d") + esc("0041") + `"]`, `\`}
	for _, fault := range faults {
		want, wantErr := jsontree.Read(t.Context(), []byte(`["a`+fault))
		require.Nil(t, want)
		var wantLone *jsontree.LoneSurrogateError
		wantIsLone := errors.As(wantErr, &wantLone)

		for unit := range 0x10000 {
			if utf16.IsSurrogate(rune(unit)) {
				continue
			}
			_, err := jsontree.Read(t.Context(), []byte(fmt.Sprintf(`["\u%04x`, unit)+fault))

			var lone *jsontree.LoneSurrogateError
			if errors.As(err, &lone) != wantIsLone || (wantIsLone && lone.Escape != wantLone.Escape) {
				t.Fatalf(`%04x before %q: want %v, got %v`, unit, fault, wantErr, err)
			}
		}
	}
}

// TestRead_DeclinesWhatIsNotOneJSONText holds the texts Read leaves to the
// caller, as ErrNotJSON and no document: no value, YAML that is no JSON, a
// JSON value beside another or beside text, and JSON that stops short, that
// the decoder refuses, or that nests past the decoder's bound. The surrogate
// escapes that stay declined are the ones that are no half: a bad digit, a
// cut one, one outside a string, and a fault that only has the digits of one
// beside it.
func TestRead_DeclinesWhatIsNotOneJSONText(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"empty":                        "",
		"a byte order mark alone":      "\xef\xbb\xbf",
		"whitespace":                   " \t\r\n ",
		"separators":                   ", : ,",
		"a YAML block mapping":         "openapi: 3.0.3\ninfo:\n  title: t\n",
		"a YAML flow mapping":          "{openapi: 3.0.3, info: {title: t}}",
		"a YAML flow mapping, quoted":  `{"openapi": 3.0.3}`,
		"JSON and a YAML comment":      "{\"a\": 1}\n# end\n",
		"a YAML comment and JSON":      "# head\n{\"a\": 1}\n",
		"a trailing comma":             `[1,]`,
		"a leading comma":              `[,1]`,
		"a missing comma":              `[1 2]`,
		"single quotes":                `{'a': 1}`,
		"a bare name":                  `{a: 1}`,
		"two values":                   `{} {}`,
		"two values on lines":          "{\"a\": 1}\n{\"b\": 2}\n",
		"a document and a stream":      "{\"a\": 1}\n---\n{\"b\": 2}\n",
		"a document and a bare marker": "{\"a\": 1}\n---\n",
		"a scalar and text":            "1 2",
		"a word":                       "abc",
		"an unclosed object":           `{"a": 1`,
		"an unclosed string":           `["abc`,
		"an unclosed array of values":  `[1, 2,`,
		"a name with no value":         `{"a":`,
		"a closer with no opener":      `]`,
		"mismatched closers":           `{"a": 1]`,
		"a leading zero":               `[01]`,
		"a plus sign":                  `[+1]`,
		"a bare dot":                   `[.5]`,
		"NaN":                          `[NaN]`,
		"a raw newline in a string":    "[\"a\nb\"]",
		"a raw tab in a string":        "[\"a\tb\"]",
		"invalid UTF-8":                "[\"a\xffb\"]",
		"an encoded surrogate":         "[\"a\xed\xa0\x80b\"]",
		"a bad digit in an escape":     `["` + esc("ZZZZ") + `"]`,
		"a cut escape":                 `["` + esc("D83") + `"]`,
		"a cut escape at the end":      `["` + `\uD8`,
		"an unknown escape":            `["\x41"]`,
		"a half outside a string":      `[` + esc("d83d") + `]`,
		"a half as a root":             esc("d83d"),
		"a half after a bad token":     `["a" ` + esc("d83d") + `]`,
		"a half in a single quote":     `['` + esc("d83d") + `']`,
		"a control character before a surrogate's digits": "[\"\tud83d\"]",
		"an unknown escape before a surrogate's digits":   `["\xd83d"]`,
		"a half in a comment":                             "[1] # " + esc("d83d"),
		"a pair after text":                               `abc "` + pair + `"`,
		"nesting past the decoder's":                      strings.Repeat("[", 10001),
		"nesting of objects past":                         strings.Repeat(`{"a":`, 10001),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, err := jsontree.Read(t.Context(), []byte(text))

			require.ErrorIs(t, err, jsontree.ErrNotJSON)
			assert.Nil(t, doc)
			var lone *jsontree.LoneSurrogateError
			assert.NotErrorAs(t, err, &lone, "declined, not refused")
		})
	}
}

// TestRead_ReadsNestingAtTheDecoderBound pins the depth Read reads: the
// decoder's bound itself, 10000 levels, whose tree is read whole.
func TestRead_ReadsNestingAtTheDecoderBound(t *testing.T) {
	t.Parallel()
	const depth = 10000
	doc := readTree(t, strings.Repeat("[", depth)+strings.Repeat("]", depth))

	n, levels := doc, 0
	for len(n.Content) > 0 {
		n, levels = n.Content[0], levels+1
	}
	assert.Equal(t, depth, levels, "one step from the document to the outer array, and one into each inner")
}

// TestRead_StopsOnceTheContextIsDone pins the asks of ctx: before the first
// token, even of a text that holds none, and every nodeview.AskEvery tokens
// after, and ctx's own error, not a decline, once it is done.
func TestRead_StopsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	array := func(items int) []byte { return []byte("[" + strings.Repeat("1,", items-1) + "1]") }

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	for name, text := range map[string][]byte{"an array": array(10), "no text": nil} {
		doc, err := jsontree.Read(cancelled, text)
		require.ErrorIs(t, err, context.Canceled, name)
		assert.NotErrorIs(t, err, jsontree.ErrNotJSON, name)
		assert.Nil(t, doc, name)
	}

	// An array of n items is n+2 tokens, and Read asks again once it has read
	// them all, at the end of the text, so the ask at token n+2 counts.
	for name, tc := range map[string]struct{ items, asks int }{
		"one token short of the second ask": {nodeview.AskEvery - 3, 1},
		"the second ask at the end":         {nodeview.AskEvery - 2, 2},
		"one token past the second ask":     {nodeview.AskEvery - 1, 2},
		"the third ask at the end":          {2*nodeview.AskEvery - 2, 3},
		"four times the interval":           {4 * nodeview.AskEvery, 5},
	} {
		live := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
		_, err := jsontree.Read(live, array(tc.items))
		require.NoError(t, err, name)
		assert.Equal(t, tc.asks, live.Calls, name)
	}

	cut := &ctxtest.Countdown{Context: t.Context(), Left: 2}
	doc, err := jsontree.Read(cut, array(4*nodeview.AskEvery))
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, doc)
	assert.Equal(t, 3, cut.Calls, "two asks passed, the third refused, and none after")
}

// TestRead_KeepsTheDecodersReasonWhenItDeclines holds the error of a text that
// is not JSON to the reason the decoder gave, so a caller who logs it learns why
// the text was read as YAML: the decoder's error is wrapped, not replaced.
func TestRead_KeepsTheDecodersReasonWhenItDeclines(t *testing.T) {
	t.Parallel()
	_, err := jsontree.Read(t.Context(), []byte("[1,]"))
	require.ErrorIs(t, err, jsontree.ErrNotJSON)
	var syntax *jsontext.SyntacticError
	require.ErrorAs(t, err, &syntax)
	assert.Equal(t, int64(2), syntax.ByteOffset, "the comma the decoder stopped at")

	_, err = jsontree.Read(t.Context(), []byte(`{"a": 1`))
	require.ErrorIs(t, err, jsontree.ErrNotJSON)
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF, "the text ended inside its object")
}

// FuzzRead_BuildsTheTreeYAMLBuilds holds Read to yaml.v3 over arbitrary text:
// whenever Read reads a text and yaml.v3 reads it too, the trees match in every
// field. A text only one of them reads says nothing, and neither does one with
// a raw U+0085, U+2028 or U+2029, which yaml.v3 counts as line breaks and Read
// does not (docs/compilers/openapi.md#yaml-decoding). A library release that
// reads a JSON text differently reddens it.
func FuzzRead_BuildsTheTreeYAMLBuilds(f *testing.F) {
	for _, seed := range []string{
		"{\n  \"a\": 1,\n  \"b\": [true, null, \"s\"]\n}\n",
		"\xef\xbb\xbf\r\n{\"é日\": [1e2, -0, 1e400, \"😀\"]}",
		`{"a": 1, "a": {}, "<<": [], "": ""}`,
		`["a\"b\\c\nd` + esc("00e9") + `", 9223372036854775808, 0.5]`,
		"[\r\"x\",\r\n1]", "42", `"s"`, "null", "{}", "[]", "",
		"openapi: 3.0.3\n", "{a: 1} # c\n", "[1,2]\n---\n[3]\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if strings.ContainsAny(string(data), "\u0085\u2028\u2029") {
			t.Skip("a line break yaml.v3 counts and Read does not")
		}
		var want yaml.Node
		if err := yaml.NewDecoder(strings.NewReader(string(data))).Decode(&want); err != nil {
			t.Skip("yaml.v3 refuses it")
		}
		got, err := jsontree.Read(t.Context(), data)
		if err != nil {
			t.Skip("Read declines it")
		}
		if diff := firstDifference(&want, got); diff != "" {
			t.Fatalf("Read builds another tree than yaml.v3 for %q, %s", data, diff)
		}
	})
}
