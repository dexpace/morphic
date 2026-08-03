package annotation

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
)

// decodeAndMarshal is the conversion RawFromNode used before GitHub #32: decode
// the node into Go's JSON model, then re-marshal it. It is kept here as the
// differential oracle for TestRawFromNode_DiffersFromTheOldDecodeOnlyInNumbers,
// which is the only claim about it worth making — that everything except number
// spelling came through it unchanged.
func decodeAndMarshal(node *yaml.Node) (json.RawMessage, error) {
	var v any
	if err := node.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// throughFloat64 re-encodes raw JSON through Go's JSON model, which rounds every
// number to float64 — the one transformation the old conversion applied that the
// new walk does not.
//
// Both sides of the comparison go through it, not just the new output: the trip
// also canonicalizes how an escape is spelled (a "\ufffd" escape comes back as
// the literal rune), and normalizing one side alone would report that as a
// difference. Rounding an already-rounded number changes nothing, so applying it
// to the old output costs the comparison none of its force.
func throughFloat64(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(raw, &v))
	out, err := json.Marshal(v)
	require.NoError(t, err)
	return string(out)
}

// TestRawFromNode_KeepsNumericLiteralsExact is the regression this change
// exists for. Every row is a value the old decode/re-marshal rewrote, in the
// one channel whose documented promise is verbatim preservation (GitHub #32).
func TestRawFromNode_KeepsNumericLiteralsExact(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"integer past int64", "12345678901234567890123", "12345678901234567890123"},
		{"decimal past float64 precision", "1.000000000000000000001", "1.000000000000000000001"},
		{"negative past int64", "-9223372036854775809", "-9223372036854775809"},
		{"trailing zero is significant", "1.10", "1.10"},
		{"exponent case is preserved", "1E+10", "1E+10"},
		{"int64 boundary", "9223372036854775807", "9223372036854775807"},
		{"uint64 boundary", "18446744073709551615", "18446744073709551615"},
		{"tiny magnitude", "1.5e-300", "1.5e-300"},
		{"explicitly tagged huge int", `!!int 12345678901234567890123`, "12345678901234567890123"},
		{"nested in a mapping", "{a: 12345678901234567890123}", `{"a":12345678901234567890123}`},
		{"nested in a sequence", "[1.000000000000000000001]", "[1.000000000000000000001]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RawFromNode(yamlNode(t, tc.yaml))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

// TestRawFromNode_ResolvesYAMLIntegerBases pins the half of numeric handling
// that must not become verbatim: YAML takes an integer's base from its prefix,
// so the source spelling is not what the value means in base 10. Preserving
// these as written would read 0o17 as seventeen.
func TestRawFromNode_ResolvesYAMLIntegerBases(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, yaml, want string }{
		{"octal", "0o17", "15"},
		{"hex", "0x1f", "31"},
		{"binary", "0b101", "5"},
		{"bare leading zero is octal", "0644", "420"},
		{"digit separators are dropped", "1_000", "1000"},
		{"leading plus is dropped", "+5", "5"},
		{"trailing dot is JSON-invalid", "5.", "5"},
		{"leading dot is JSON-invalid", ".5", "0.5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RawFromNode(yamlNode(t, tc.yaml))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
			assert.True(t, json.Valid(got), "every rendered number is JSON-valid")
		})
	}
}

