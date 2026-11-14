// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package doctree_test

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/internal/ctxtest"
)

// The characters a text file cannot hold safely, spelled as bytes, and the
// backslash, so no escape is ever written where a tool might fold it.
const (
	nel = "\xc2\x85"
	ls  = "\xe2\x80\xa8"
	ps  = "\xe2\x80\xa9"
	e0  = "\xee\x80\x80"
	bom = "\xef\xbb\xbf"
	bs  = "\\"

	minDoc = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"
)

// read reads text with no node budget, which it must read.
func read(t testing.TB, text string) doctree.Document {
	t.Helper()
	doc, err := doctree.Read(t.Context(), []byte(text), budget.Unbounded())
	require.NoError(t, err, "Read reads %q", text)
	return doc
}

// at walks from n, a document node or a root, through each step: a key of a
// mapping or an index of a sequence. It returns the value node.
func at(t testing.TB, n *yaml.Node, steps ...string) *yaml.Node {
	t.Helper()
	require.NotNil(t, n)
	if n.Kind == yaml.DocumentNode {
		require.Len(t, n.Content, 1)
		n = n.Content[0]
	}
	for _, step := range steps {
		switch n.Kind {
		case yaml.MappingNode:
			var next *yaml.Node
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == step {
					next = n.Content[i+1]
				}
			}
			require.NotNil(t, next, "no key %q", step)
			n = next
		case yaml.SequenceNode:
			i, err := strconv.Atoi(step)
			require.NoError(t, err)
			require.Less(t, i, len(n.Content))
			n = n.Content[i]
		default:
			require.Failf(t, "not a collection", "step %q of a node of kind %d", step, n.Kind)
		}
	}
	return n
}

// dump lists every node under n in document order as its kind, tag, value and
// position, so two trees are equal when their dumps are.
func dump(n *yaml.Node) []string {
	return collect(n, func(n *yaml.Node) string {
		return fmt.Sprintf("%d %s %q @%d:%d", n.Kind, n.Tag, n.Value, n.Line, n.Column)
	})
}

// positions lists the line and column of every node under n.
func positions(n *yaml.Node) []string {
	return collect(n, func(n *yaml.Node) string { return fmt.Sprintf("@%d:%d", n.Line, n.Column) })
}

// collect lists what describe says of every node under n, in document order.
func collect(n *yaml.Node, describe func(*yaml.Node) string) []string {
	var out []string
	var visit func(*yaml.Node)
	visit = func(n *yaml.Node) {
		out = append(out, describe(n))
		for _, c := range n.Content {
			visit(c)
		}
	}
	visit(n)
	return out
}

// TestRead_AJSONTextPastTheNodeBudgetStopsWhereItCrosses holds a JSON text to
// the node budget as it is read: the error is the first node past the budget,
// and what follows that node is never looked at.
func TestRead_AJSONTextPastTheNodeBudgetStopsWhereItCrosses(t *testing.T) {
	t.Parallel()
	const text = `{"openapi":"3.1.0","info":{"title":"t","version":"1"},"paths":{},"x-p":[0,0,0,0,0,0,0,0]}`
	// The same text cut after its thirteenth node, the key "x-p", and then
	// something no JSON reader accepts.
	const cut = `{"openapi":"3.1.0","info":{"title":"t","version":"1"},"paths":{},"x-p"@@@`

	whole, err := doctree.Read(t.Context(), []byte(text), budget.Unbounded())
	require.NoError(t, err)
	assert.Equal(t, 22, whole.Nodes, "the document node and every node under it, each once")
	_, err = doctree.Read(t.Context(), []byte(text), budget.AtMost(22))
	require.NoError(t, err, "a document of exactly the budget is within it")

	for name, data := range map[string]string{"the whole text": text, "the text cut at the thirteenth node": cut} {
		_, err := doctree.Read(t.Context(), []byte(data), budget.AtMost(12))

		var past *doctree.PastNodesError
		require.ErrorAs(t, err, &past, name)
		assert.Equal(t, 13, past.Nodes, "%s: the first node past the budget", name)
		assert.False(t, past.Whole, "%s: reading stopped there, so the count is not the document's", name)
		most, bounded := past.Limit.Max()
		assert.True(t, bounded, name)
		assert.Equal(t, 12, most, name)
	}
}

