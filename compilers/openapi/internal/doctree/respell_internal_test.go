// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package doctree

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
)

// The bytes a text file cannot hold safely, spelled out, and the backslash.
const (
	nel = "\xc2\x85"
	ls  = "\xe2\x80\xa8"
	ps  = "\xe2\x80\xa9"
	e0  = "\xee\x80\x80"
	e1  = "\xee\x80\x81"
	bs  = "\\"
)

// TestRespell_ATextWithNoTriggerIsReturnedAsItIs holds the common path: a text
// with no break character, no escaped slash and no directive comes back as the
// slice it was given, with nothing to restore.
func TestRespell_ATextWithNoTriggerIsReturnedAsItIs(t *testing.T) {
	t.Parallel()
	in := []byte("openapi: 3.1.0\ninfo: {title: \"a" + bs + "\\b\", version: \"1\"}\n")

	out, r, dirs := respell(in)

	assert.Same(t, &in[0], &out[0], "no copy is made")
	assert.Equal(t, respelling{}, r)
	assert.Empty(t, dirs)
}

// TestRespell_APlaceholderIsTheLowestOneNoSpellingOfTheTextCanMake holds the
// choice of a placeholder: the lowest private-use code point from U+E000 that
// the text cannot make a string hold, by writing it raw, by an escape of any
// of the three lengths in either case, or by a percent run in a tag.
func TestRespell_APlaceholderIsTheLowestOneNoSpellingOfTheTextCanMake(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text string
		want       rune
	}{
		{"nothing spelled", "a: \"p" + nel + "q\"\n", 0xE000},
		{"the character written raw", "a: \"p" + nel + "q\"\nb: " + e0 + "\n", 0xE001},
		{"an escape", "a: \"p" + nel + "q\"\nb: \"" + bs + "uE000\"\n", 0xE001},
		{"an escape in lower case", "a: \"p" + nel + "q\"\nb: \"" + bs + "ue000\"\n", 0xE001},
		{"a long escape", "a: \"p" + nel + "q\"\nb: \"" + bs + "U0000E000\"\n", 0xE001},
		{"an escape beside an unquoted copy", "a: \"p" + nel + "q\"\nb: \"" + bs + "uE000\"\nc: x" + bs + "uE000y\n", 0xE001},
		{"a percent run in a tag", "a: \"p" + nel + "q\"\nb: !e%EE%80%80 v\n", 0xE001},
		{"two taken", "a: \"p" + nel + "q\"\nb: " + e0 + e1 + "\n", 0xE002},
		{"an escape of the second", "a: \"p" + nel + "q\"\nb: \"" + bs + "uE000\"\nc: \"" + bs + "uE001\"\n", 0xE002},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, r, _ := respell([]byte(tc.text))

			assert.Equal(t, tc.want, r.breaks[0])
			assert.Zero(t, r.breaks[1])
			assert.Zero(t, r.breaks[2])
			assert.NotContains(t, string(out), nel, "the break is gone from what the parser reads")
		})
	}
}

// TestRespell_EachTriggerGetsAPlaceholderOfItsOwn holds the placeholders of
// the three breaks and of the escaped slash apart from each other and from
// everything the text spells.
func TestRespell_EachTriggerGetsAPlaceholderOfItsOwn(t *testing.T) {
	t.Parallel()
	text := "a: \"p" + nel + "q\"\nb: p" + ls + "q\nc: |\n  p" + ps + "q\nd: \"x" + bs + "/y\"\ne: \"" + bs + "uE000\"\n"

	out, r, _ := respell([]byte(text))

	got := []rune{r.breaks[0], r.breaks[1], r.breaks[2], r.slash}
	seen := map[rune]bool{}
	for _, p := range got {
		assert.GreaterOrEqual(t, p, rune(0xE001), "U+E000 is spelled by an escape")
		assert.False(t, seen[p], "placeholders are distinct: %U", got)
		seen[p] = true
	}
	for _, c := range []string{nel, ls, ps, bs + "/"} {
		assert.NotContains(t, string(out), c)
	}
}

