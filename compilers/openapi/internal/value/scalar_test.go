// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package value_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// classifyRows is every way a scalar is written that classification turns on:
// tag × style × spelling. Each row is parsed from its YAML spelling, so the tag
// and style are the ones a parse gives, and names the kind and value it reads
// as, or that it is refused. annotation's renderer agreement test reads the
// same rows through both renderers.
var classifyRows = []struct {
	name, src string
	kind      value.ScalarKind
	num       ir.BigVal
	text      string
	refused   bool
}{
	{name: "plain null", src: "null", kind: value.ScalarNull, text: "null"},
	{name: "tilde null", src: "~", kind: value.ScalarNull, text: "~"},
	{name: "empty null", src: "!!null ''", kind: value.ScalarNull},
	{name: "null tag over text", src: "!!null abc", refused: true},
	{name: "plain true", src: "true", kind: value.ScalarBool, text: "true"},
	{name: "bool tag over text", src: "!!bool maybe", refused: true},
	{name: "plain int", src: "42", kind: value.ScalarNumber, num: "42", text: "42"},
	{name: "leading-zero int", src: "0644", kind: value.ScalarNumber, num: "644", text: "0644"},
	{name: "hex int", src: "0x1f", kind: value.ScalarNumber, num: "31", text: "0x1f"},
	{name: "separated int", src: "1_000", kind: value.ScalarNumber, num: "1000", text: "1_000"},
	{name: "plain float", src: "1.5", kind: value.ScalarNumber, num: "1.5", text: "1.5"},
	{name: "float past float64", src: "1.8e308", kind: value.ScalarNumber, num: "1.8e308", text: "1.8e308"},
	{name: "negative past float64", src: "-1.8e308", kind: value.ScalarNumber, num: "-1.8e308", text: "-1.8e308"},
	{name: "exponent past float64", src: "1e400", kind: value.ScalarNumber, num: "1e400", text: "1e400"},
	{name: "exponent at the bound", src: "1e999999999999999999", kind: value.ScalarNumber,
		num: "1e999999999999999999", text: "1e999999999999999999"},
	{name: "exponent past the bound", src: "1e1000000000000000000", refused: true},
	{name: "negative exponent past the bound", src: "-1e-1000000000000000000", refused: true},
	{name: "float tag past the bound", src: "!!float 1e1000000000000000000", refused: true},
	{name: "quoted past the bound", src: `"1e1000000000000000000"`, kind: value.ScalarString,
		text: "1e1000000000000000000"},
	{name: "infinity", src: ".inf", refused: true},
	{name: "not a number", src: ".nan", refused: true},
	{name: "int tag over text", src: "!!int 1.5", refused: true},
	{name: "float tag over text", src: "!!float abc", refused: true},
	{name: "plain string", src: "hello", kind: value.ScalarString, text: "hello"},
	{name: "quoted number", src: `"1.8e308"`, kind: value.ScalarString, text: "1.8e308"},
	{name: "single-quoted number", src: "'42'", kind: value.ScalarString, text: "42"},
	{name: "string tag over a number", src: "!!str 42", kind: value.ScalarString, text: "42"},
	{name: "merge text as a value", src: "<<", kind: value.ScalarString, text: "<<"},
	{name: "plain timestamp", src: "2021-1-1", kind: value.ScalarTimestamp, text: "2021-1-1"},
	{name: "timestamp tag over text", src: "!!timestamp notadate", refused: true},
	{name: "binary", src: "!!binary aGVsbG8=", kind: value.ScalarBinary, text: "aGVsbG8="},
	{name: "binary tag over text", src: "!!binary '###'", refused: true},
	{name: "a document's own tag", src: "!acme thing", kind: value.ScalarTagged, text: "thing"},
}

// TestClassify_ReadsEachSpellingOnce holds Classify to classifyRows: the kind
// each spelling reads as, the number a number holds, and the text kept beside
// it, or that the text is refused for its tag.
func TestClassify_ReadsEachSpellingOnce(t *testing.T) {
	t.Parallel()
	for _, row := range classifyRows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			got, err := value.Classify(openapitest.YAMLNode(t, row.src))
			if row.refused {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, row.kind, got.Kind)
			assert.Equal(t, row.num, got.Num)
			assert.Equal(t, row.text, got.Text)
		})
	}
}

// TestClassify_RefusesAPlainNumberPastTheExponentRange pins a plain scalar
// YAML resolves to a string for being past float64's range, where no BigVal
// holds its power of ten either: it is refused as the number it is, as its
// !!float spelling is, never read as text (docs/compilers/openapi.md#values).
func TestClassify_RefusesAPlainNumberPastTheExponentRange(t *testing.T) {
	t.Parallel()
	node := openapitest.YAMLNode(t, "1e1000000000000000000")
	require.Equal(t, "!!str", node.ShortTag(), "the premise: YAML reads the literal as a string")
	_, err := value.Classify(node)
	require.ErrorIs(t, err, ir.ErrExponentRange)
}

// TestClassify_ReadsTheValueOfItsKind pins the values a bool and a binary
// carry, and the refusal of a node that is no scalar.
func TestClassify_ReadsTheValueOfItsKind(t *testing.T) {
	t.Parallel()
	b, err := value.Classify(openapitest.YAMLNode(t, "false"))
	require.NoError(t, err)
	assert.False(t, b.Bool)
	b, err = value.Classify(openapitest.YAMLNode(t, "true"))
	require.NoError(t, err)
	assert.True(t, b.Bool)

	bin, err := value.Classify(openapitest.YAMLNode(t, "!!binary aGVsbG8="))
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), bin.Bytes)

	_, err = value.Classify(openapitest.YAMLNode(t, "[a]"))
	require.Error(t, err)
	_, err = value.Classify(nil)
	require.Error(t, err)
}