// TestRead_AYAMLTextPastTheNodeBudgetIsCountedWhole holds a YAML text to the same
// budget once it is parsed, and says the count is the whole document's.
func TestRead_AYAMLTextPastTheNodeBudgetIsCountedWhole(t *testing.T) {
	t.Parallel()
	const text = "openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\nx-p: [0, 0, 0, 0, 0, 0, 0, 0]\n"

	whole := read(t, text)
	assert.Equal(t, 22, whole.Nodes, "a YAML text and its JSON twin hold the same nodes")
	_, err := doctree.Read(t.Context(), []byte(text), budget.AtMost(22))
	require.NoError(t, err)

	_, err = doctree.Read(t.Context(), []byte(text), budget.AtMost(12))

	var past *doctree.PastNodesError
	require.ErrorAs(t, err, &past)
	assert.Equal(t, 22, past.Nodes)
	assert.True(t, past.Whole)
	most, _ := past.Limit.Max()
	assert.Equal(t, 12, most)
}

// TestRead_AnAliasIsCountedAsItself holds the count to its definition: an alias
// is one node and what it names is not counted again.
func TestRead_AnAliasIsCountedAsItself(t *testing.T) {
	t.Parallel()

	doc := read(t, "a: &x [1, 2, 3]\nb: *x\n")

	// document, mapping, a, sequence, 3 items, b, alias
	assert.Equal(t, 9, doc.Nodes)
}

// TestRead_ATailIsWhatTheStreamHoldsPastItsFirstDocument pins what Rest says of
// a stream: how many documents past the first hold content, where the first
// begins, and the three ways counting can end.
func TestRead_ATailIsWhatTheStreamHoldsPastItsFirstDocument(t *testing.T) {
	t.Parallel()
	const ovFirst = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\nactions:\n  - target: $.info\n    update: {description: first}\n"
	const ovSecond = "overlay: 1.0.0\ninfo: {title: o2, version: \"1\"}\nactions:\n  - target: $.info\n    update: {x-second: dropped}\n"
	tests := []struct {
		name, text string
		want       doctree.Tail
		first      string // a key of the document taken
	}{
		{"a second document of an overlay", ovFirst + "---\n" + ovSecond,
			doctree.Tail{Line: 7, Column: 1, Dropped: 1}, "overlay"},
		{"a leading empty document is skipped", "---\n---\n" + ovFirst, doctree.Tail{}, "overlay"},
		{"a trailing separator holds nothing", minDoc + "---\n", doctree.Tail{}, "openapi"},
		{"one document", minDoc, doctree.Tail{}, "openapi"},
		{"1023 content documents", minDoc + strings.Repeat("---\nfoo: 1\n", 1023),
			doctree.Tail{Line: 5, Column: 1, Dropped: 1023}, "openapi"},
		{"1024 content documents are all read", minDoc + strings.Repeat("---\nfoo: 1\n", 1024),
			doctree.Tail{Line: 5, Column: 1, Dropped: 1024}, "openapi"},
		{"1025 content documents run past the bound", minDoc + strings.Repeat("---\nfoo: 1\n", 1025),
			doctree.Tail{Line: 5, Column: 1, Dropped: 1024, Capped: true}, "openapi"},
		{"1024 content documents then one that does not parse",
			minDoc + strings.Repeat("---\nfoo: 1\n", 1024) + "---\n: : :\n",
			doctree.Tail{Line: 5, Column: 1, Dropped: 1024, Unparsed: true}, "openapi"},
		{"one content document then one that does not parse", minDoc + "---\nfoo: 1\n---\n: : :\n",
			doctree.Tail{Line: 5, Column: 1, Dropped: 1, Unparsed: true}, "openapi"},
		{"a document that does not parse and nothing before it", minDoc + "---\n: : :\n",
			doctree.Tail{Unparsed: true}, "openapi"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := read(t, tc.text)

			assert.Equal(t, tc.want, doc.Rest)
			at(t, doc.Root, tc.first)
		})
	}
}

