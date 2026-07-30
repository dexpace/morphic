package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// TestReverseMappings_Permutes covers the rewrite's shapes: what it reverses,
// what it leaves alone, and what it refuses.
func TestReverseMappings_Permutes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		want string // "" means the rewrite must refuse
	}{
		{
			name: "a mapping's entries reverse",
			src:  "a: 1\nb: 2\nc: 3\n",
			want: "c: 3\nb: 2\na: 1\n",
		},
		{
			name: "each key keeps its own value",
			src:  "a: 1\nb: {x: 1, y: 2}\n",
			want: "b: {y: 2, x: 1}\na: 1\n",
		},
		{
			name: "a sequence keeps its order",
			src:  "allOf:\n  - first\n  - second\n",
			want: "allOf:\n    - first\n    - second\n",
		},
		{
			name: "nested mappings reverse too",
			src:  "outer:\n  a: 1\n  b: 2\n",
			want: "outer:\n    b: 2\n    a: 1\n",
		},
		{
			// Reversing decides which declaration wins, so the permutation is not
			// meaning-preserving and the source is excluded rather than reported (#95).
			name: "a duplicate key is refused",
			src:  "a: 1\na: 2\n",
		},
		{
			name: "a duplicate key nested inside is refused",
			src:  "outer:\n  dup: 1\n  dup: 2\n",
		},
		{
			name: "an unparseable source is refused",
			src:  "a: [unclosed\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := reverseMappings([]byte(tc.src))
			if tc.want == "" {
				assert.False(t, ok, "this source must not be permuted")
				return
			}
			require.True(t, ok, "this source is permutable")
			assert.Equal(t, tc.want, string(got))
		})
	}
}

// TestReverseMappings_AliasIsNotFollowed pins that an anchored mapping is
// reversed once, at its anchor, rather than once per alias pointing at it.
func TestReverseMappings_AliasIsNotFollowed(t *testing.T) {
	t.Parallel()
	got, ok := reverseMappings([]byte("anchor: &a\n  x: 1\n  y: 2\nuse: *a\n"))
	require.True(t, ok)
	assert.Equal(t, "use: *a\nanchor: &a\n    y: 2\n    x: 1\n", string(got),
		"the anchored mapping reverses once and the alias still points at it")
}

// TestReverseNode_DepthBound covers the walk's bound. No document the compiler
// accepts nests this deep, so the bound exists to keep the walk terminating
// rather than to reject real input.
func TestReverseNode_DepthBound(t *testing.T) {
	t.Parallel()
	deep := yamlMapping()
	assert.False(t, reverseNode(&deep, maxReverseDepth+1),
		"past the bound the rewrite refuses rather than descending further")
	assert.True(t, reverseNode(nil, 0), "a nil node is nothing to reverse, not a refusal")
}

// TestReversedPairs_OddTrailingElementIsKept pins that a malformed mapping is
// passed through rather than losing its trailing node. A parsed mapping always
// holds key/value pairs, so this guards the helper rather than a reachable input.
func TestReversedPairs_OddTrailingElementIsKept(t *testing.T) {
	t.Parallel()
	a, b, c := scalarNode("a"), scalarNode("b"), scalarNode("c")
	got := reversedPairs([]*yaml.Node{a, b, c})
	require.Len(t, got, 3)
	assert.Equal(t, []*yaml.Node{a, b, c}, got, "one pair, reversed onto itself, then the odd tail")
}

// TestOrderInvariant_UnpermutableSourcePasses pins the two ways the oracle
// declines to ask its question, so neither reads as a finding.
func TestOrderInvariant_UnpermutableSourcePasses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"a duplicate key excludes the source", "a: 1\na: 2\n"},
		{"a source with nothing to permute", "a: 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			detail, ok := orderInvariant(context.Background(), "spec", []byte(tc.src), &ir.Document{})
			assert.True(t, ok, "declining to ask is not a finding")
			assert.Empty(t, detail)
		})
	}
}

// TestOrderInvariant_CompileFailureIsReported covers the recompile's error
// paths, which a correct compiler never produces on a source that already
// compiled once.
func TestOrderInvariant_CompileFailureIsReported(t *testing.T) {
	const src = "a: 1\nb: 2\n"
	orig := compile
	t.Cleanup(func() { compile = orig })

	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, errors.New("boom")
	}
	detail, ok := orderInvariant(context.Background(), "spec", []byte(src), &ir.Document{})
	assert.False(t, ok)
	assert.Contains(t, detail, "boom")

	compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, nil
	}
	detail, ok = orderInvariant(context.Background(), "spec", []byte(src), &ir.Document{})
	assert.False(t, ok)
	assert.Contains(t, detail, "no document")
}

