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
// differential oracle for TestRawFromNode_DiffersFromTheOldDecodeOnlyWhereRecorded,
// which is the only claim about it worth making — that everything came through
// it unchanged except number spelling and the rows rawDivergences names.
func decodeAndMarshal(node *yaml.Node) (json.RawMessage, error) {
	var v any
	if err := node.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// throughFloat64 re-encodes raw JSON through Go's JSON model, which rounds every
// number to float64 — the only transformation the old conversion applied that
// the new walk does not, on a row rawDivergences does not name. A divergence is
// compared unnormalized instead, since that spelling is what it pins.
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
		{"out-of-float64-range plain scalar stays a string", "1e400", `"1e400"`},
		// A tag YAML has a type for and JSON does not keeps the source text
		// (GitHub #242). The resolved form is derivable from the spelling; the
		// spelling is not derivable from the resolved form.
		{"date keeps its source spelling", "2021-1-1", `"2021-1-1"`},
		{"date keeps its padding", "2021-01-01", `"2021-01-01"`},
		{"datetime keeps its space separator", "2021-01-01 10:20:30", `"2021-01-01 10:20:30"`},
		{"datetime keeps its zone as written", "2021-01-01T10:20:30+05:00", `"2021-01-01T10:20:30+05:00"`},
		{"datetime keeps its fractional seconds", "2021-01-01T10:20:30.5Z", `"2021-01-01T10:20:30.5Z"`},
		{"explicitly tagged date keeps its spelling", "!!timestamp 2021-1-1", `"2021-1-1"`},
		{"binary keeps its base64 text", `!!binary aGVsbG8=`, `"aGVsbG8="`},
		{"binary keeps a byte no UTF-8 can name", `!!binary /w==`, `"/w=="`},
		// base64.StdEncoding skips newlines, so YAML's own block spelling of a
		// !!binary decodes; what it kept was the decoded form, and what it
		// keeps now is the wrapped text the source wrote.
		{"block-form binary keeps its line breaks", "!!binary |\n  aGVs\n  bG8=\n", `"aGVs\nbG8=\n"`},
		// yaml.v3 resolves no type for these, so the scalar keeps the text it
		// was written with rather than being dropped for want of a tag.
		{"unresolvable double-bang tag", "!!python/object x", `"x"`},
		{"local tag", "!foo bar", `"bar"`},
		{"fully written-out tag still resolves", "!<tag:yaml.org,2002:int> 5", "5"},
		{"an explicit numeric tag outranks quoting", `!!int "5"`, "5"},
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
		{"alias as a mapping key", "a: &k mykey\nb: {*k: v}", `{"a":"mykey","b":{"mykey":"v"}}`},
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
		{"alias key naming an integer", "a: &k 1\nb: {*k: v}", "not a string"},
		{"alias key naming a sequence", "a: &k [1]\nb: {*k: v}", "not a string"},
		{"locally tagged key", "{!foo k: v}", "not a string"},
		{"duplicate key", "{k: 1, k: 2}", "already defined"},
		{"duplicate key after a merge", "{<<: {p: 1}, k: 1, k: 2}", "already defined"},
		{"not a number", ".nan", "numeric literal"},
		{"positive infinity", ".inf", "numeric literal"},
		{"negative infinity", "-.inf", "numeric literal"},
		{"merge from a scalar", "{<<: 1}", "map merge requires"},
		{"merge from a sequence of scalars", "{<<: [1]}", "map merge requires"},
		{"merge from an alias to a scalar", "a: &n 1\nb: {<<: *n}", "map merge requires"},
		{"non-string key inside a merged mapping", "{<<: [{1: v}]}", "not a string"},
		{"boolean tag on a non-boolean", "!!bool notabool", "bool literal"},
		// The tag check outlives the decode that used to supply the output: a
		// scalar that does not satisfy the type it declares is still refused,
		// so preserving the source spelling widened nothing (GitHub #242).
		{"timestamp tag on a non-date", "!!timestamp notadate", "timestamp literal"},
		{"binary tag on non-base64", `!!binary "###"`, "binary literal"},
		// NewBigVal itself now refuses a binary exponent (GitHub #45), so this
		// is refused one step earlier than it used to be: at NumericLiteral,
		// not at the json.Valid splice check (see spliceNumber and
		// TestSpliceNumber_RefusesANonJSONNumber for that check's own case).
		{"float tag on a binary-exponent literal", "!!float 1p4", "not a decimal numeric literal"},
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
		{
			"unresolved alias as a key",
			mappingOf(&yaml.Node{Kind: yaml.AliasNode, Value: "x"},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "v"}),
			"resolves to nothing",
		},
		{
			"alias chain longer than a source could mean",
			mappingOf(aliasChain(maxAliasHops+1),
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "v"}),
			"chains past",
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