// TestRead_AStreamOfEmptyDocumentsPastTheBoundIsStillReported holds the bound to
// what the stream does past it: 1,025 empty documents then one with content
// leaves a tail that says reading was cut, and says no document holds content.
func TestRead_AStreamOfEmptyDocumentsPastTheBoundIsStillReported(t *testing.T) {
	t.Parallel()

	doc := read(t, minDoc+strings.Repeat("---\n", 1025)+"---\nfoo: bar\n")

	assert.Equal(t, doctree.Tail{Capped: true}, doc.Rest, "nothing counted, and the bound ended the count")
	at(t, doc.Root, "openapi")
}

// TestRead_AStreamHoldingNoDocumentHasARootOfNoKind holds the root of a stream
// with nothing in it, and of a JSON text, which has no tail.
func TestRead_AStreamHoldingNoDocumentHasARootOfNoKind(t *testing.T) {
	t.Parallel()

	empty := read(t, "")
	require.NotNil(t, empty.Root)
	assert.Equal(t, yaml.Kind(0), empty.Root.Kind)

	jsonDoc := read(t, `{"a": 1}`)
	assert.Equal(t, doctree.Tail{}, jsonDoc.Rest)
	assert.Nil(t, jsonDoc.Directive)
}

// TestRead_ADirectiveIsTheOneGoverningTheDocumentTaken pins which %YAML
// directive Document.Directive reports: one in the prologue or after a
// document end, on its own line, governing the document taken and no other.
func TestRead_ADirectiveIsTheOneGoverningTheDocumentTaken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text string
		want       *doctree.Directive
	}{
		{"none", minDoc, nil},
		{"1.1", "%YAML 1.1\n---\na: 1\n", &doctree.Directive{Minor: 1, Line: 1}},
		{"1.2", "%YAML 1.2\n---\n" + minDoc, &doctree.Directive{Minor: 2, Line: 1}},
		{"1.2 after a byte order mark", bom + "%YAML 1.2\n---\n" + minDoc, &doctree.Directive{Minor: 2, Line: 1}},
		{"1.3 is read as a later minor", "%YAML 1.3\n---\n" + minDoc, &doctree.Directive{Minor: 3, Line: 1}},
		{"1.10 is the tenth minor", "%YAML 1.10\n---\n" + minDoc, &doctree.Directive{Minor: 10, Line: 1}},
		{"after a comment", "# c\n%YAML 1.1\n---\na: 1\n", &doctree.Directive{Minor: 1, Line: 2}},
		{"of a later document, which governs nothing the tree holds", minDoc + "...\n%YAML 1.1\n---\nfoo: 1\n", nil},
		{"written in a scalar's middle line",
			"openapi: 3.1.0\ninfo:\n  title: t\n  version: \"1\"\n  description: \"a\n%YAML 1.3\n  b\"\npaths: {}\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := read(t, tc.text)

			assert.Equal(t, tc.want, doc.Directive)
		})
	}

	later := read(t, minDoc+"...\n%YAML 1.2\n---\nfoo: 1\n")
	assert.Equal(t, doctree.Tail{Line: 7, Column: 1, Dropped: 1}, later.Rest,
		"a later document opening with a directive is read and counted, not left unparsed")

	middle := read(t, "openapi: 3.1.0\ninfo:\n  title: t\n  version: \"1\"\n  description: \"a\n%YAML 1.3\n  b\"\npaths: {}\n")
	assert.Equal(t, "a %YAML 1.3 b", at(t, middle.Root, "info", "description").Value,
		"a line that starts with the directive's spelling inside a scalar is text")

	_, err := doctree.Read(t.Context(), []byte("%YAML 2.0\n---\n"+minDoc), budget.Unbounded())
	require.Error(t, err, "another major version is the lexer's to refuse")
	var past *doctree.PastNodesError
	assert.NotErrorAs(t, err, &past)
}

