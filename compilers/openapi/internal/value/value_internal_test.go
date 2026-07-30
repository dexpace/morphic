package value

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

func TestValueFromNode_Scalars(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      ir.Value
	}{
		{"null", "null", ir.Value{Kind: ir.ValueNull}},
		{"bool", "true", ir.Value{Kind: ir.ValueBool, Bool: true}},
		{"string", `"hi"`, ir.Value{Kind: ir.ValueString, Str: "hi"}},
		{"int precision", "9007199254740993", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("9007199254740993")}},
		{"big decimal", "0.30000000000000004", ir.Value{Kind: ir.ValueNumber, Num: ir.BigVal("0.30000000000000004")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := FromNode(yamlNode(t, tc.src))
			require.NoError(t, err)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestNumericLiteral_YAMLBases pins the rule that a numeric scalar is captured
// as the value YAML resolved, not as the base-10 reading of its source text. The
// two differ for every prefixed spelling, and reading the text as decimal stores
// a different number with nothing to report it.
func TestNumericLiteral_YAMLBases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      ir.BigVal
	}{
		{"octal 1.1 spelling", "0644", "420"},
		{"octal negative", "-017", "-15"},
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
			got, err := NumericLiteral(yamlNode(t, tc.src))
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NumericLiteral(yamlNode(t, tc.src))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNumericLiteral_NilNode covers the nil guard.
func TestNumericLiteral_NilNode(t *testing.T) {
	t.Parallel()
	_, err := NumericLiteral(nil)
	require.Error(t, err)
}

// TestNumericLiteral_UndecodableInt covers !!int spellings no base-10 reading
// can recover: a non-numeric run, and a prefixed magnitude past uint64 whose
// base would have to be guessed. Neither may fall back to its source text.
func TestNumericLiteral_UndecodableInt(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"12abc", "077777777777777777777777", "0x1FFFFFFFFFFFFFFFFFFFF", ""} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			_, err := NumericLiteral(scalarNode("!!int", src))
			require.Error(t, err)
		})
	}
}

func TestValueFromNode_ObjectPreservesOrder(t *testing.T) {
	t.Parallel()
	got, err := FromNode(yamlNode(t, "b: 1\na: 2\n"))
	require.NoError(t, err)
	require.Equal(t, ir.ValueObject, got.Kind)
	require.Len(t, got.Object, 2)
	require.Equal(t, "b", got.Object[0].Name)
	require.Equal(t, "a", got.Object[1].Name)
}

func TestValueFromNode_NilYieldsNull(t *testing.T) {
	t.Parallel()
	got, err := FromNode(nil)
	require.NoError(t, err)
	assert.Equal(t, ir.ValueNull, got.Kind)
}

func TestValueFromNode_AliasFollowed(t *testing.T) {
	t.Parallel()
	target := scalarNode("!!str", "hi")
	alias := &yaml.Node{Kind: yaml.AliasNode, Alias: target}
	got, err := FromNode(alias)
	require.NoError(t, err)
	assert.Equal(t, ir.ValueString, got.Kind)
	assert.Equal(t, "hi", got.Str)
}

func TestValueFromNode_Sequence(t *testing.T) {
	t.Parallel()
	seq := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
		scalarNode("!!int", "1"), scalarNode("!!str", "x"),
	}}
	got, err := FromNode(seq)
	require.NoError(t, err)
	require.Equal(t, ir.ValueList, got.Kind)
	require.Len(t, got.List, 2)
	assert.Equal(t, ir.BigVal("1"), got.List[0].Num)
	assert.Equal(t, "x", got.List[1].Str)
}

func TestValueFromNode_Binary(t *testing.T) {
	t.Parallel()
	got, err := FromNode(yamlNode(t, "!!binary aGVsbG8="))
	require.NoError(t, err)
	require.Equal(t, ir.ValueBytes, got.Kind)
	assert.Equal(t, []byte("hello"), got.Bytes)
}

