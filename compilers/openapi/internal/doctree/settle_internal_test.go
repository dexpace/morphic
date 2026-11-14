// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package doctree

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
)

// TestYAML11Reading_ReadsEachTypeByItsPublishedExpression holds the reading of
// a plain scalar by YAML 1.1's types to each form of each type, and to the
// spellings that are none and so a string.
func TestYAML11Reading_ReadsEachTypeByItsPublishedExpression(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"": "null", "~": "null", "Yes": "the boolean true", "OFF": "the boolean false",
		"0b1_01": "the integer 5", "0x_1F": "the integer 31", "0": "the integer 0", "017": "the integer 15",
		"-12": "the integer -12", "1_000": "the integer 1000", "1:30": "the integer 90", "-1:30": "the integer -90",
		"1.5": "the number 1.5", "-.5e+3": "the number -500", "1_.5": "the number 1.5", "1:30.5": "the number 90.5",
		".inf": "positive infinity", "-.Inf": "negative infinity", ".NaN": "NaN",
		"0b": "a string", "0x": "a string", "1:60": "a string", ":30": "a string", "1e3": "a string",
		"1.e3": "a string", "1:30.5e+3": "a string", "x1.5": "a string", "a:1.5": "a string", "1.5.5": "a string",
		".": "a string", "1.5e+x": "a string", "1:3a": "a string", "0b12": "a string", "1a:30": "a string",
		"0b_": "the integer 0",
	} {
		assert.Equal(t, want, yaml11Reading(text).String(), "%q", text)
	}
}

// TestCoreReading_ReadsTheTagResolveGave holds YAML 1.2's reading of a plain
// scalar to its tag, and a core integer the parser read as a float to the
// integer it is.
func TestCoreReading_ReadsTheTagResolveGave(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ tag, text, want string }{
		{"!!null", "~", "null"}, {"!!bool", "True", "the boolean true"},
		{"!!int", "0o17", "the integer 15"}, {"!!int", "0x1f", "the integer 31"}, {"!!int", "-7", "the integer -7"},
		{"!!float", ".inf", "positive infinity"}, {"!!float", "1.5", "the number 1.5"},
		{"!!float", "089", "the integer 89"}, {"!!str", "089", "a string"},
	} {
		n := &yaml.Node{Kind: yaml.ScalarNode, Tag: tc.tag, Value: tc.text}
		assert.Equal(t, tc.want, coreReading(n).String(), "%s %q", tc.tag, tc.text)
	}
}

// TestReading_WordsANumberExactly holds a number's wording to its exact value,
// spelled out unless that would be long, and an exponent past the bound to the
// text written.
func TestReading_WordsANumberExactly(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"0.000": "the number 0", "1.5e3": "the number 1500", "1.25": "the number 1.25",
		"0.0025": "the number 0.0025", "1e1000": "the number 1" + strings.Repeat("0", 1000),
		"1e-1000": "the number 0." + strings.Repeat("0", 999) + "1",
		"1e1001":  "the number 1e1001", "1e99999999999999999999": "the number 1e99999999999999999999",
	} {
		r, ok := decimalOf(false, text)
		require.True(t, ok)
		assert.Equal(t, want, r.String(), "%q", text)
	}
	big := reading{kind: asNumber, text: "15", exp: 1001, neg: true}
	assert.Equal(t, "the number -15e1001", big.String())
	assert.True(t, reading{kind: asNumber, text: "5", integer: true}.same(reading{kind: asNumber, text: "5"}))
}

// TestCore_MatchesOnlyTheCoreSchemasForms holds the two core matchers to the
// forms the core schema spells, and the digit reader to hexadecimal.
func TestCore_MatchesOnlyTheCoreSchemasForms(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"0o", "0o8", "0x", "0x1G", "", "+", "1_0"} {
		assert.False(t, coreInt(text), "%q", text)
	}
	for _, text := range []string{"0o7", "0xaF", "-01", "+9"} {
		assert.True(t, coreInt(text), "%q", text)
	}
	for _, text := range []string{".nan", "-.INF", "1.", ".5", "+1.5e-3", "1e3", "007"} {
		assert.True(t, coreFloat(text), "%q", text)
	}
	for _, text := range []string{"1e", "1e+", ".", "e3", "1.x", "-.nan", "1._5"} {
		assert.False(t, coreFloat(text), "%q", text)
	}
	assert.Equal(t, 16, digitValue('g'))
}

// TestRespell_EdgesOfWhatItReads holds what respell leaves alone: a slash with
// no placeholder free, a truncated character, escape or percent run, and a
// directive the parser would refuse.
func TestRespell_EdgesOfWhatItReads(t *testing.T) {
	t.Parallel()
	var every strings.Builder
	for r := rune(firstPlaceholder); r < firstPlaceholder+placeholders; r++ {
		every.WriteRune(r)
	}
	full := "a: \"x" + bs + "/y\"\nb: " + every.String() + "\n"
	out, r, _ := respell([]byte(full))
	assert.Equal(t, full, string(out), "no placeholder free leaves the slash as written")
	assert.Zero(t, r.slash)

	assert.Zero(t, spelled([]byte("\xee\x80")), "a character cut short")
	assert.Zero(t, spelled([]byte(`%EE%8G%80 \uZZZZ \u12`)), "a run or an escape that is no hexadecimal")

	for _, line := range []string{"%YAMLx 1.2", "%YAML", "%YAML 1.", "%YAML 1.+2", "%YAML 1.2 x", "%YAML 1.2#c", "%YAML 1.x"} {
		_, _, ok := yamlDirective([]byte(line), 1)
		assert.False(t, ok, "%q", line)
	}
	d, _, ok := yamlDirective([]byte("%YAML\t1.2 # c"), 3)
	require.True(t, ok)
	assert.Equal(t, Directive{Minor: 2, Line: 3}, d)

	dirs, _ := directives([]byte("a: 0\r\n... # end\r%YAML 1.2\r\n---\r\nb: 1\r\n"))
	assert.Equal(t, []Directive{{Minor: 2, Line: 3}}, dirs, "lines end at CR LF and at CR alone")
	dirs, out = directives([]byte("%YAML 1.2"))
	assert.Equal(t, []Directive{{Minor: 2, Line: 1}}, dirs, "a last line with no break")
	assert.Equal(t, "%YAML 1.1", string(out))
}