// rawDivergences is the closed list of inputs on which the walk deliberately
// disagrees with the decode it replaced, beyond how a number is spelled. Each
// row holds at least one scalar YAML gives a type and JSON does not, kept as
// the text the source wrote rather than as the resolved form (GitHub #242).
//
// Both spellings are pinned. The new one is the preservation the change exists
// for; the old one is what keeps the row honest — without it a divergence that
// quietly stopped being one would leave this entry asserting nothing, which is
// the same hole as excluding the row outright.
var rawDivergences = map[string]struct{ old, want string }{
	"2021-1-1":            {`"2021-01-01T00:00:00Z"`, `"2021-1-1"`},
	"2021-01-01 10:20:30": {`"2021-01-01T10:20:30Z"`, `"2021-01-01 10:20:30"`},
	`!!binary aGVsbG8=`:   {`"hello"`, `"aGVsbG8="`},
	// The old spelling is the escape encoding/json writes for a byte no UTF-8
	// can name, which is the loss itself: 0xFF and a source that really wrote
	// U+FFFD both reached the IR as this, with nothing to tell them apart.
	`!!binary /w==`: {"\"\\ufffd\"", `"/w=="`},
	// Nesting is the same rule one level down: one divergent scalar makes the
	// whole construct diverge, which is how every raw site holding a structure
	// rather than a bare scalar reaches this.
	"{when: 2021-1-1, blob: !!binary /w==}": {
		"{\"blob\":\"\\ufffd\",\"when\":\"2021-01-01T00:00:00Z\"}",
		`{"blob":"/w==","when":"2021-1-1"}`,
	},
	// A block !!binary decodes to the same bytes as the flow one above, so the
	// old spelling is identical while the source text is not.
	"!!binary |\n  aGVs\n  bG8=\n": {`"hello"`, `"aGVs\nbG8=\n"`},
}

// rawEquivalenceCorpus is the input set both differential tests below read.
var rawEquivalenceCorpus = []string{
	"1", "1.5", "-2", "0", "0.0", "1e10", "1.10", "0o17", "0x1f", "1_000",
	"12345678901234567890123", "1.000000000000000000001", "-9223372036854775809",
	"null", "true", "false", "hello", `"123"`, `""`, "1e400", `"a<b>&c"`,
	// The scalars YAML types and JSON does not, in both spellings that matter:
	// ones the old decode rewrote, which rawDivergences records, and ones it
	// already reproduced exactly. The second kind is what keeps that map tight —
	// it must list what actually diverges, not every input carrying such a tag.
	"2021-1-1", "2021-01-01 10:20:30", "2021-01-01T10:20:30.5Z",
	"2021-01-01T10:20:30+05:00", `!!binary aGVsbG8=`, `!!binary /w==`,
	"!!binary |\n  aGVs\n  bG8=\n", "{when: 2021-1-1, blob: !!binary /w==}",
	"{}", "[]", "[1, 2, 3]", "{z: 1, a: 2, m: 3}", "{a: {b: {c: 1}}}",
	"[[1], [2, [3]]]", "{a: [1, {b: 2}], c: null}",
	"{<<: {p: 1}, q: 2}", "{<<: {p: 1}, p: 2}", "{<<: [{p: 1}, {p: 2}]}",
	"a: &m {p: 1}\nb: {<<: *m, q: 2}", "a: &n 5\nb: *n", "a: &s [1, 2]\nb: *s",
	"{k: [{n: 12345678901234567890123}, 1.10]}",
	"{unicode: \"héllo→\"}", "{empty_map: {}, empty_seq: []}",
	// Shapes where the walk had to reproduce a yaml.v3 rule rather than a
	// JSON one. Each of these was a live regression until the rule was found.
	"a: &k mykey\nb: {*k: v}", "!!python/object x", "!foo bar", "!!set x",
	"!<tag:yaml.org,2002:int> 5", `!!int "5"`, `!!str 123`,
	"{<<: {}}", "{<<: [{}]}", "a: &m {p: 1}\nb: {<<: [*m, {q: 2}]}",
	"{a: yes, b: no, c: on, d: off}", "{a: 1:30, b: 12:34:56}",
	"|\n  line1\n  line2", ">\n  folded text", "a: &s {x: 1}\nb: *s\nc: *s",
	`{"": 1}`, `{"a\nb": 1}`, "{a: }", "{a: null, b: ~}",
	// Refused by the old conversion, preserved by the walk. They carry no
	// equality claim, and are here so the asymmetry is visible in a -v run
	// rather than asserted in a comment.
	"!!int 12345678901234567890123", "!!float 1e400",
}

