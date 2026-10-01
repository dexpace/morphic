package harness

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
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
//
// The anchor and its use sit in a sequence, whose order the rewrite leaves
// alone, so the anchor still precedes the alias afterwards. Declared as two keys
// of one mapping they would swap, and a source whose permutation puts an alias
// above its anchor is refused outright — the case below this one.
func TestReverseMappings_AliasIsNotFollowed(t *testing.T) {
	t.Parallel()
	got, ok := reverseMappings([]byte("items:\n  - anchor: &a\n      x: 1\n      y: 2\n  - use: *a\n"))
	require.True(t, ok)
	assert.Equal(t, "items:\n    - anchor: &a\n        y: 2\n        x: 1\n    - use: *a\n", string(got),
		"the anchored mapping reverses once and the alias still points at it")
}

// TestReverseMappings_AliasAboveItsAnchorIsRefused pins the exclusion that
// replaced the shape the test above used to assert. Reversing the two keys puts
// the alias first, and YAML requires an anchor to be defined before it is
// referenced, so the permuted source does not parse at all. Handing that to the
// compiler reported the parse failure as order dependence.
func TestReverseMappings_AliasAboveItsAnchorIsRefused(t *testing.T) {
	t.Parallel()
	got, ok := reverseMappings([]byte("anchor: &a\n  x: 1\n  y: 2\nuse: *a\n"))
	assert.False(t, ok, "a permutation that no longer parses is not a faithful one")
	assert.Nil(t, got)
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
		{"an unparseable source excludes the baseline too", "a: [unclosed\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			detail, ok := orderInvariant(context.Background(), "spec", []byte(tc.src))
			assert.True(t, ok, "declining to ask is not a finding")
			assert.Empty(t, detail)
		})
	}
}

// TestOrderInvariant_CompileFailureIsReported covers the permuted recompile's
// error paths, which a correct compiler never produces on a source whose
// baseline already compiled.
//
// The seam answers for the baseline and fails only for the permutation. Failing
// both would exercise nothing: a baseline that will not compile means the
// re-encoding is not the source, which the oracle skips rather than reports.
func TestOrderInvariant_CompileFailureIsReported(t *testing.T) {
	const src = "a: 1\nb: 2\n"
	orig := compile
	t.Cleanup(func() { compile = orig })

	baseline, ok := reencodeMappings([]byte(src))
	require.True(t, ok)

	onlyPermutedFails := func(fail func() (*ir.Document, []ir.Diagnostic, error)) {
		compile = func(_ context.Context, _ string, data []byte) (*ir.Document, []ir.Diagnostic, error) {
			if bytes.Equal(data, baseline) {
				return &ir.Document{}, nil, nil
			}
			return fail()
		}
	}

	onlyPermutedFails(func() (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, errors.New("boom")
	})
	detail, ok := orderInvariant(context.Background(), "spec", []byte(src))
	assert.False(t, ok)
	assert.Contains(t, detail, "boom")

	onlyPermutedFails(func() (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, nil
	})
	detail, ok = orderInvariant(context.Background(), "spec", []byte(src))
	assert.False(t, ok)
	assert.Contains(t, detail, "no document")
}

// TestOrderInvariant_UncompilableBaselineIsNotAFinding pins the skip the test
// above depends on: when the re-encoded source yields no document, the oracle has
// no faithful baseline to ask its question of, so it declines rather than
// reporting the permutation.
//
// Both ways of yielding none are driven, because they are separate branches and
// a compiler reaches them for different reasons — an I/O or programmer fault
// against a source it declines to lower at all.
func TestOrderInvariant_UncompilableBaselineIsNotAFinding(t *testing.T) {
	tests := []struct {
		name string
		fail func() (*ir.Document, []ir.Diagnostic, error)
	}{
		{"the baseline errors", func() (*ir.Document, []ir.Diagnostic, error) {
			return nil, nil, errors.New("boom")
		}},
		{"the baseline compiles to no document", func() (*ir.Document, []ir.Diagnostic, error) {
			return nil, nil, nil
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := compile
			t.Cleanup(func() { compile = orig })
			compile = func(context.Context, string, []byte) (*ir.Document, []ir.Diagnostic, error) {
				return tc.fail()
			}
			detail, ok := orderInvariant(context.Background(), "spec", []byte("a: 1\nb: 2\n"))
			assert.True(t, ok, "no baseline is not a finding about the compiler")
			assert.Empty(t, detail)
		})
	}
}