// TestRespell_AUTF16OrUTF32TextIsReadAsTheUTF8ItEncodes holds a text opening
// with a UTF-16 or UTF-32 byte order mark to the UTF-8 it encodes, respelled as
// that UTF-8 is, and one that encodes no text, a truncated unit or half a
// surrogate pair, to the parser, handed back as it is.
func TestRespell_AUTF16OrUTF32TextIsReadAsTheUTF8ItEncodes(t *testing.T) {
	t.Parallel()
	const text = "a: \"x" + bs + "/y\"\n%YAML 1.3\nb: \U0001F600\n"
	encode := func(mark string, width int, bigEndian bool) []byte {
		out := []byte(mark)
		for _, r := range text {
			units := []rune{r}
			if width == 2 {
				units = units[:0]
				for _, u := range utf16.Encode([]rune{r}) {
					units = append(units, rune(u))
				}
			}
			for _, u := range units {
				out = append(out, unitBytes(u, width, bigEndian)...)
			}
		}
		return out
	}
	want, wantR, _ := respell([]byte(text))
	for name, in := range map[string][]byte{
		"UTF-16LE": encode("\xff\xfe", 2, false), "UTF-16BE": encode("\xfe\xff", 2, true),
		"UTF-32LE": encode("\xff\xfe\x00\x00", 4, false), "UTF-32BE": encode("\x00\x00\xfe\xff", 4, true),
	} {
		out, r, dirs := respell(in)

		assert.Equal(t, string(want), string(out), name)
		assert.Equal(t, wantR, r, name)
		assert.Empty(t, dirs, "%s: %%YAML after content is no directive", name)
	}

	for name, in := range map[string][]byte{
		"a truncated unit":           []byte("\xff\xfea\x00b"),
		"half a surrogate pair":      []byte("\xff\xfe\x3d\xd8a\x00"),
		"a code point past U+10FFFF": []byte("\xff\xfe\x00\x00\x00\x00\x11\x00"),
	} {
		out, r, dirs := respell(in)

		assert.Same(t, &in[0], &out[0], "%s: the input slice itself", name)
		assert.Equal(t, respelling{}, r, name)
		assert.Empty(t, dirs, name)
	}
}

// unitBytes is the code unit u in width bytes, in the byte order bigEndian
// names.
func unitBytes(u rune, width int, bigEndian bool) []byte {
	out := make([]byte, width)
	for i := range width {
		b := byte(u >> (8 * (width - 1 - i)))
		if bigEndian {
			out[i] = b
		} else {
			out[width-1-i] = b
		}
	}
	return out
}

// TestRespell_ATextSpellingEveryCandidateLeavesItsTriggerAsWritten states the
// cap: sixty-four candidates are tried, and a text spelling all of them leaves
// its break as written for the parser to read its own way.
func TestRespell_ATextSpellingEveryCandidateLeavesItsTriggerAsWritten(t *testing.T) {
	t.Parallel()
	var every strings.Builder
	for r := rune(0xE000); r < 0xE040; r++ {
		every.WriteRune(r)
	}
	text := "a: \"p" + nel + "q\"\nb: " + every.String() + "\n"

	out, r, _ := respell([]byte(text))

	assert.Equal(t, text, string(out))
	assert.Zero(t, r.breaks[0])

	// One candidate left free is taken.
	free := strings.Replace(text, string(rune(0xE03F)), "", 1)
	_, r, _ = respell([]byte(free))
	assert.Equal(t, rune(0xE03F), r.breaks[0], "the sixty-fourth candidate is the last tried")
}

// TestRespell_ADirectiveIsRecordedAndAMinorPastOneIsRewrittenToOnePointOne holds
// step two of the respelling: where a %YAML line counts, what is recorded, and
// the one-for-one rewrite of a version the lexer would refuse.
func TestRespell_ADirectiveIsRecordedAndAMinorPastOneIsRewrittenToOnePointOne(t *testing.T) {
	t.Parallel()
	const body = "---\na: 1\n"
	tests := []struct {
		name, text, wantOut string
		want                []Directive
	}{
		{"1.1 is read as it is", "%YAML 1.1\n" + body, "%YAML 1.1\n" + body, []Directive{{Minor: 1, Line: 1}}},
		{"1.2", "%YAML 1.2\n" + body, "%YAML 1.1\n" + body, []Directive{{Minor: 2, Line: 1}}},
		{"1.3", "%YAML 1.3\n" + body, "%YAML 1.1\n" + body, []Directive{{Minor: 3, Line: 1}}},
		{"1.10 is padded to its width", "%YAML 1.10\n" + body, "%YAML 1.1 \n" + body, []Directive{{Minor: 10, Line: 1}}},
		{"after a byte order mark", "\xef\xbb\xbf%YAML 1.2\n" + body, "\xef\xbb\xbf%YAML 1.1\n" + body, []Directive{{Minor: 2, Line: 1}}},
		{"after a comment and a blank line", "# c\n\n%YAML 1.2\n" + body, "# c\n\n%YAML 1.1\n" + body, []Directive{{Minor: 2, Line: 3}}},
		{"after a document end", "a: 0\n...\n%YAML 1.2\n" + body, "a: 0\n...\n%YAML 1.1\n" + body, []Directive{{Minor: 2, Line: 3}}},
		{"another major is the lexer's", "%YAML 2.0\n" + body, "%YAML 2.0\n" + body, nil},
		{"inside a scalar it is text", "a: \"x\n%YAML 1.3\n  y\"\n", "a: \"x\n%YAML 1.3\n  y\"\n", nil},
		{"after content with no document end it is no directive", "a: 0\n%YAML 1.3\n", "a: 0\n%YAML 1.3\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, _, dirs := respell([]byte(tc.text))

			assert.Equal(t, tc.wantOut, string(out))
			assert.Len(t, out, len(tc.text), "one character for one")
			assert.Equal(t, tc.want, dirs)
		})
	}
}