// TestRead_AYAML11DocumentRecordsWhatItReadsOtherwise pins Document.Legacy: in
// a document declaring YAML 1.1, each plain value scalar YAML 1.1 reads as
// another JSON value than YAML 1.2 does, and each line-break character YAML 1.1
// ends a line at, in document order.
func TestRead_AYAML11DocumentRecordsWhatItReadsOtherwise(t *testing.T) {
	t.Parallel()
	const body = "openapi: 3.1.0\ninfo: {title: t, version: \"1\", description: \"a" + nel + "b\"}\npaths: {}\n" +
		"components:\n  schemas:\n    S:\n      type: object\n      properties:\n" +
		"        m: {type: integer, default: 0644}\n" +
		"        e: {type: string, enum: [yes, n, abc]}\n" +
		"        t: {type: string, default: 1:30}\n" +
		"        o: {type: integer, default: 0o17}\n" +
		"        f: {type: number, default: 1e3}\n" +
		"        h: {type: string, default: 0X1F}\n" +
		"        y: {type: string}\n"

	doc := read(t, "%YAML 1.1\n---\n"+body)

	type entry struct {
		value     string
		brk       rune
		inComment bool
	}
	got := make([]entry, 0, len(doc.Legacy))
	for _, l := range doc.Legacy {
		got = append(got, entry{l.Node.Value, l.Break, l.InComment})
	}
	want := []entry{
		{"a" + nel + "b", 0x85, false},
		{"0644", 0, false},
		{"yes", 0, false},
		{"n", 0, false},
		{"1:30", 0, false},
		{"0o17", 0, false},
		{"1e3", 0, false},
	}
	assert.Equal(t, want, got, "in document order; nothing for 0X1F, abc, the quoted version, the key y or 3.1.0")

	for _, text := range []string{body, "%YAML 1.2\n---\n" + body, "%YAML 1.3\n---\n" + body} {
		assert.Empty(t, read(t, text).Legacy, "a document that does not declare YAML 1.1 records nothing")
	}
}

// TestRead_AYAML11DocumentRecordsABreakInAComment pins the comment half of
// Legacy: a character YAML 1.1 ends a line at, in a comment attached to a node.
func TestRead_AYAML11DocumentRecordsABreakInAComment(t *testing.T) {
	t.Parallel()

	doc := read(t, "%YAML 1.1\n---\na: 1 # c"+ls+"d\n")

	require.Len(t, doc.Legacy, 1)
	assert.True(t, doc.Legacy[0].InComment)
	assert.Equal(t, rune(0x2028), doc.Legacy[0].Break)
}

// TestRead_ACoreScalarIsRetaggedAndNeverRewritten holds the resolution of a plain
// scalar to YAML 1.2's core schema: only its tag is the compiler's reading, its
// text is what the source wrote, and a key is left as it was.
func TestRead_ACoreScalarIsRetaggedAndNeverRewritten(t *testing.T) {
	t.Parallel()
	text := minDoc + "components:\n  schemas:\n    S:\n      type: integer\n" +
		"      maxLength: 0b11\n      default: 001\n      minimum: 0o17\n      maximum: +0x1F\n" +
		"      multipleOf: 089\n      example: 2021-01-01\n" +
		"      enum: [1e3, .5, -.inf, 0X1F, 1_0.5, 0o1_7, -007, +12]\n      const: !!int 0644\n"
	tests := []struct {
		path       []string
		tag, value string
	}{
		{[]string{"maxLength"}, "!!str", "0b11"},
		{[]string{"default"}, "!!int", "001"},
		{[]string{"minimum"}, "!!int", "0o17"},
		{[]string{"maximum"}, "!!str", "+0x1F"},
		{[]string{"multipleOf"}, "!!float", "089"},
		{[]string{"example"}, "!!str", "2021-01-01"},
		{[]string{"enum", "0"}, "!!float", "1e3"},
		{[]string{"enum", "1"}, "!!float", ".5"},
		{[]string{"enum", "2"}, "!!float", "-.inf"},
		{[]string{"enum", "3"}, "!!str", "0X1F"},
		{[]string{"enum", "4"}, "!!str", "1_0.5"},
		{[]string{"enum", "5"}, "!!str", "0o1_7"},
		{[]string{"enum", "6"}, "!!int", "-007"},
		{[]string{"enum", "7"}, "!!int", "+12"},
		{[]string{"const"}, "!!int", "0644"},
	}

	doc := read(t, text)

	for _, tc := range tests {
		n := at(t, doc.Root, append([]string{"components", "schemas", "S"}, tc.path...)...)
		assert.Equal(t, tc.tag, n.ShortTag(), "%v", tc.path)
		assert.Equal(t, tc.value, n.Value, "%v: the text is what the source wrote", tc.path)
	}
}