// TestPastNodesError_SaysWhatTheReadKnows holds the error to the two counts a
// read can know.
func TestPastNodesError_SaysWhatTheReadKnows(t *testing.T) {
	t.Parallel()
	whole := &PastNodesError{Nodes: 22, Limit: budget.AtMost(12), Whole: true}
	assert.Equal(t, "the document parses to 22 nodes, past the 12-node budget", whole.Error())
	cut := &PastNodesError{Nodes: 13, Limit: budget.AtMost(12)}
	assert.Equal(t, "the document holds more than 12 nodes, the node budget; reading stopped there", cut.Error())
}

// TestRead_AStreamOfEmptyDocumentsIsItsFirst holds the document taken when no
// document holds content: the first, a null.
func TestRead_AStreamOfEmptyDocumentsIsItsFirst(t *testing.T) {
	t.Parallel()
	doc, err := Read(t.Context(), []byte("---\n---\n"), budget.Unbounded())
	require.NoError(t, err)
	require.Equal(t, yaml.DocumentNode, doc.Root.Kind)
	assert.Equal(t, "!!null", doc.Root.Content[0].ShortTag())
	assert.Equal(t, 2, doc.Root.Content[0].Line)
}

// TestLegacy_ALongScalarIsReadByKindAlone holds the two readings of a plain
// scalar past maxReadText to their kinds, in allocations that do not grow with
// its text, for each form whose value a base change would build: octal, hex,
// binary and base 60 under YAML 1.1, and a core hex integer.
func TestLegacy_ALongScalarIsReadByKindAlone(t *testing.T) {
	// AllocsPerRun counts the whole process, so the test runs alone.
	forms := map[string]func(n int) string{
		"octal":   func(n int) string { return "0" + strings.Repeat("7", n) },
		"hex":     func(n int) string { return "0x" + strings.Repeat("f", n) },
		"binary":  func(n int) string { return "0b" + strings.Repeat("1", n) },
		"base 60": func(n int) string { return "1" + strings.Repeat(":59", n/3) },
	}
	for name, form := range forms {
		allocs := func(n int) float64 {
			text := form(n)
			return testing.AllocsPerRun(3, func() {
				yaml11Reading(text)
				coreInteger("0x" + strings.Repeat("a", n))
			})
		}
		assert.Equal(t, allocs(2*maxReadText), allocs(200*maxReadText), name)
	}

	doc := read(t, "%YAML 1.1\n---\na: 1_"+strings.Repeat("0", maxReadText)+"\n")
	require.Len(t, doc.Legacy, 1)
	assert.Equal(t, Legacy{Node: doc.Legacy[0].Node, Was: "an integer", Is: "a string", Kinds: true}, doc.Legacy[0])
	assert.Equal(t, "a string", reading{kind: asString}.kindOnly().String())
	assert.Equal(t, "a number", reading{kind: asOther, text: "NaN"}.kindOnly().String())
}

// read reads text with no node budget, which it must read.
func read(t *testing.T, text string) Document {
	t.Helper()
	doc, err := Read(t.Context(), []byte(text), budget.Unbounded())
	require.NoError(t, err)
	return doc
}

// TestSettle_StopsOnceTheContextIsDone holds the walk after the parse to its
// context: a done one ends it with the context's error and sets nothing.
func TestSettle_StopsOnceTheContextIsDone(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: [1, 2, 3]\n"), &root))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	doc := Document{Root: &root}

	err := settle(ctx, &doc, respelling{})

	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, doc.Nodes, "nothing counted is reported")
}

// TestRead_ANumberLikeKeyKeepsTheParsersTagAndText holds every mapping key,
// those the core schema reads as numbers and those it reads as strings, to the
// text written and the tag the parser gave it: no key is resolved.
func TestRead_ANumberLikeKeyKeepsTheParsersTagAndText(t *testing.T) {
	t.Parallel()
	const text = "k: {007: a, 1: b, 01: c, 0x10: d, 1e3: e, 0b11: f, 1_000: g, 0X1F: h, +0x10: i}\n"
	var own yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(text), &own))

	doc := read(t, text)

	got, want := doc.Root.Content[0].Content[1].Content, own.Content[0].Content[1].Content
	require.Len(t, got, len(want))
	for i := 0; i < len(got); i += 2 {
		assert.Equal(t, want[i].Value, got[i].Value, "the key's text")
		assert.Equal(t, want[i].Tag, got[i].Tag, "%s keeps the parser's tag", want[i].Value)
	}
	assert.Equal(t, "!!int", got[10].Tag, "0b11, a string to the core schema, is the parser's integer as a key")
}