// TestRawFromNode_DiffersFromTheOldDecodeOnlyWhereRecorded is the equivalence
// oracle for the rewrite. Reading the two implementations cannot show they
// agree; rounding the new output through float64 and demanding the old output
// back can, because rounding is the only transformation the old one applied to
// a row rawDivergences does not name.
//
// A row the old conversion refused carries no claim — the new walk is allowed
// to be strictly more capable, and on `!!int 12345678901234567890123` and on a
// block-form `!!binary` it is.
func TestRawFromNode_DiffersFromTheOldDecodeOnlyWhereRecorded(t *testing.T) {
	t.Parallel()
	for _, src := range rawEquivalenceCorpus {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			node := yamlNode(t, src)

			want, oldErr := decodeAndMarshal(node)
			got, newErr := RawFromNode(node)

			if recorded, diverges := rawDivergences[src]; diverges {
				require.NoError(t, oldErr, "a recorded divergence must be a row the old conversion accepted")
				require.NoError(t, newErr)
				assert.Equal(t, recorded.old, string(want),
					"the old spelling, pinned so the row cannot stop being a divergence unnoticed")
				assert.Equal(t, recorded.want, string(got), "the source text the walk preserves instead")
				return
			}
			if oldErr != nil {
				t.Skipf("the old conversion refused this input (%v); the new walk owes it nothing", oldErr)
			}
			require.NoError(t, newErr, "the old conversion accepted this input")
			assert.Equal(t, throughFloat64(t, want), throughFloat64(t, got),
				"the walk must differ from the decode it replaced only in how a number is spelled, "+
					"or in a way rawDivergences records")
		})
	}
}

// TestRawDivergences_NamesOnlyCorpusRows keeps the two halves tied together. A
// divergence naming an input the corpus does not hold is never evaluated, so it
// would sit there reading as coverage while asserting nothing at all.
func TestRawDivergences_NamesOnlyCorpusRows(t *testing.T) {
	t.Parallel()
	for src := range rawDivergences {
		assert.Contains(t, rawEquivalenceCorpus, src,
			"a recorded divergence the corpus never feeds through is dead weight")
	}
}

// aliasChain builds n alias nodes each naming the next, ending on a string. A
// parser reaches this shape only by anchoring an alias, and never this deep.
func aliasChain(n int) *yaml.Node {
	node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "end"}
	for i := 0; i < n; i++ {
		node = &yaml.Node{Kind: yaml.AliasNode, Value: "a", Alias: node}
	}
	return node
}

// mappingOf builds a one-pair mapping node, for the malformed shapes no parser
// emits.
func mappingOf(key, val *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{key, val}}
}

// TestRawConv_RefusesNodesNoCallerShouldPass covers the preconditions the walk
// asserts on itself rather than on its input. None is reachable through
// RawFromNode — it rejects a nil node before walking, every mapping handed to
// mappingInto has already been proven to be one, and scalar routes only two
// tags into verbatimTagged — so they are exercised here directly. They stay
// because dropping one turns a caller's mistake into a silently wrong answer
// rather than an answer at all.
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

	// scalar routes only !!timestamp and !!binary into verbatimTagged, so no
	// input reaches this arm. It answers rather than falling through to the
	// base64 check, which is what a third tag added to that case would hit.
	got, err = c.verbatimTagged(yamlNode(t, "plain"))
	require.Error(t, err, "a tag scalar does not route here is a caller bug")
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), `scalar tag "!!str" is not kept verbatim`)
}

// TestSpliceNumber_RefusesANonJSONNumber covers the refusal scalar's !!int/
// !!float arm cannot reach today, the same way TestRawConv_RefusesNodesNoCallerShouldPass
// covers verbatimTagged's default case: no value.NumericLiteral result can
// fail spliceNumber's json.Valid check, since NewBigVal enforces BigVal's
// "always JSON-valid" contract itself now (GitHub #45 was a binary exponent
// slipping through that contract and reaching here unrefused). The check
// stays as insurance against a future regression in that contract, so it is
// exercised directly with a source string NumericLiteral itself would now
// refuse before ever handing it to spliceNumber.
func TestSpliceNumber_RefusesANonJSONNumber(t *testing.T) {
	t.Parallel()

	got, err := spliceNumber("1p4", "1p4")

	require.Error(t, err, "1p4 is not a JSON number")
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "which is not JSON")
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

// TestRawFromNode_ResolvesAnUntaggedScalarByItsValue pins the one input for
// which reading the tag off the node and resolving it differ. Every parser-built
// scalar arrives tagged — the parser resolves them, and writes `!!int` even for a
// spelled-out `!<tag:yaml.org,2002:int>` — so only a hand-assembled node reaches
// this, and taking n.Tag would render 5 as the string "5".
func TestRawFromNode_ResolvesAnUntaggedScalarByItsValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, value, want string }{
		{"integer", "5", "5"},
		{"decimal", "1.10", "1.10"},
		{"boolean", "true", "true"},
		{"word", "hello", `"hello"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RawFromNode(&yaml.Node{Kind: yaml.ScalarNode, Value: tc.value})
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}