// TestRead_AMergeKeyStillMergesAndAMergeValueIsText holds the one exception to
// the core schema, a << key, and its mirror: a << where a value stands is a
// string.
func TestRead_AMergeKeyStillMergesAndAMergeValueIsText(t *testing.T) {
	t.Parallel()

	doc := read(t, "base: &b {p: 1}\nuses:\n  <<: *b\n  q: 2\nvalue: <<\n")

	assert.Equal(t, "!!merge", doc.Root.Content[0].Content[3].Content[0].ShortTag(), "the key keeps merging")
	assert.Equal(t, "!!str", at(t, doc.Root, "value").ShortTag())
}

// TestRead_NoKeyAndNoValueIsRewrittenToItsNumber holds the first design's
// failure: a key such as 007 beside a required entry "007" and a value
// version: 007 keep the text they were written in.
func TestRead_NoKeyAndNoValueIsRewrittenToItsNumber(t *testing.T) {
	t.Parallel()

	doc := read(t, minDoc+"components:\n  schemas:\n    S:\n      required: [\"007\", 007]\n"+
		"      properties:\n        007: {type: string}\n        1: {type: string}\n        01: {type: integer}\n")

	props := at(t, doc.Root, "components", "schemas", "S", "properties")
	var keys []string
	for i := 0; i < len(props.Content); i += 2 {
		keys = append(keys, props.Content[i].Value)
	}
	assert.Equal(t, []string{"007", "1", "01"}, keys)
	req := at(t, doc.Root, "components", "schemas", "S", "required")
	assert.Equal(t, "007", req.Content[0].Value)
	assert.Equal(t, "007", req.Content[1].Value)
}

// TestRead_AJSONTextIsNeverResolvedAgain holds the tags of a JSON text to the
// ones the parser gives them: the core step runs on YAML alone, so an integer
// past 64 bits stays the float the parser calls it.
func TestRead_AJSONTextIsNeverResolvedAgain(t *testing.T) {
	t.Parallel()

	doc := read(t, `{"openapi":"3.1.0","info":{"title":"t","version":"1"},"paths":{},"x-n":[1E3,-0,0.5e-2,99999999999999999999]}`)

	n := at(t, doc.Root, "x-n")
	got := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		got = append(got, c.Value+" "+c.ShortTag())
	}
	assert.Equal(t, []string{"1E3 !!float", "-0 !!int", "0.5e-2 !!float", "99999999999999999999 !!float"}, got)
}

// TestRead_AJSONTextReadsAlikeInEveryEncoding holds a JSON text in UTF-16 and
// UTF-32 to the tree its UTF-8 gives: it is read as JSON, so what only the JSON
// reader reads, a surrogate pair of escapes and a key past the parser's length,
// reads in each encoding.
func TestRead_AJSONTextReadsAlikeInEveryEncoding(t *testing.T) {
	t.Parallel()
	text := `{"openapi":"3.1.0","info":{"title":"a` + bs + `ud83d` + bs + `ude00b","version":"1"},` +
		`"paths":{},"x-` + strings.Repeat("k", 1100) + `":1}`
	want := read(t, text)

	for name, enc := range map[string]struct {
		mark      string
		width     int
		bigEndian bool
	}{
		"UTF-16LE": {"\xff\xfe", 2, false}, "UTF-16BE": {"\xfe\xff", 2, true},
		"UTF-32LE": {"\xff\xfe\x00\x00", 4, false}, "UTF-32BE": {"\x00\x00\xfe\xff", 4, true},
	} {
		in := []byte(enc.mark)
		for _, r := range text {
			in = append(in, encoded(r, enc.width, enc.bigEndian)...)
		}

		got, err := doctree.Read(t.Context(), in, budget.Unbounded())

		require.NoError(t, err, name)
		assert.Empty(t, cmp.Diff(dump(want.Root), dump(got.Root)), name)
		assert.Equal(t, want.Nodes, got.Nodes, name)
	}
}

// encoded is r in UTF-16 or UTF-32, as width names, in the byte order
// bigEndian names. r is in the Basic Multilingual Plane when width is 2.
func encoded(r rune, width int, bigEndian bool) []byte {
	out := make([]byte, width)
	for i := range width {
		shift := 8 * (width - 1 - i)
		if !bigEndian {
			shift = 8 * i
		}
		out[i] = byte(r >> shift)
	}
	return out
}