// TestClassify_ResolvesAnUntaggedNodeByItsText pins ShortTag: a node built by
// hand carries no tag, and is read as a parse would have tagged its text.
func TestClassify_ResolvesAnUntaggedNodeByItsText(t *testing.T) {
	t.Parallel()
	got, err := value.Classify(&yaml.Node{Kind: yaml.ScalarNode, Value: "7"})
	require.NoError(t, err)
	assert.Equal(t, value.ScalarNumber, got.Kind)
	assert.Equal(t, ir.BigVal("7"), got.Num)
}

// TestNumericLiteral_YAMLBases pins the rule that a numeric scalar is captured
// as the value YAML 1.2's core schema resolves, not as the base-10 reading of
// its source text. The two differ for every prefixed spelling, and a leading
// zero is no prefix: 0644 is 644.
func TestNumericLiteral_YAMLBases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      ir.BigVal
	}{
		{"leading zero is base ten", "0644", "644"},
		{"leading zero negative is base ten", "-017", "-17"},
		{"octal 1.2 spelling", "0o17", "15"},
		{"hex", "0x1f", "31"},
		{"hex negative", "-0x1f", "-31"},
		{"binary", "0b1010", "10"},
		{"separators", "1_000", "1000"},
		{"plain decimal", "42", "42"},
		{"leading plus", "+42", "42"},
		{"above int64", "18446744073709551615", "18446744073709551615"},
		// A float keeps its text: decoding it would round it through float64.
		{"float leading zero", "09", "9"},
		{"float separators", "1_000.5", "1000.5"},
		{"float precision", "1.23456789012345678901234567890", "1.23456789012345678901234567890"},
		{"float beyond int64", "123456789012345678901234567890", "123456789012345678901234567890"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := value.NumericLiteral(openapitest.YAMLNode(t, tc.src))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNumericLiteral_ExplicitIntBeyondUint64 covers a magnitude past uint64
// under an explicit !!int tag: yaml.v3 resolves one that big to !!float, so it
// only reaches an !!int node when the source says so and nothing can decode it.
// Its plain-decimal text means what it says, so it survives to the last digit
// rather than being dropped for being undecodable.
func TestNumericLiteral_ExplicitIntBeyondUint64(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      ir.BigVal
	}{
		{"positive", "!!int 99999999999999999999999", "99999999999999999999999"},
		{"negative", "!!int -99999999999999999999999", "-99999999999999999999999"},
		{"separators", "!!int 99_999_999_999_999_999_999_999", "99999999999999999999999"},
		{"plus sign", "!!int +99999999999999999999999", "99999999999999999999999"},
		// A leading zero is no base prefix, and 0x reads exactly at any width
		// within the digits read: no base is guessed.
		{"leading zero", "!!int 077777777777777777777777", "77777777777777777777777"},
		{"negative leading zero", "!!int -077777777777777777777777", "-77777777777777777777777"},
		{"hex past uint64", "!!int 0x1FFFFFFFFFFFFFFFFFFFF", "2417851639229258349412351"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := value.NumericLiteral(openapitest.YAMLNode(t, tc.src))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNumericLiteral_NilNode covers the nil guard.
func TestNumericLiteral_NilNode(t *testing.T) {
	t.Parallel()
	_, err := value.NumericLiteral(nil)
	require.Error(t, err)
}

// TestNumericLiteral_UndecodableInt covers !!int spellings no reading
// recovers: a non-numeric run, and a base prefix behind a sign, which the core
// schema does not spell, past what the parser decodes. Neither may fall back
// to its source text.
func TestNumericLiteral_UndecodableInt(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"12abc", "",
		// Valid decimal literals, and no integer: an !!int spelling must be a
		// digit run, whatever a decimal grammar would accept.
		"1e30", "1.5", "+-5",
		// A base prefix behind a sign.
		"-0x1FFFFFFFFFFFFFFFFFFFF",
	} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			_, err := value.NumericLiteral(openapitest.ScalarNode("!!int", src))
			require.Error(t, err)
		})
	}
}

// TestNumericLiteral_NonNumericTagReadsAsDecimal covers the tag this package
// does not recognise as numeric. A quoted bound reaches it — YAML resolves
// `minimum: "10"` to !!str — and carries no YAML-assigned base, so its text is
// the decimal it looks like rather than something to reinterpret.
func TestNumericLiteral_NonNumericTagReadsAsDecimal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tag  string
		val  string
		want ir.BigVal
	}{
		{name: "a quoted integer", tag: "!!str", val: "10", want: "10"},
		{name: "a quoted decimal", tag: "!!str", val: "1.5", want: "1.5"},
		// The leading zero is not an octal prefix at this tag, and BigVal is a
		// canonical decimal, so it is dropped rather than reinterpreted — 0644 is
		// 644 here, as it is under !!int.
		{name: "a leading zero is dropped, not read as octal", tag: "!!str", val: "0644", want: "644"},
		{name: "a signed value", tag: "!!str", val: "-7", want: "-7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := value.NumericLiteral(openapitest.ScalarNode(tc.tag, tc.val))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