// spells reports whether text can make a string of the tree hold r: written
// raw, as a \u or \U escape in either case, or in the UTF-8 decoding of a run of
// percent-escaped bytes, the way a tag spells one.
func spells(text string, r rune) bool {
	if strings.ContainsRune(text, r) {
		return true
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, fmt.Sprintf(`\u%04x`, r)) || strings.Contains(lower, fmt.Sprintf(`\u%08x`, r)) {
		return true
	}
	for i := range len(text) {
		run := percentRun(text[i:])
		for len(run) > 0 {
			c, size := utf8.DecodeRune(run)
			if c == r {
				return true
			}
			run = run[size:]
		}
	}
	return false
}

// percentRun decodes the run of %HH escapes text opens with.
func percentRun(text string) []byte {
	var run []byte
	for len(text) >= 3 && text[0] == '%' {
		b, err := strconv.ParseUint(text[1:3], 16, 8)
		if err != nil {
			break
		}
		run = append(run, byte(b))
		text = text[3:]
	}
	return run
}

// tree parses text as the parser does, or returns its error's text.
func tree(text []byte) (*yaml.Node, string) {
	var doc yaml.Node
	if err := yaml.Unmarshal(text, &doc); err != nil {
		return nil, err.Error()
	}
	return &doc, ""
}

// walk calls f on every node under n.
func walk(n *yaml.Node, f func(*yaml.Node)) {
	f(n)
	for _, c := range n.Content {
		walk(c, f)
	}
}

// restored parses a respelled text and restores its tree under r. The error is
// the parser's, as text, when it refuses the text.
func restored(text []byte, r respelling) (*yaml.Node, string) {
	doc, msg := tree(text)
	if doc == nil {
		return nil, msg
	}
	walk(doc, r.restore)
	return doc, ""
}

// strs lists every string of a tree that text can hold: values, tags, anchors
// and the three comments.
func strs(doc *yaml.Node) []string {
	var out []string
	walk(doc, func(n *yaml.Node) {
		out = append(out, n.Value, n.Tag, n.Anchor, n.HeadComment, n.LineComment, n.FootComment)
	})
	return out
}

// shape lists each node's kind, value and position, which a tag change of the
// core schema leaves alone.
func shape(n *yaml.Node) []string {
	var out []string
	walk(n, func(n *yaml.Node) {
		out = append(out, fmt.Sprintf("%d %q @%d:%d", n.Kind, n.Value, n.Line, n.Column))
	})
	return out
}