func TestValueFromNode_Timestamp(t *testing.T) {
	t.Parallel()
	// YAML 1.1 resolves each of these plain scalars to !!timestamp; the exact
	// source spelling must survive as a string, not get parsed into a Go
	// time.Time or dropped.
	cases := []string{"2021-01-01", "2021-01-01T00:00:00Z", "2021-1-1"}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			node := yamlNode(t, src)
			require.Equal(t, "!!timestamp", node.Tag, "precondition: this spelling must resolve to !!timestamp")
			got, err := FromNode(node)
			require.NoError(t, err)
			assert.Equal(t, ir.ValueString, got.Kind)
			assert.Equal(t, src, got.Str, "verbatim spelling survives, not a canonicalized or parsed form")
		})
	}
}

func TestValueFromNode_ScalarErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		node *yaml.Node
	}{
		{"bad bool", scalarNode("!!bool", "notabool")},
		{"bad int", scalarNode("!!int", "12abc")},
		{"bad float", scalarNode("!!float", "1.2.3")},
		{"bad binary", scalarNode("!!binary", "@@@not-base64")},
		{"unsupported tag", scalarNode("!custom", "2020-01-01")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := FromNode(tc.node)
			require.Error(t, err)
		})
	}
}

func TestValueFromNode_OverflowNumberIsNumber(t *testing.T) {
	t.Parallel()
	// A float64-overflow literal resolves to a plain !!str node; it must be
	// captured as the number it is, canonicalized, not as a string.
	got, err := FromNode(scalarNode("!!str", "1.8e308"))
	require.NoError(t, err)
	assert.Equal(t, ir.ValueNumber, got.Kind)
	assert.Equal(t, ir.BigVal("1.8e308"), got.Num)
}

func TestValueFromNode_QuotedNumericStaysString(t *testing.T) {
	t.Parallel()
	// A quoted numeric string is not plain, so it stays a string.
	node := scalarNode("!!str", "123")
	node.Style = yaml.DoubleQuotedStyle
	got, err := FromNode(node)
	require.NoError(t, err)
	assert.Equal(t, ir.ValueString, got.Kind)
	assert.Equal(t, "123", got.Str)
}

func TestValueFromNode_UnsupportedNodeKind(t *testing.T) {
	t.Parallel()
	_, err := FromNode(&yaml.Node{Kind: yaml.Kind(99)})
	require.Error(t, err)
}

func TestValueFromNode_SequenceChildError(t *testing.T) {
	t.Parallel()
	seq := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
		scalarNode("!custom", "x"),
	}}
	_, err := FromNode(seq)
	require.Error(t, err)
}

func TestValueFromNode_MappingValueError(t *testing.T) {
	t.Parallel()
	m := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		scalarNode("!!str", "k"), scalarNode("!custom", "x"),
	}}
	_, err := FromNode(m)
	require.Error(t, err)
}

func TestValueFromNode_DepthCapExceeded(t *testing.T) {
	t.Parallel()
	n := scalarNode("!!int", "1")
	for range maxValueDepth + 2 {
		n = &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{n}}
	}
	_, err := FromNode(n)
	require.Error(t, err)
}

// yamlNode parses src and returns its single document's root node. It is a copy
// of the compiler package's helper rather than a shared one: a test helper that
// crossed a package boundary would be the first thing to make this package
// depend on its parent, which is the direction the extraction exists to remove.
func yamlNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	require.Len(t, doc.Content, 1, "expected a single document node")
	return doc.Content[0]
}

// scalarNode builds a bare scalar yaml.Node with the given tag and value.
func scalarNode(tag, val string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: val}
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
		// 644 here and 420 under !!int, which is the whole reason the two paths
		// are separate.
		{name: "a leading zero is dropped, not read as octal", tag: "!!str", val: "0644", want: "644"},
		{name: "a signed value", tag: "!!str", val: "-7", want: "-7"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NumericLiteral(scalarNode(tc.tag, tc.val))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