// TestDiffOrderInvariants_ReportsEachChannel drives the three ways a permutation
// can differ, so each message is exercised rather than only the first.
func TestDiffOrderInvariants_ReportsEachChannel(t *testing.T) {
	t.Parallel()
	model := func(id ir.TypeID, wire string) *ir.Model {
		return &ir.Model{
			ID:         id,
			Name:       ir.Naming{Source: "M", Canonical: "m"},
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

// TestDiagnosticSet_IgnoresLineAndColumnButKeepsWhetherPositioned pins the two
// halves of the multiset key that a Position adds. A permutation moves a
// finding's line and column by design, so two findings differing only in those
// render as the same entry; but whether a finding carries a position at all is
// not something a permutation changes, so a positioned finding must still
// render differently from the same finding with none.
// TestOrderInvariant_PermutationArtifactsAreNotFindings pins the first half end
// to end, on a fixture whose finding the permutation moves in line and column.
func TestDiagnosticSet_IgnoresLineAndColumnButKeepsWhetherPositioned(t *testing.T) {
	t.Parallel()
	base := ir.Diagnostic{
		Severity:   ir.SeverityError,
		Code:       "x/y",
		Provenance: ir.Provenance{Source: 0, Position: ir.Position{Line: 5, Column: 1}},
	}

	movedLine := base
	movedLine.Provenance.Position = ir.Position{Line: 9, Column: 3}
	assert.Equal(t, diagnosticSet([]ir.Diagnostic{base}), diagnosticSet([]ir.Diagnostic{movedLine}),
		"line and column alone must not distinguish two findings")

	unpositioned := base
	unpositioned.Provenance.Position = ir.Position{}
	assert.NotEqual(t, diagnosticSet([]ir.Diagnostic{base}), diagnosticSet([]ir.Diagnostic{unpositioned}),
		"whether a finding has a position at all must still distinguish it from one that has none")
}

// TestDiagnosticSet_KeepsEachLocatorApart pins the rest of the key. Two findings
// that differ only in severity, code, source, pointer or node are two findings,
// since a permutation changes none of those; two that differ only in message
// text are one, for the reason diagnosticSet gives.
func TestDiagnosticSet_KeepsEachLocatorApart(t *testing.T) {
	t.Parallel()
	base := ir.Diagnostic{
		Severity:   ir.SeverityWarning,
		Code:       "x/y",
		Message:    "m",
		Provenance: ir.Provenance{Source: 0, Pointer: "/a", Node: "t/x/A"},
	}
	for _, tc := range []struct {
		name     string
		vary     func(d *ir.Diagnostic)
		distinct bool
	}{
		{"severity", func(d *ir.Diagnostic) { d.Severity = ir.SeverityError }, true},
		{"code", func(d *ir.Diagnostic) { d.Code = "x/z" }, true},
		{"source", func(d *ir.Diagnostic) { d.Provenance.Source = 1 }, true},
		{"pointer", func(d *ir.Diagnostic) { d.Provenance.Pointer = "/b" }, true},
		{"node", func(d *ir.Diagnostic) { d.Provenance.Node = "t/x/B" }, true},
		{"message", func(d *ir.Diagnostic) { d.Message = "other" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			varied := base
			tc.vary(&varied)
			got, want := diagnosticSet([]ir.Diagnostic{varied}), diagnosticSet([]ir.Diagnostic{base})
			if tc.distinct {
				assert.NotEqual(t, want, got, "a finding with another %s is another finding", tc.name)
				return
			}
			assert.Equal(t, want, got, "a finding differing only in its %s is the same finding", tc.name)
		})
	}
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
			ID:              "t/x/A",
			Examples:        ex,
			Properties:      p,
			AdditionalProps: &ir.AdditionalProps{Patterns: pp},
		}}}
	}
	detail, ok := diffOrderInvariants(build(false), build(true), nil)
	assert.True(t, ok, "reordering a source-ordered collection is the permutation, not a defect: %s", detail)
}

// TestOrderInvariant_ReachesTheCorpus is the reachability guard. The oracle
// declines silently on a source it cannot permute, so a rewrite that refused
// everything would leave the corpus sweep green while checking nothing.
//
// It asserts the oracle asked its question of every conformance spec, not
// merely that it ran, and guards both arms: the rewrite declines a source it
// cannot faithfully permute, and the baseline one whose re-encoding will not
// compile, a skip with no diagnostic behind it.
func TestOrderInvariant_ReachesTheCorpus(t *testing.T) {
	t.Parallel()
	const dir = "../../testdata/conformance/openapi"
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var specs, permuted, baselined int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		specs++
		data, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, readErr)

		baseline, ok := reencodeMappings(data)
		if !ok {
			continue
		}
		if doc, _, compileErr := compile(t.Context(), e.Name(), baseline); compileErr == nil && doc != nil {
			baselined++
		}
		rewritten, ok := reverseMappings(data)
		if ok && string(rewritten) != string(baseline) {
			permuted++
		}
	}
	require.Positive(t, specs, "found no conformance specs to permute")
	assert.Equal(t, specs, permuted,
		"every conformance spec must be permutable, or the oracle is silently skipping it")
	assert.Equal(t, specs, baselined,
		"every conformance spec's re-encoding must still compile, or the oracle has no baseline to compare against")
}