// FuzzRespell_PlaceholdersAreInvisible holds the respelling to what it
// promises, over arbitrary text: reading with the placeholders swapped for
// others the text cannot spell gives the same tree or the same refusal, no
// string of the tree holds a placeholder the text does not, and a text with no
// trigger is the parser's own tree.
func FuzzRespell_PlaceholdersAreInvisible(f *testing.F) {
	for _, seed := range []string{
		"openapi: 3.1.0\ninfo: {title: \"a" + bs + "/b\", version: \"1\"}\n",
		"a: 'x" + bs + "/y'\nb: x" + bs + "/y\nc: |\n  x" + bs + "/y\nd: 1 # c" + bs + "/d\ne: \"x" + bs + bs + "/y\"\nf: \"x" + bs + bs + bs + "/y\"\n",
		"t: \"x" + bs + bs + bs + "x61y\"\nu: \"p" + bs + "/q\"\n",
		"a: \"p" + nel + "q\"\nb: 'p" + nel + "q'\nc: p" + ls + "q\nd: |\n  p" + ps + "q\n  r\np" + nel + "k: 1\nz: 2\n",
		"a: 1 # c" + nel + "paths: {}\n",
		"title: \"x" + bs + "uE000y\"\ndescription: 'a" + nel + "b'\n",
		"title: \"x" + bs + "ue000y\"\ndescription: 'a" + nel + "b'\n",
		"title: \"x" + bs + "U0000E000y\"\ndescription: 'a" + nel + "b'\n",
		"title: \"x" + e0 + "y\"\ndescription: 'a" + nel + "b'\n",
		"x-t: !e%EE%80%80 v\ndescription: 'a" + nel + "b'\n",
		"a: \"" + bs + "/" + bs + "/\"\n",
		"a: " + bs + "/\n# " + bs + "/\n",
		"\xfe\xff\xe0\x30",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		out, r, _ := respell([]byte(text))
		// A UTF-16 or UTF-32 text spells what the UTF-8 it encodes spells.
		written := text
		if utf8Text, ok := transcoded([]byte(text)); ok {
			written = string(utf8Text)
		}
		placeholders := slicesOf(r)
		first, firstErr := restored(out, r)

		// No placeholder the text does not spell may show in the tree.
		if first != nil {
			for _, s := range strs(first) {
				for _, c := range s {
					if c >= 0xE000 && c < 0xE040 {
						assert.True(t, spells(written, c), "%U shows in the tree of %q and the text cannot spell it", c, text)
					}
				}
			}
		}

		// The same read under a disjoint range of placeholders.
		const base = 0xF100
		swapped := out
		moved := r
		next := rune(base)
		for _, p := range placeholders {
			if spells(written, next) {
				t.Skip("the alternative range is spelled by the text")
			}
			swapped = bytes.ReplaceAll(swapped, []byte(string(p)), []byte(string(next)))
			moved = replacePlaceholder(moved, p, next)
			next++
		}
		second, secondErr := restored(swapped, moved)

		assert.Equal(t, firstErr != "", secondErr != "", "both refuse the text or neither does: %q", text)
		if first != nil && second != nil {
			assert.Empty(t, cmp.Diff(strs(first), strs(second)), "the trees differ under another range of placeholders: %q", text)
			assert.Empty(t, cmp.Diff(shape(first), shape(second)), "positions: %q", text)
		}

		// A text with no trigger is the parser's own tree, but for tags.
		if bytes.Equal(out, []byte(text)) && !strings.ContainsAny(text, "%") && !strings.Contains(text, "---") && !strings.Contains(text, "...") {
			own, ownErr := tree([]byte(text))
			if ownErr == "" && first != nil {
				assert.Empty(t, cmp.Diff(shape(own), shape(first)), "%q", text)
			}
		}
	})
}

// slicesOf lists the placeholders r holds, in a fixed order, leaving out what
// it does not use.
func slicesOf(r respelling) []rune {
	out := make([]rune, 0, 4)
	for _, p := range []rune{r.breaks[0], r.breaks[1], r.breaks[2], r.slash} {
		if p != 0 {
			out = append(out, p)
		}
	}
	return out
}

// replacePlaceholder returns r with the placeholder from standing as to.
func replacePlaceholder(r respelling, from, to rune) respelling {
	for i, p := range r.breaks {
		if p == from {
			r.breaks[i] = to
		}
	}
	if r.slash == from {
		r.slash = to
	}
	return r
}

// TestReadPlaceholders_TheFuzzTargetSeesTheTextOnlyThroughRead pins that the
// fuzz target above and Read agree on a seed: the tree Read gives and the
// one the target restores hold the same strings.
func TestReadPlaceholders_TheFuzzTargetSeesTheTextOnlyThroughRead(t *testing.T) {
	t.Parallel()
	text := "a: \"p" + bs + "/q\"\nb: 'p" + nel + "q'\n"

	out, r, _ := respell([]byte(text))
	viaRespell, msg := restored(out, r)
	require.Empty(t, msg)
	viaRead, err := Read(t.Context(), []byte(text), budget.Unbounded())
	require.NoError(t, err)

	assert.Empty(t, cmp.Diff(shape(viaRespell), shape(viaRead.Root)))
	assert.Equal(t, "p/q", viaRead.Root.Content[0].Content[1].Value)
}
