package value

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// classifyRows is every way a scalar is written that classification turns on:
// tag × style × spelling. Each row is parsed from its YAML spelling, so the tag
// and style are the ones a parse gives, and names the kind and value it reads
// as, or that it is refused. annotation's renderer agreement test reads the
// same rows through both renderers.
var classifyRows = []struct {
	name, src string
	kind      ScalarKind
	num       ir.BigVal
	text      string
	refused   bool
}{
	{name: "plain null", src: "null", kind: ScalarNull, text: "null"},
	{name: "tilde null", src: "~", kind: ScalarNull, text: "~"},
	{name: "empty null", src: "!!null ''", kind: ScalarNull},
	{name: "null tag over text", src: "!!null abc", refused: true},
	{name: "plain true", src: "true", kind: ScalarBool, text: "true"},
	{name: "bool tag over text", src: "!!bool maybe", refused: true},
	{name: "plain int", src: "42", kind: ScalarNumber, num: "42", text: "42"},
	{name: "octal int", src: "0644", kind: ScalarNumber, num: "420", text: "0644"},
	{name: "hex int", src: "0x1f", kind: ScalarNumber, num: "31", text: "0x1f"},
	{name: "separated int", src: "1_000", kind: ScalarNumber, num: "1000", text: "1_000"},
	{name: "plain float", src: "1.5", kind: ScalarNumber, num: "1.5", text: "1.5"},
	{name: "float past float64", src: "1.8e308", kind: ScalarNumber, num: "1.8e308", text: "1.8e308"},
	{name: "negative past float64", src: "-1.8e308", kind: ScalarNumber, num: "-1.8e308", text: "-1.8e308"},
	{name: "exponent past float64", src: "1e400", kind: ScalarNumber, num: "1e400", text: "1e400"},
	{name: "infinity", src: ".inf", refused: true},
	{name: "not a number", src: ".nan", refused: true},
	{name: "int tag over text", src: "!!int 1.5", refused: true},
	{name: "float tag over text", src: "!!float abc", refused: true},
	{name: "plain string", src: "hello", kind: ScalarString, text: "hello"},
	{name: "quoted number", src: `"1.8e308"`, kind: ScalarString, text: "1.8e308"},
	{name: "single-quoted number", src: "'42'", kind: ScalarString, text: "42"},
	{name: "string tag over a number", src: "!!str 42", kind: ScalarString, text: "42"},
	{name: "merge text as a value", src: "<<", kind: ScalarString, text: "<<"},
	{name: "plain timestamp", src: "2021-1-1", kind: ScalarTimestamp, text: "2021-1-1"},
	{name: "timestamp tag over text", src: "!!timestamp notadate", refused: true},
	{name: "binary", src: "!!binary aGVsbG8=", kind: ScalarBinary, text: "aGVsbG8="},
	{name: "binary tag over text", src: "!!binary '###'", refused: true},
	{name: "a document's own tag", src: "!acme thing", kind: ScalarTagged, text: "thing"},
}

// TestClassify_ReadsEachSpellingOnce holds Classify to classifyRows: the kind
// each spelling reads as, the number a number holds, and the text kept beside
// it, or that the text is refused for its tag.
func TestClassify_ReadsEachSpellingOnce(t *testing.T) {
	t.Parallel()
	for _, row := range classifyRows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			got, err := Classify(openapitest.YAMLNode(t, row.src))
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

// TestClassify_ReadsTheValueOfItsKind pins the values a bool and a binary
// carry, and the refusal of a node that is no scalar.
func TestClassify_ReadsTheValueOfItsKind(t *testing.T) {
	t.Parallel()
	b, err := Classify(openapitest.YAMLNode(t, "false"))
	require.NoError(t, err)
	assert.False(t, b.Bool)
	b, err = Classify(openapitest.YAMLNode(t, "true"))
	require.NoError(t, err)
	assert.True(t, b.Bool)

	bin, err := Classify(openapitest.YAMLNode(t, "!!binary aGVsbG8="))
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), bin.Bytes)

	_, err = Classify(openapitest.YAMLNode(t, "[a]"))
	require.Error(t, err)
	_, err = Classify(nil)
	require.Error(t, err)
}

// TestClassify_ResolvesAnUntaggedNodeByItsText pins ShortTag: a node built by
// hand carries no tag, and is read as a parse would have tagged its text.
func TestClassify_ResolvesAnUntaggedNodeByItsText(t *testing.T) {
	t.Parallel()
	got, err := Classify(&yaml.Node{Kind: yaml.ScalarNode, Value: "7"})
	require.NoError(t, err)
	assert.Equal(t, ScalarNumber, got.Kind)
	assert.Equal(t, ir.BigVal("7"), got.Num)
}