// TestRawFromNode_RendersEveryScalarTag covers the tags a walk over the node
// tree must render itself, now that no decode into `any` renders them for it.
func TestRawFromNode_RendersEveryScalarTag(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, yaml, want string }{
		{"null", "null", "null"},
		{"tilde is null", "~", "null"},
		{"true", "true", "true"},
		{"false", "false", "false"},
		{"string", "hello", `"hello"`},
		{"quoted number stays a string", `"123"`, `"123"`},
		{"empty string", `""`, `""`},
		{"HTML is escaped, as encoding/json does it", `"a<b>&c"`, `"a\u003cb\u003e\u0026c"`},
		{"date normalizes to RFC 3339", "2021-1-1", `"2021-01-01T00:00:00Z"`},
		{"datetime keeps its nanoseconds", "2021-01-01T10:20:30.5Z", `"2021-01-01T10:20:30.5Z"`},
		{"binary carries decoded bytes", `!!binary aGVsbG8=`, `"hello"`},
		{"out-of-float64-range plain scalar stays a string", "1e400", `"1e400"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RawFromNode(yamlNode(t, tc.yaml))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

// TestRawFromNode_PreservesMergeAndOrderingSemantics pins the mapping rules the
// walk had to reimplement when it stopped delegating to yaml.v3's decoder.
// Losing any of them would be a silent regression: the construct still converts,
// just to something else.
func TestRawFromNode_PreservesMergeAndOrderingSemantics(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, yaml, want string }{
		{"keys sort", "{z: 1, a: 2, m: 3}", `{"a":2,"m":3,"z":1}`},
		{"sequence keeps source order", "[3, 1, 2]", `[3,1,2]`},
		{"merge key expands", "{<<: {p: 1}, q: 2}", `{"p":1,"q":2}`},
		{"own key beats merged key", "{<<: {p: 1}, p: 2}", `{"p":2}`},
		{"earlier merge source wins", "{<<: [{p: 1}, {p: 2}]}", `{"p":1}`},
		{"merge through an alias", "a: &m {p: 1}\nb: {<<: *m, q: 2}", `{"a":{"p":1},"b":{"p":1,"q":2}}`},
		{"nested merge inside a merged map", "a: &m {<<: {deep: 1}, p: 2}\nb: {<<: *m}", `{"a":{"deep":1,"p":2},"b":{"deep":1,"p":2}}`},
		{"alias to a scalar", "a: &n 5\nb: *n", `{"a":5,"b":5}`},
		{"alias to a sequence", "a: &s [1, 2]\nb: *s", `{"a":[1,2],"b":[1,2]}`},
		{"empty mapping", "{}", `{}`},
		{"empty sequence", "[]", `[]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RawFromNode(yamlNode(t, tc.yaml))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

// TestRawFromNode_RefusesWhatJSONCannotName pins the failures. Each one is a
// construct that reaches the IR in no form at all rather than in a weaker one,
// which is why the caller turns it into a diagnostic (GitHub #144).
func TestRawFromNode_RefusesWhatJSONCannotName(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, yaml, wantErr string }{
		{"integer key", "{1: v}", "not a string"},
		{"null key", "{null: v}", "not a string"},
		{"bool key", "{true: v}", "not a string"},
		{"sequence key", "{? [1, 2]\n: v}", "not a string"},
		{"nested non-string key", "{outer: {1: v}}", "not a string"},
		{"duplicate key", "{k: 1, k: 2}", "already defined"},
		{"duplicate key after a merge", "{<<: {p: 1}, k: 1, k: 2}", "already defined"},
		{"not a number", ".nan", "numeric literal"},
		{"positive infinity", ".inf", "numeric literal"},
		{"negative infinity", "-.inf", "numeric literal"},
		{"unknown scalar tag", "!!python/object x", `unsupported scalar tag`},
		{"merge from a scalar", "{<<: 1}", "map merge requires"},
		{"merge from a sequence of scalars", "{<<: [1]}", "map merge requires"},
		{"merge from an alias to a scalar", "a: &n 1\nb: {<<: *n}", "map merge requires"},
		{"non-string key inside a merged mapping", "{<<: [{1: v}]}", "not a string"},
		{"boolean tag on a non-boolean", "!!bool notabool", "bool literal"},
		{"timestamp tag on a non-date", "!!timestamp notadate", "timestamp literal"},
		{"binary tag on non-base64", `!!binary "###"`, "binary literal"},
		{"float tag on a binary-exponent literal", "!!float 1p4", "which is not JSON"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RawFromNode(yamlNode(t, tc.yaml))
			require.Error(t, err)
			assert.Nil(t, got, "a refusal writes nothing")
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestRawFromNode_IsBoundedOnACyclicAlias proves the walk terminates on the one
// input shape a node tree can hold that a document tree cannot: an alias naming
// an ancestor. Without the depth bound this recurses until the stack dies.
func TestRawFromNode_IsBoundedOnACyclicAlias(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: &x [*x]\n"), &doc))

	got, err := RawFromNode(doc.Content[0])

	require.Error(t, err, "a cyclic alias is refused rather than followed forever")
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "nesting exceeds")
}

// TestRawFromNode_IsBoundedOnAliasAmplification proves the node budget stops a
// wide alias chain. scan refuses this shape long before it reaches here; the
// bound exists so the walk does not depend on that having run.
func TestRawFromNode_IsBoundedOnAliasAmplification(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x, x, x, x, x, x, x, x]\n")
	for level := 1; level <= 8; level++ {
		fmt.Fprintf(&b, "a%d: &a%d [", level, level)
		for i := 0; i < 9; i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "*a%d", level-1)
		}
		b.WriteString("]\n")
	}
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(b.String()), &doc))

	got, err := RawFromNode(doc.Content[0])

	require.Error(t, err, "a multiplicative alias chain is refused")
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "expands past")
}