// TestRead_AnEscapedSlashReadsAsASlashOnlyWhereTheStyleSaysSo pins what an
// escaped slash, which YAML 1.2 allows and the library refuses, reads as in each
// style: a slash in a double-quoted scalar, the two characters anywhere else,
// and an even run of backslashes before a slash is text. Every node stays where
// the text writes it.
func TestRead_AnEscapedSlashReadsAsASlashOnlyWhereTheStyleSaysSo(t *testing.T) {
	t.Parallel()
	text := "a: 'x" + bs + "/y'\nb: x" + bs + "/y\nc: |\n  x" + bs + "/y\nd: 1 # c" + bs + "/d\n" +
		"e: \"x" + bs + bs + "/y\"\nf: \"x" + bs + bs + bs + "/y\"\n"
	twin := strings.ReplaceAll(text, bs, "q")

	doc := read(t, text)

	assert.Equal(t, "x"+bs+"/y", at(t, doc.Root, "a").Value, "single-quoted")
	assert.Equal(t, "x"+bs+"/y", at(t, doc.Root, "b").Value, "plain")
	assert.Equal(t, "x"+bs+"/y\n", at(t, doc.Root, "c").Value, "block")
	assert.Equal(t, "1", at(t, doc.Root, "d").Value)
	assert.Equal(t, "# c"+bs+"/d", at(t, doc.Root, "d").LineComment, "a comment is text")
	assert.Equal(t, "x"+bs+"/y", at(t, doc.Root, "e").Value, "an escaped backslash, then a slash")
	assert.Equal(t, "x"+bs+"/y", at(t, doc.Root, "f").Value, "an escaped backslash, then an escaped slash")
	assert.Empty(t, cmp.Diff(positions(read(t, twin).Root), positions(doc.Root)), "positions as the one-for-one twin's")

	d := read(t, "t: \"x"+bs+bs+bs+"x61y\"\nu: \"p"+bs+"/q\"\n")
	assert.Equal(t, "x"+bs+"ay", at(t, d.Root, "t").Value, "an escaped backslash then the escape of a")
	assert.Equal(t, "p/q", at(t, d.Root, "u").Value)
}

// TestRead_ALineBreakYAML11EndsALineAtIsACharacter pins the three characters
// YAML 1.1 ends a line at and YAML 1.2 does not, in a double-quoted, a
// single-quoted, a plain and a block scalar and in a key: each is kept in the
// text, and no node's position moves.
func TestRead_ALineBreakYAML11EndsALineAtIsACharacter(t *testing.T) {
	t.Parallel()
	text := "a: \"p" + nel + "q\"\nb: 'p" + nel + "q'\nc: p" + ls + "q\nd: |\n  p" + ps + "q\n  r\np" + nel + "k: 1\nz: 2\n"
	twin := strings.NewReplacer(nel, "-", ls, "-", ps, "-").Replace(text)

	doc := read(t, text)

	assert.Equal(t, "p"+nel+"q", at(t, doc.Root, "a").Value)
	assert.Equal(t, "p"+nel+"q", at(t, doc.Root, "b").Value)
	assert.Equal(t, "p"+ls+"q", at(t, doc.Root, "c").Value)
	assert.Equal(t, "p"+ps+"q\nr\n", at(t, doc.Root, "d").Value)
	assert.Equal(t, "p"+nel+"k", doc.Root.Content[0].Content[8].Value, "the key")
	z := at(t, doc.Root, "z")
	assert.Equal(t, [2]int{8, 4}, [2]int{z.Line, z.Column}, "z's value is on line 8")
	assert.Equal(t, 8, doc.Root.Content[0].Content[10].Line, "z's key is on line 8, where the text writes it")
	assert.Equal(t, 1, doc.Root.Content[0].Content[10].Column)
	assert.Empty(t, cmp.Diff(positions(read(t, twin).Root), positions(doc.Root)), "positions as the twin with a hyphen for each break")
}