// TestDiffOrderInvariants_ReportsEachChannel drives the three ways a permutation
// can differ, so each message is exercised rather than only the first.
func TestDiffOrderInvariants_ReportsEachChannel(t *testing.T) {
	t.Parallel()
	model := func(id ir.TypeID, wire string) *ir.Model {
		return &ir.Model{
			TypeCommon: ir.TypeCommon{ID: id, Name: ir.Naming{Source: "M", Canonical: "m"}},
			Properties: []ir.Property{{ID: ir.PropID("p/x/" + wire), WireName: wire}},
		}
	}
	one := &ir.Document{Types: ir.TypeRegistry{"t/x/A": model("t/x/A", "a")}}

	t.Run("a different number of types", func(t *testing.T) {
		t.Parallel()
		detail, ok := diffOrderInvariants(one, &ir.Document{Types: ir.TypeRegistry{}}, nil)
		assert.False(t, ok)
		assert.Contains(t, detail, "interns 0 types against 1")
	})

	t.Run("a type that changed shape", func(t *testing.T) {
		t.Parallel()
		other := &ir.Document{Types: ir.TypeRegistry{"t/x/A": model("t/x/A", "b")}}
		detail, ok := diffOrderInvariants(one, other, nil)
		assert.False(t, ok)
		assert.Contains(t, detail, "type registry depends on declaration order")
	})

	t.Run("a different set of diagnostics", func(t *testing.T) {
		t.Parallel()
		other := &ir.Document{Types: ir.TypeRegistry{"t/x/A": model("t/x/A", "a")}}
		detail, ok := diffOrderInvariants(one, other,
			[]ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "c", Provenance: ir.Provenance{Pointer: "/p"}}})
		assert.False(t, ok)
		assert.Contains(t, detail, "diagnostics depend on declaration order")
	})
}

// TestDiffOrderInvariants_ReorderedCollectionsAreNotAFinding pins the sorting:
// properties, pattern properties and examples are held in source order, so a
// permutation reorders them by design and must not be reported.
func TestDiffOrderInvariants_ReorderedCollectionsAreNotAFinding(t *testing.T) {
	t.Parallel()
	props := []ir.Property{{ID: "p/x/a", WireName: "a"}, {ID: "p/x/b", WireName: "b"}}
	patterns := []ir.PatternProps{{Pattern: "^a"}, {Pattern: "^b"}}
	examples := []ir.Example{{Name: "one"}, {Name: "two"}}
	build := func(reversed bool) *ir.Document {
		p, pp, ex := props, patterns, examples
		if reversed {
			p = []ir.Property{props[1], props[0]}
			pp = []ir.PatternProps{patterns[1], patterns[0]}
			ex = []ir.Example{examples[1], examples[0]}
		}
		return &ir.Document{Types: ir.TypeRegistry{"t/x/A": &ir.Model{
			TypeCommon:      ir.TypeCommon{ID: "t/x/A", Examples: ex},
			Properties:      p,
			AdditionalProps: &ir.AdditionalProps{Patterns: pp},
		}}}
	}
	detail, ok := diffOrderInvariants(build(false), build(true), nil)
	assert.True(t, ok, "reordering a source-ordered collection is the permutation, not a defect: %s", detail)
}

// TestOrderInvariant_ReachesTheCorpus is the reachability guard. The oracle
// declines silently on a source it cannot permute, so a rewrite that started
// refusing everything would leave the corpus sweep green while checking nothing
// — the same shape as a verifier wired into CI that never meets its input.
//
// It asserts the oracle actually asked its question of most of the corpus, not
// merely that it ran.
func TestOrderInvariant_ReachesTheCorpus(t *testing.T) {
	t.Parallel()
	const dir = "../../testdata/conformance/openapi"
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var specs, permuted int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		specs++
		data, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, readErr)
		rewritten, ok := reverseMappings(data)
		if ok && string(rewritten) != string(data) {
			permuted++
		}
	}
	require.Positive(t, specs, "found no conformance specs to permute")
	assert.Equal(t, specs, permuted,
		"every conformance spec must be permutable, or the oracle is silently skipping it")
}

// yamlMapping returns a mapping node for the depth-bound test.
func yamlMapping() yaml.Node {
	return yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{scalarNode("k"), scalarNode("v")}}
}

// scalarNode returns a scalar node carrying value.
func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: value}
}

// TestReverseMappings_EncodeFailureIsRefused drives the defensive re-encode
// path. A tree that has just parsed cannot fail to re-encode, so the seam is how
// the branch is reached at all — the same arrangement reserializeJSON uses.
func TestReverseMappings_EncodeFailureIsRefused(t *testing.T) {
	orig := encodeYAML
	t.Cleanup(func() { encodeYAML = orig })
	encodeYAML = func(any) ([]byte, error) { return nil, errors.New("boom") }

	got, ok := reverseMappings([]byte("a: 1\nb: 2\n"))
	assert.False(t, ok, "a source that will not re-encode is excluded, not reported")
	assert.Nil(t, got)
}

// TestCheck_OrderDependentOutcome pins that Check classifies an order-dependent
// compiler as such rather than folding it into another oracle. The seam returns
// a different registry for the permuted bytes, which is what a lowering that
// depends on declaration order does.
func TestCheck_OrderDependentOutcome(t *testing.T) {
	orig := compile
	t.Cleanup(func() { compile = orig })

	const src = "a: 1\nb: 2\n"
	compile = func(_ context.Context, _ string, data []byte) (*ir.Document, []ir.Diagnostic, error) {
		id := ir.TypeID("t/x/AsWritten")
		if string(data) != src {
			id = "t/x/Reversed"
		}
		path, _ := ir.IDPath(ir.IDKindType, string(id))
		m := &ir.Model{TypeCommon: ir.TypeCommon{
			ID:         id,
			Name:       ir.Naming{Source: "M", Canonical: "m"},
			Provenance: ir.Provenance{Pointer: "/" + path},
		}}
		return &ir.Document{Types: ir.TypeRegistry{m.ID: m}}, nil, nil
	}

	r := Check(context.Background(), "spec", []byte(src))
	assert.Equal(t, OutcomeOrderDependent, r.Outcome, "detail: %s", r.Detail)
	assert.Contains(t, r.Detail, "declaration order")
}