// TestRawFromNode_RejectsMalformedNodes covers the node shapes no parser
// produces but a caller could hand-build, so the walk answers rather than
// panicking on them.
func TestRawFromNode_RejectsMalformedNodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		node    *yaml.Node
		wantErr string
	}{
		{"zero node", &yaml.Node{}, "unsupported yaml node kind"},
		{"unresolved alias", &yaml.Node{Kind: yaml.AliasNode, Value: "x"}, "resolves to nothing"},
		{"childless document", &yaml.Node{Kind: yaml.DocumentNode}, "document node holds 0 children"},
		{
			"merge from an unresolved alias",
			mappingOf(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!merge", Value: "<<"},
				&yaml.Node{Kind: yaml.AliasNode, Value: "x"}),
			"resolves to nothing",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RawFromNode(tc.node)
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestRawFromNode_WalksThroughADocumentNode covers the entry a caller reaches
// when it hands over a whole parsed document rather than a node inside one.
func TestRawFromNode_WalksThroughADocumentNode(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("{a: 1}\n"), &doc))
	require.Equal(t, yaml.DocumentNode, doc.Kind)

	got, err := RawFromNode(&doc)

	require.NoError(t, err)
	assert.JSONEq(t, `{"a":1}`, string(got))
}

// TestRawFromNode_DiffersFromTheOldDecodeOnlyInNumbers is the equivalence
// oracle for the rewrite. Reading the two implementations cannot show they
// agree; rounding the new output through float64 and demanding the old output
// back can, because that is the only transformation the old one applied.
//
// A row the old conversion refused carries no claim — the new walk is allowed
// to be strictly more capable, and on `!!int 12345678901234567890123` it is.
func TestRawFromNode_DiffersFromTheOldDecodeOnlyInNumbers(t *testing.T) {
	t.Parallel()
	corpus := []string{
		"1", "1.5", "-2", "0", "0.0", "1e10", "1.10", "0o17", "0x1f", "1_000",
		"12345678901234567890123", "1.000000000000000000001", "-9223372036854775809",
		"null", "true", "false", "hello", `"123"`, `""`, "1e400", `"a<b>&c"`,
		"2021-1-1", "2021-01-01T10:20:30.5Z", `!!binary aGVsbG8=`, `!!binary /w==`,
		"{}", "[]", "[1, 2, 3]", "{z: 1, a: 2, m: 3}", "{a: {b: {c: 1}}}",
		"[[1], [2, [3]]]", "{a: [1, {b: 2}], c: null}",
		"{<<: {p: 1}, q: 2}", "{<<: {p: 1}, p: 2}", "{<<: [{p: 1}, {p: 2}]}",
		"a: &m {p: 1}\nb: {<<: *m, q: 2}", "a: &n 5\nb: *n", "a: &s [1, 2]\nb: *s",
		"{k: [{n: 12345678901234567890123}, 1.10]}",
		"{unicode: \"héllo→\"}", "{empty_map: {}, empty_seq: []}",
	}
	for _, src := range corpus {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			node := yamlNode(t, src)

			want, oldErr := decodeAndMarshal(node)
			got, newErr := RawFromNode(node)

			if oldErr != nil {
				t.Skipf("the old conversion refused this input (%v); the new walk owes it nothing", oldErr)
			}
			require.NoError(t, newErr, "the old conversion accepted this input")
			assert.Equal(t, throughFloat64(t, want), throughFloat64(t, got),
				"the walk must differ from the decode it replaced only in how a number is spelled")
		})
	}
}

// mappingOf builds a one-pair mapping node, for the malformed shapes no parser
// emits.
func mappingOf(key, val *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{key, val}}
}

// TestRawConv_RefusesNodesNoCallerShouldPass covers the two preconditions the
// walk asserts on itself rather than on its input. Neither is reachable through
// RawFromNode — it rejects a nil node before walking, and every mapping handed
// to mappingInto has already been proven to be one — so they are exercised here
// directly. They stay because dropping them turns a caller's mistake into a
// silently empty object rather than an answer.
func TestRawConv_RefusesNodesNoCallerShouldPass(t *testing.T) {
	t.Parallel()

	var c rawConv
	got, err := c.node(nil, 0)
	require.Error(t, err, "a nil node is a caller bug, not an absent construct")
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "nil yaml node")

	err = c.mappingInto(map[string]json.RawMessage{}, yamlNode(t, "[1]"), 0)
	require.Error(t, err, "filling a mapping from a sequence is a caller bug")
	assert.Contains(t, err.Error(), "expected a mapping")
}

// TestRawFromNode_BoundsAMergeChainThatNeverRevisitsANode proves the bound on
// the one recursion that does not pass through node(): a `<<` whose source is
// itself nothing but a `<<`. Each level adds depth while adding no value to
// walk, so only merge's own check stops it.
//
// The chain is built rather than parsed because yaml.v3 caps parse nesting at
// the same figure, so no document could express one this deep inline; aliases
// are how a real source would reach it.
func TestRawFromNode_BoundsAMergeChainThatNeverRevisitsANode(t *testing.T) {
	t.Parallel()
	innermost := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	chain := innermost
	for i := 0; i < maxRawDepth+10; i++ {
		chain = mappingOf(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!merge", Value: "<<"}, chain)
	}

	got, err := RawFromNode(chain)

	require.Error(t, err, "a merge chain past the depth bound is refused")
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "nesting exceeds")
}