// TestOrderInvariant_PermutationArtifactsAreNotFindings covers sources whose two
// orders differ because of the rewrite rather than because of a lowering, so
// reporting one would be a false finding about the compiler.
//
// Each must come out OK, which Check returns only once the order oracle has run
// and found nothing. An earlier oracle's outcome is not order-dependence either,
// so asserting only that let a case whose fixture drew an error diagnostic pass
// without the oracle ever running (#550).
func TestOrderInvariant_PermutationArtifactsAreNotFindings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
	}{
		{
			// yaml.Marshal re-emits the flow mapping "{A}" — whose value is an
			// implicit null — as "{A: ''}", so the second compile reads an empty
			// string where the first read null. Block style keeps the null.
			name: "flow-style implicit null",
			src: "openapi: 3.0.0\ninfo: {title: 0, version: 0}\n" +
				"components:\n schemas:\n  0:\n   allOf:\n    - {A}\n",
		},
		{
			// Reversed, the alias precedes the anchor it names, which YAML forbids:
			// the permuted source does not parse at all.
			name: "an alias reordered above its anchor",
			src:  "openapi: 3.0.0\ninfo: {title: 0, version: 0}\n0: &m\n1: *m\n",
		},
		{
			// The permutation moves the fixture's one finding to another line
			// and column, both of which diagnosticSet sets aside;
			// TestOrderInvariant_LineAndColumnCaseMovesItsFinding holds the
			// fixture to drawing that finding and to moving it.
			name: "a diagnostic located by line and column",
			src:  positionedFindingSpec,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := Check(context.Background(), tc.name+".yaml", []byte(tc.src))
			assert.Equal(t, OutcomeOK, res.Outcome,
				"a permutation artifact must reach the order oracle and not be reported: %s", res.Detail)
		})
	}
}

// positionedFindingSpec draws one finding, located by line and column: a
// validation warning about an invalid callback expression. Reversing the source
// moves that expression to another line and, inside its flow mapping, to
// another column.
const positionedFindingSpec = "openapi: 3.0.0\ninfo: {title: t, version: v}\npaths:\n  /a:\n    get:\n" +
	"      responses:\n        \"200\":\n          description: ok\n" +
	"      callbacks:\n        cb: {notAnExpression: {}, \"{$request.body#/url}\": {}}\n"

// TestOrderInvariant_LineAndColumnCaseMovesItsFinding holds the "a diagnostic
// located by line and column" case to its question. That case would go on
// passing if its fixture stopped drawing the finding or the permutation stopped
// moving it, and the finding comes from the third-party parser's validation
// rather than a rule of the compiler's own, so it can change with no change
// here. The line and the column are each asserted because diagnosticSet sets
// each aside.
func TestOrderInvariant_LineAndColumnCaseMovesItsFinding(t *testing.T) {
	t.Parallel()
	positionIn := func(src []byte) ir.Position {
		t.Helper()
		_, diags, err := compile(t.Context(), "positioned.yaml", src)
		require.NoError(t, err)
		require.Len(t, diags, 1, "the fixture must draw exactly one finding")
		return diags[0].Provenance.Position
	}
	baseline, ok := reencodeMappings([]byte(positionedFindingSpec))
	require.True(t, ok, "the fixture must re-encode")
	reversed, ok := reverseMappings([]byte(positionedFindingSpec))
	require.True(t, ok, "the fixture must be permutable")

	asWritten, permuted := positionIn(baseline), positionIn(reversed)
	assert.NotEqual(t, asWritten.Line, permuted.Line, "the permutation must move the finding to another line")
	assert.NotEqual(t, asWritten.Column, permuted.Column, "the permutation must move the finding to another column")
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

// TestReencodeMappings_EncodeFailureIsRefused drives the defensive re-encode
// path on the baseline arm, the counterpart of the reverseMappings case above.
func TestReencodeMappings_EncodeFailureIsRefused(t *testing.T) {
	orig := encodeYAML
	t.Cleanup(func() { encodeYAML = orig })
	encodeYAML = func(any) ([]byte, error) { return nil, errors.New("boom") }

	got, ok := reencodeMappings([]byte("a: 1\nb: 2\n"))
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
		m := &ir.Model{
			ID:         id,
			Name:       ir.Naming{Source: "M", Canonical: "m"},
			Provenance: ir.Provenance{Pointer: jsontext.Pointer("/" + path)},
		}
		return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{m.ID: m}}, nil, nil
	}

	r := Check(context.Background(), "spec", []byte(src))
	assert.Equal(t, OutcomeOrderDependent, r.Outcome, "detail: %s", r.Detail)
	assert.Contains(t, r.Detail, "declaration order")
}