// TestRead_APlaceholderNeverShowsInATree holds the respelling invisible: whatever
// the text spells, no string of the tree holds a character the compiler chose
// to stand for another.
func TestRead_APlaceholderNeverShowsInATree(t *testing.T) {
	t.Parallel()
	const tail = "\n  version: \"1\"\n  description: 'a" + nel + "b'\npaths: {}\n"
	titles := map[string]string{
		"an escape":                 "\"x" + bs + "uE000y\"",
		"an escape in lower case":   "\"x" + bs + "ue000y\"",
		"a long escape":             "\"x" + bs + "U0000E000y\"",
		"the character itself":      "\"x" + e0 + "y\"",
		"an escape beside a tag":    "\"x" + bs + "uE000y\"\n  x-t: !e%EE%80%80 v",
		"the character beside both": "\"x" + e0 + "y\"\n  x-t: !e%EE%80%80 v",
	}
	for name, title := range titles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc := read(t, "openapi: 3.1.0\ninfo:\n  title: "+title+tail)

			assert.Equal(t, "x"+e0+"y", at(t, doc.Root, "info", "title").Value)
			assert.Equal(t, "a"+nel+"b", at(t, doc.Root, "info", "description").Value)
			if strings.Contains(title, "x-t") {
				assert.Equal(t, "!e"+e0, at(t, doc.Root, "info", "x-t").Tag, "a percent run in a tag is the character it decodes to")
			}
		})
	}
}

// TestRead_ATextHoldingEveryPlaceholderIsTheParsersOwn states a limit: when the
// text leaves no placeholder free, a break character is left as written and
// the parser reads it its own way.
func TestRead_ATextHoldingEveryPlaceholderIsTheParsersOwn(t *testing.T) {
	t.Parallel()
	var every strings.Builder
	for r := rune(0xE000); r < 0xE040; r++ {
		every.WriteRune(r)
	}

	doc := read(t, "a: \"p"+nel+"q\"\nb: "+every.String()+"\n")

	assert.Equal(t, "p q", at(t, doc.Root, "a").Value, "a raw U+0085 in a double-quoted scalar reads as a space")
	assert.Equal(t, every.String(), at(t, doc.Root, "b").Value)
}

// TestRead_ASurrogatePairEscapeInYAMLIsTheLexersToRefuse pins a stated limit: a
// pair of surrogate escapes in YAML text is refused by the lexer, as YAML 1.2
// defines \u as one character and joins no pair.
func TestRead_ASurrogatePairEscapeInYAMLIsTheLexersToRefuse(t *testing.T) {
	t.Parallel()

	_, err := doctree.Read(t.Context(),
		[]byte("openapi: 3.1.0\ninfo: {title: \"a"+bs+"uD83D"+bs+"uDE00b\", version: \"1\"}\npaths: {}\n"), budget.Unbounded())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "found invalid Unicode character escape code")
}

// TestRead_StopsAtADoneContextWithNoPartialTree puts a cancel at each check of
// the context Read makes, over a JSON and a YAML text that need many: the answer
// is the context's error and no tree, or the tree an uncancelled read gives.
func TestRead_StopsAtADoneContextWithNoPartialTree(t *testing.T) {
	t.Parallel()
	items := 4 * nodeview.AskEvery
	jsonText := `{"a":[` + strings.Repeat("0,", items) + `0]}`
	yamlText := "a: \"p" + bs + "/q\"\nl:\n" + strings.Repeat("  - 0\n", items)

	for name, text := range map[string]string{"JSON": jsonText, "YAML": yamlText} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ref := read(t, text)

			live := &ctxtest.Countdown{Context: t.Context(), Left: math.MaxInt}
			_, err := doctree.Read(live, []byte(text), budget.Unbounded())
			require.NoError(t, err)
			asks := live.Calls
			require.GreaterOrEqual(t, asks, 4, "a read of %d nodes asks about once per %d", items, nodeview.AskEvery)

			for left := range asks {
				cut := &ctxtest.Countdown{Context: t.Context(), Left: left}
				doc, err := doctree.Read(cut, []byte(text), budget.Unbounded())

				require.ErrorIs(t, err, context.Canceled, "cut at ask %d of %d", left, asks)
				assert.Nil(t, doc.Root, "no partial tree")
			}
			whole := &ctxtest.Countdown{Context: t.Context(), Left: asks}
			doc, err := doctree.Read(whole, []byte(text), budget.Unbounded())
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(dump(ref.Root), dump(doc.Root)))
		})
	}
}
