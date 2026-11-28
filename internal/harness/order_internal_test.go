// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// TestReverseMappings_Permutes covers the rewrite's shapes: what it reverses,
// what it leaves alone, and what it declines, and why.
func TestReverseMappings_Permutes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		src      string
		want     string  // the permuted source, when the rewrite permutes it
		declines Decline // why the rewrite declines it, when it does
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
			// A stream's documents keep their order, and each is permuted, so the
			// compiler reads the stream it read before.
			name: "every document of a stream reverses",
			src:  "a: 1\nb: 2\n---\nc: 3\nd: 4\n",
			want: "b: 2\na: 1\n---\nd: 4\nc: 3\n",
		},
		{
			// Reversing decides which declaration wins, so the permutation is not
			// meaning-preserving and the source is excluded rather than reported (#95).
			name:     "a duplicate key is declined",
			src:      "a: 1\na: 2\n",
			declines: DeclineDuplicateKey,
		},
		{
			name:     "a duplicate key nested inside is declined",
			src:      "outer:\n  dup: 1\n  dup: 2\n",
			declines: DeclineDuplicateKey,
		},
		{
			name:     "a duplicate key in a later document is declined",
			src:      "a: 1\nb: 2\n---\ndup: 1\ndup: 2\n",
			declines: DeclineDuplicateKey,
		},
		{
			// The parser reads an alias key as the scalar it names, so this mapping
			// writes rate twice however it spells it (#1105).
			name:     "a key repeated through an alias is declined",
			src:      "- {x: &k rate}\n- {rate: 1, *k : 2}\n",
			declines: DeclineDuplicateKey,
		},
		{
			// A key that is no scalar reads as the empty string, whichever anchor
			// names it (docs/parser-library.md#non-scalar-key).
			name:     "two aliases of mappings are one key",
			src:      "- &a {x: 1}\n- &b {y: 2}\n- {? *a : 1, ? *b : 2}\n",
			declines: DeclineDuplicateKey,
		},
		{
			// An anchor's name is no key: the alias reads as other, so nothing repeats.
			name: "an alias named like another key repeats nothing",
			src:  "- {x: &rate other}\n- {rate: 1, *rate : 2}\n",
			want: "- {x: &rate other}\n- {*rate: 2, rate: 1}\n",
		},
		{
			// Their order decides which merge source wins.
			name:     "two merge keys are declined",
			src:      "- &a {x: 1}\n- &b {y: 2}\n- {<<: *a, <<: *b}\n",
			declines: DeclineDuplicateKey,
		},
		{
			// Reversed, the alias would come first, so the anchor moves to it and
			// the alias takes the anchor's place.
			name: "an alias reversed above its anchor takes the anchor",
			src:  "anchor: &a\n  x: 1\n  y: 2\nuse: *a\n",
			want: "use: &a\n    y: 2\n    x: 1\nanchor: *a\n",
		},
		{
			// The merge source moves into the merge key it feeds.
			name: "a merge source reversed below its merge key moves to it",
			src:  "base: &b {x: 1}\nuse: {<<: *b, y: 2}\n",
			want: "use: {y: 2, <<: &b {x: 1}}\nbase: *b\n",
		},
		{
			// The encoder writes an alias by name, and with a name given twice a
			// moved anchor can rebind an alias to the other node.
			name:     "an anchor name given twice is declined",
			src:      "p: {u: &a 1, v: *a}\nq: &a 2\n",
			declines: DeclineAnchorRedefined,
		},
		{
			name:     "an unparseable source is declined",
			src:      "a: [unclosed\n",
			declines: DeclineNotYAML,
		},
		{
			name:     "a stream longer than the rewrite reads is declined",
			src:      strings.Repeat("a: 1\n---\n", maxPermutedDocuments) + "a: 1\n",
			declines: DeclineTooManyDocuments,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, why := reverseMappings([]byte(tc.src))
			assert.Equal(t, tc.declines, why)
			assert.Equal(t, tc.want, string(got))
			if why == "" {
				assertSameYAML(t, tc.src, string(got))
			}
		})
	}
}

// assertSameYAML asserts that two streams decode to the same data, every alias
// expanded and every mapping read regardless of its order: what a permutation
// must leave as it found it.
func assertSameYAML(t *testing.T, want, got string) {
	t.Helper()
	decodeAll := func(src string) []any {
		var docs []any
		dec := yaml.NewDecoder(strings.NewReader(src))
		for {
			var doc any
			err := dec.Decode(&doc)
			if errors.Is(err, io.EOF) {
				return docs
			}
			require.NoError(t, err, "%q must decode", src)
			docs = append(docs, doc)
		}
	}
	assert.Equal(t, decodeAll(want), decodeAll(got), "the permutation must say what the source said")
}

// TestReverseMappings_AliasIsNotFollowed pins that an anchored mapping is
// reversed once, at its anchor, rather than once per alias pointing at it.
//
// The anchor and its use sit in a sequence, whose order the rewrite leaves
// alone, so the anchor still precedes the alias afterwards and stays where it
// is. Declared as two keys of one mapping they would swap, and the anchor would
// move to the alias (TestReverseMappings_Permutes).
func TestReverseMappings_AliasIsNotFollowed(t *testing.T) {
	t.Parallel()
	got, why := reverseMappings([]byte("items:\n  - anchor: &a\n      x: 1\n      y: 2\n  - use: *a\n"))
	require.Empty(t, why)
	assert.Equal(t, "items:\n    - anchor: &a\n        y: 2\n        x: 1\n    - use: *a\n", string(got),
		"the anchored mapping reverses once and the alias still points at it")
}

// TestReverseMappings_AnAnchorMovesAheadOfEveryAlias holds the move to what
// YAML requires: however anchors and aliases interleave, nest or chain, the
// permuted stream parses, which it would not with an alias written before its
// anchor, and decodes to what the source did. The chain is the shape a merge
// chain through components takes, each link merging the one before it.
func TestReverseMappings_AnAnchorMovesAheadOfEveryAlias(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"a: &x {k: 1}\nb: {inner: *x}\nc: *x\n",
		"a: &x {k: &y [1, 2]}\nb: *y\nc: *x\nd: *y\n",
		"s:\n  l0: &l0 {a: 1}\n  l1: &l1 {<<: *l0, b: 2}\n  l2: &l2 {<<: *l1, c: 3}\n  l3: {<<: *l2, d: 4}\n",
		"keys: {x: &k rate}\nuse: {*k : 1, other: 2}\n",
		"- {a: &x 1, b: *x}\n- {c: *x, d: &y 2, e: *y}\n",
	} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			got, why := reverseMappings([]byte(src))
			require.Empty(t, why)
			require.NotEqual(t, src, string(got), "the source has mappings to permute")
			assertSameYAML(t, src, string(got))
		})
	}
}

// TestReverseNode_DepthBound covers the walks' bound. No document the compiler
// accepts nests this deep, so the bound exists to keep each walk terminating
// rather than to reject real input. Moving an anchor can carry its subtree
// deeper than the source nests, so the anchor walks hold the bound too.
func TestReverseNode_DepthBound(t *testing.T) {
	t.Parallel()
	deep := yamlMapping()
	assert.Equal(t, DeclineTooDeep, reverseNode(&deep, maxReverseDepth+1),
		"past the bound the rewrite declines rather than descending further")
	assert.Empty(t, reverseNode(nil, 0), "a nil node is nothing to reverse, not a refusal")
	assert.Equal(t, DeclineTooDeep, findAnchors(&deep, maxReverseDepth+1, map[*yaml.Node]slot{}, map[string]*yaml.Node{}))
	assert.Equal(t, DeclineTooDeep, hoistFirstUses(&deep, maxReverseDepth+1, map[*yaml.Node]slot{}, map[*yaml.Node]bool{}))

	anchored := &yaml.Node{Kind: yaml.MappingNode, Anchor: "a", Content: []*yaml.Node{scalarNode("k"), scalarNode("v")}}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{anchored}}
	nested := doc
	for range maxReverseDepth {
		nested = &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{nested}}
	}
	assert.Equal(t, DeclineTooDeep, hoistAnchors(nested), "an anchor below the bound is not sought")

	// Moving an anchored subtree to a deeper alias can deepen the tree past
	// what was sought, so the move holds the bound below the top too.
	assert.Equal(t, DeclineTooDeep, hoistFirstUses(nested, 0, map[*yaml.Node]slot{}, map[*yaml.Node]bool{}))
}

// TestReversedPairs_OddTrailingElementIsKept pins that a malformed mapping is
// passed through rather than losing its trailing node. A parsed mapping always
// holds key/value pairs, so this guards the helper rather than a reachable input.
func TestReversedPairs_OddTrailingElementIsKept(t *testing.T) {
	t.Parallel()
	a, b, node := scalarNode("a"), scalarNode("b"), scalarNode("c")
	got := reversedPairs([]*yaml.Node{a, b, node})
	require.Len(t, got, 3)
	assert.Equal(t, []*yaml.Node{a, b, node}, got, "one pair, reversed onto itself, then the odd tail")
}

// TestOrderInvariant_DeclinesWhatItCannotPermute pins each way the oracle
// declines to ask its question: none reads as a finding, and none as a pass.
func TestOrderInvariant_DeclinesWhatItCannotPermute(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		why       Decline
	}{
		{"a duplicate key excludes the source", "a: 1\na: 2\n", DeclineDuplicateKey},
		{"a key repeated through an alias excludes the source", `openapi: 3.1.0
info: {title: T, version: "1"}
paths: {}
components:
  schemas:
    S:
      allOf:
        - {x-k: &k rate}
        - {type: object, properties: {rate: {type: string}, *k : {type: integer}}}
`, DeclineDuplicateKey},
		{"an anchor name given twice", "openapi: 3.1.0\ninfo: {title: &t T, version: &t \"1\"}\npaths: {}\n",
			DeclineAnchorRedefined},
		{"a source with nothing to permute", "a: 1\n", DeclineNothingToPermute},
		{"an unparseable source excludes the baseline too", "a: [unclosed\n", DeclineNotYAML},
		{"a stream longer than the rewrite reads", strings.Repeat("a: 1\n---\n", maxPermutedDocuments+1),
			DeclineTooManyDocuments},
		{"a re-encoding that reads as no spec", "a: 1\nb: 2\n", DeclineReencodingNoSpec},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := orderInvariant(context.Background(), "spec", []byte(tc.src), true)
			assert.Equal(t, declined(OracleOrder, tc.why), got)
		})
	}
}

// TestOrderInvariant_CompileFailureIsReported covers the error paths of the
// two compiles the oracle makes, which a correct compiler never produces on a
// source that already compiled.
//
// The seam answers for the baseline and fails only for the permutation, and
// then fails the baseline alone with a Go error, which is never the spec's
// and so is reported too, where a baseline that reads as no spec is declined
// (the test below).
func TestOrderInvariant_CompileFailureIsReported(t *testing.T) {
	const src = "a: 1\nb: 2\n"

	baseline, why := reencodeMappings([]byte(src))
	require.Empty(t, why)

	onlyPermutedFails := func(fail func() (*ir.Document, []ir.Diagnostic, error)) {
		swapCompile(t, func(_ context.Context, _ string, data []byte, _ []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
			if bytes.Equal(data, baseline) {
				return &ir.Document{}, nil, nil
			}
			return fail()
		})
	}

	onlyPermutedFails(func() (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, errors.New("boom")
	})
	got := orderInvariant(context.Background(), "spec", []byte(src), true)
	assert.Equal(t, found(OracleOrder, "recompile permuted: boom"), got)

	swapCompile(t, func(_ context.Context, _ string, data []byte, _ []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		if bytes.Equal(data, baseline) {
			return nil, nil, errors.New("baseline boom")
		}
		return &ir.Document{}, nil, nil
	})
	got = orderInvariant(context.Background(), "spec", []byte(src), true)
	assert.Equal(t, found(OracleOrder, "compile re-encoded: baseline boom"), got)
}

// TestOrderInvariant_AReencodingNoCompilerTakesIsDeclined pins the skip the
// test above depends on: when the re-encoded source reads as no spec, the
// oracle has no baseline to ask its question of, so it declines rather than
// reporting the permutation.
func TestOrderInvariant_AReencodingNoCompilerTakesIsDeclined(t *testing.T) {
	swapCompile(t, func(context.Context, string, []byte, []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		return nil, nil, fmt.Errorf("no compiler takes it: %w", errUnrecognized)
	})

	got := orderInvariant(context.Background(), "spec", []byte("a: 1\nb: 2\n"), true)

	assert.Equal(t, declined(OracleOrder, DeclineReencodingNoSpec), got)
}

// TestOrderInvariant_RefusalsCompareTheirFindings pins what the oracle asks of
// a source the compiler refuses in an order: two refusals compare what they
// report, and a refusal in one order alone is a finding, whichever order it is.
// A refusal of the re-encoding alone, where the source compiles, is the
// rewrite's, so the oracle declines rather than compare two refusals.
func TestOrderInvariant_RefusalsCompareTheirFindings(t *testing.T) {
	const src = "a: 1\nb: 2\n"
	baseline, why := reencodeMappings([]byte(src))
	require.Empty(t, why)
	refusal := func(code string) []ir.Diagnostic {
		return []ir.Diagnostic{{Severity: ir.SeverityError, Code: code, Provenance: ir.Provenance{Pointer: "/a"}}}
	}
	for _, tc := range []struct {
		name                  string
		compiled              bool
		asWritten, permuted   *ir.Document
		writtenDiags, permDia []ir.Diagnostic
		want                  Verdict
	}{
		{"two refusals alike", false, nil, nil, refusal("x/refused"), refusal("x/refused"), passed(OracleOrder)},
		{"two refusals apart", false, nil, nil, refusal("x/refused"), refusal("x/other"), found(OracleOrder,
			"diagnostics depend on declaration order (-as-written +reversed):\n")},
		{"a refusal permuted only", true, &ir.Document{}, nil, nil, refusal("x/refused"), found(OracleOrder,
			"the permuted source compiles to no document, and as written to one")},
		{"a refusal as written only", false, nil, &ir.Document{}, refusal("x/refused"), nil, found(OracleOrder,
			"the permuted source compiles to a document, and as written to none")},
		{"a refusal of the re-encoding alone", true, nil, nil, refusal("x/refused"), refusal("x/refused"),
			declined(OracleOrder, DeclineReencodingRefused)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			swapCompile(t, func(_ context.Context, _ string, data []byte, _ []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
				if bytes.Equal(data, baseline) {
					return tc.asWritten, tc.writtenDiags, nil
				}
				return tc.permuted, tc.permDia, nil
			})

			got := orderInvariant(context.Background(), "spec", []byte(src), tc.compiled)

			assert.Equal(t, tc.want.Outcome, got.Outcome)
			assert.Equal(t, tc.want.Declined, got.Declined)
			assert.True(t, strings.HasPrefix(got.Detail, tc.want.Detail), got.Detail)
		})
	}
}

// TestDiffOrderInvariants_ReportsEachChannel drives each way a permutation can
// differ, so each message is exercised rather than only the first. Each part
// of the document is compared, an operation's as well as a registry's (#684).
func TestDiffOrderInvariants_ReportsEachChannel(t *testing.T) {
	t.Parallel()
	model := func(id ir.TypeID, wire string) *ir.Model {
		return &ir.Model{
			ID:         id,
			Name:       ir.Naming{Source: "M", Canonical: "m"},
			Properties: []ir.Property{{ID: ir.PropertyID("property/x/" + wire), WireName: wire}},
		}
	}
	one := &ir.Document{Types: ir.TypeRegistry{"type/x/A": model("type/x/A", "a")}}
	const differs = "document depends on declaration order"

	t.Run("a different number of types", func(t *testing.T) {
		t.Parallel()
		detail, ok := diffOrderInvariants(one, nil, &ir.Document{Types: ir.TypeRegistry{}}, nil)
		assert.False(t, ok)
		assert.Contains(t, detail, "interns 0 types against 1")
	})

	t.Run("a type that changed shape", func(t *testing.T) {
		t.Parallel()
		other := &ir.Document{Types: ir.TypeRegistry{"type/x/A": model("type/x/A", "b")}}
		detail, ok := diffOrderInvariants(one, nil, other, nil)
		assert.False(t, ok)
		assert.Contains(t, detail, differs)
	})

	t.Run("a server that changed", func(t *testing.T) {
		t.Parallel()
		server := func(url string) *ir.Document {
			return &ir.Document{Servers: map[ir.ServerID]ir.Server{"server/x/a": {ID: "server/x/a", URLTemplate: url}}}
		}
		detail, ok := diffOrderInvariants(server("/a"), nil, server("/b"), nil)
		assert.False(t, ok)
		assert.Contains(t, detail, differs)
	})

	t.Run("an auth scheme that changed", func(t *testing.T) {
		t.Parallel()
		scheme := func(pointer jsontext.Pointer) *ir.Document {
			return &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{
				"auth/x/a": {ID: "auth/x/a", Provenance: ir.Provenance{Pointer: pointer}},
			}}
		}
		detail, ok := diffOrderInvariants(scheme("/a"), nil, scheme("/b"), nil)
		assert.False(t, ok)
		assert.Contains(t, detail, differs)
	})

	t.Run("a header whose type changed", func(t *testing.T) {
		t.Parallel()
		// What #606 flipped: the type an operation's response header names.
		header := func(target ir.TypeID) *ir.Document {
			return withOperation(ir.Operation{ID: "operation/x/get", Responses: []ir.Response{{
				Headers: []ir.Header{{ID: "property/x/h", Type: ir.TypeRef{Target: target}}},
			}}})
		}
		detail, ok := diffOrderInvariants(header("type/primitive/string"), nil, header("type/primitive/integer"), nil)
		assert.False(t, ok)
		assert.Contains(t, detail, differs)
		assert.Contains(t, detail, `"target": "type/primitive/integer"`, "the diff names what changed")
	})

	t.Run("the hash of the source compiled", func(t *testing.T) {
		t.Parallel()
		// The permutation rewrites those bytes, so only the first entry is
		// forgiven: another source read is the same file in both orders.
		sources := func(own, other string) *ir.Document {
			return &ir.Document{Sources: []ir.SourceInfo{{Path: "a", Hash: own}, {Path: "b", Hash: other}}}
		}
		detail, ok := diffOrderInvariants(sources("1", "x"), nil, sources("2", "x"), nil)
		assert.True(t, ok, detail)
		detail, ok = diffOrderInvariants(sources("1", "x"), nil, sources("1", "y"), nil)
		assert.False(t, ok)
		assert.Contains(t, detail, differs)
	})

	t.Run("a different set of diagnostics", func(t *testing.T) {
		t.Parallel()
		other := &ir.Document{Types: ir.TypeRegistry{"type/x/A": model("type/x/A", "a")}}
		detail, ok := diffOrderInvariants(one, nil, other,
			[]ir.Diagnostic{{Severity: ir.SeverityInfo, Code: "c", Provenance: ir.Provenance{Pointer: "/p"}}})
		assert.False(t, ok)
		assert.Contains(t, detail, "diagnostics depend on declaration order")
	})

	t.Run("diagnostics held on the document alike", func(t *testing.T) {
		t.Parallel()
		// The document's own list follows walk order, so it is compared as the
		// multiset the run's list is, not as written.
		withDiags := func(codes ...string) *ir.Document {
			doc := &ir.Document{}
			for _, c := range codes {
				doc.Diagnostics = append(doc.Diagnostics, ir.Diagnostic{Severity: ir.SeverityInfo, Code: c})
			}
			return doc
		}
		detail, ok := diffOrderInvariants(withDiags("a", "b"), nil, withDiags("b", "a"), nil)
		assert.True(t, ok, detail)
	})
}

// withOperation is a document of one service holding operation in one group.
func withOperation(operation ir.Operation) *ir.Document {
	return &ir.Document{Services: []ir.Service{{ID: "service/x/S", Groups: []ir.OperationGroup{{
		ID: "operation-group/x/G", Operations: []ir.Operation{operation},
	}}}}}
}

// TestDiffOrderInvariants_AFlowsScopesFollowTheSource pins that the auth registry
// compares a flow's scopes as a set: they keep source order, which a permutation
// reverses, while a changed scope still differs.
func TestDiffOrderInvariants_AFlowsScopesFollowTheSource(t *testing.T) {
	t.Parallel()
	doc := func(scopes ...ir.OAuthScope) *ir.Document {
		return &ir.Document{Auth: map[ir.AuthID]ir.AuthScheme{
			"auth/x/a": {ID: "auth/x/a", Flows: []ir.OAuthFlow{{Scopes: scopes}}},
		}}
	}
	read, write := ir.OAuthScope{Name: "read"}, ir.OAuthScope{Name: "write"}

	detail, ok := diffOrderInvariants(doc(read, write), nil, doc(write, read), nil)
	assert.True(t, ok, detail)

	_, ok = diffOrderInvariants(doc(read, write), nil, doc(read, ir.OAuthScope{Name: "admin"}), nil)
	assert.False(t, ok)
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
// that differ only in severity, code or any part of their provenance but its
// position are two findings, since a permutation changes none of those; two
// that differ only in message text are one, for the reason diagnosticSet gives.
func TestDiagnosticSet_KeepsEachLocatorApart(t *testing.T) {
	t.Parallel()
	base := ir.Diagnostic{
		Severity:   ir.SeverityWarning,
		Code:       "x/y",
		Message:    "m",
		Provenance: ir.Provenance{Source: 0, Pointer: "/a", Node: "type/x/A"},
	}
	for _, tc := range []struct {
		name     string
		vary     func(diagnostic *ir.Diagnostic)
		distinct bool
	}{
		{"severity", func(diagnostic *ir.Diagnostic) { diagnostic.Severity = ir.SeverityError }, true},
		{"code", func(diagnostic *ir.Diagnostic) { diagnostic.Code = "x/z" }, true},
		{"source", func(diagnostic *ir.Diagnostic) { diagnostic.Provenance.Source = 1 }, true},
		{"pointer", func(diagnostic *ir.Diagnostic) { diagnostic.Provenance.Pointer = "/b" }, true},
		{"node", func(diagnostic *ir.Diagnostic) { diagnostic.Provenance.Node = "type/x/B" }, true},
		{"writer", func(diagnostic *ir.Diagnostic) { diagnostic.Provenance.WrittenBy = 1 }, true},
		{"inferred", func(diagnostic *ir.Diagnostic) { diagnostic.Provenance.Inferred = "group-path-prefix" }, true},
		{"message", func(diagnostic *ir.Diagnostic) { diagnostic.Message = "other" }, false},
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

// reorderedCollections is a document holding two elements of every collection
// whose order a mapping's entry order decides, each element as given, or each
// pair swapped, as reversing the source swaps them. differ changes the second
// element of the collection it names, so each comparison can be shown to read
// what it forgives the order of.
func reorderedCollections(reversed bool, differ string) *ir.Document {
	pair := func(a, b string) (string, string) {
		if reversed {
			return b, a
		}
		return a, b
	}
	second := func(of, v string) string {
		if of == differ {
			return v + "-changed"
		}
		return v
	}
	p0, p1 := pair("property/x/a", second("properties", "property/x/b"))
	pp0, pp1 := pair("^a", second("patterns", "^b"))
	ex0, ex1 := pair("one", second("examples", "two"))
	v0, v1 := pair("a", second("variables", "b"))
	h0, h1 := pair("property/x/h1", second("headers", "property/x/h2"))
	m0, m1 := pair("application/json", second("contents", "text/plain"))
	lp0, lp1 := pair("parameter/x/a", second("link params", "parameter/x/b"))
	cb0, cb1 := pair("onA", second("callbacks", "onB"))
	su0, su1 := pair("auth/x/a", second("scheme uses", "auth/x/b"))
	rc0, rc1 := pair("application/json", second("request content types", "text/plain"))
	rawB := `"b":2`
	if differ == "raw members" {
		rawB = `"b":3`
	}
	j0, j1 := pair(`"a":1`, rawB)
	status := func(of string, code int) ir.ResponseConditions {
		if of == differ {
			code++
		}
		return ir.ResponseConditions{StatusCodes: []ir.StatusRange{{From: code, To: code}}}
	}
	links := []ir.OperationLink{
		{Name: ir.Naming{Source: "first"}, Params: []ir.LinkParam{{Param: ir.ParameterID(lp0)}, {Param: ir.ParameterID(lp1)}}},
		{Name: ir.Naming{Source: second("links", "second")}},
	}
	responses := []ir.Response{{}, {Conditions: status("responses", 201)}}
	errs := []ir.ErrorCase{{Conditions: status("", 400)}, {Conditions: status("errors", 404)}}
	if reversed {
		slices.Reverse(links)
		slices.Reverse(responses)
		slices.Reverse(errs)
	}

	// The first response, at 200 in both orders, holds the collections a
	// response holds, so the two responses are told apart by their status.
	full := ir.Response{Conditions: status("", 200),
		Headers: []ir.Header{{ID: ir.PropertyID(h0)}, {ID: ir.PropertyID(h1)}},
		Links:   links,
		Payload: &ir.Payload{Contents: []ir.Content{{MediaType: m0}, {MediaType: m1}}},
	}
	for i := range responses {
		if responses[i].Conditions.StatusCodes == nil {
			responses[i] = full
		}
	}
	operation := func(id string) ir.Operation {
		return ir.Operation{ID: ir.OperationID(id), Responses: responses, Errors: errs,
			Auth: []ir.AuthRequirement{{Schemes: []ir.SchemeUse{{Scheme: ir.AuthID(su0)}, {Scheme: ir.AuthID(su1)}}}},
			Bindings: ir.OperationBindings{HTTP: []ir.HTTPBinding{{
				RequestContentTypes: []string{rc0, rc1},
				Callbacks:           []ir.Callback{{Name: cb0}, {Name: cb1}},
			}}},
			Unmodeled: ir.Unmodeled{"x": {Value: ir.RawValue("{" + j0 + "," + j1 + "}")}},
		}
	}
	operations := []ir.Operation{operation("operation/x/a"), operation(second("operations", "operation/x/b"))}
	groups := []ir.OperationGroup{{ID: "operation-group/x/a", Operations: operations}, {ID: ir.GroupID(second("groups", "operation-group/x/b"))}}
	if reversed {
		slices.Reverse(operations)
		slices.Reverse(groups)
	}
	return &ir.Document{
		Types: ir.TypeRegistry{"type/x/A": &ir.Model{
			ID:                   "type/x/A",
			Examples:             []ir.Example{{Name: ex0}, {Name: ex1}},
			Properties:           []ir.Property{{ID: ir.PropertyID(p0)}, {ID: ir.PropertyID(p1)}},
			AdditionalProperties: &ir.AdditionalProperties{Patterns: []ir.PatternProperties{{Pattern: pp0}, {Pattern: pp1}}},
		}},
		Servers: map[ir.ServerID]ir.Server{"server/x/a": {ID: "server/x/a",
			Variables: []ir.ServerVariable{{Name: v0}, {Name: v1}}}},
		Services: []ir.Service{{ID: "service/x/S", Groups: groups}},
	}
}

// reorderable names each collection reorderedCollections holds.
var reorderable = []string{
	"properties", "patterns", "examples", "variables", "headers", "operations", "groups", "contents",
	"links", "link params", "callbacks", "scheme uses", "request content types", "raw members",
	"responses", "errors",
}

// TestDiffOrderInvariants_ReorderedCollectionsAreNotAFinding pins the
// order-blind comparison: each collection a mapping's entry order decides is
// held in source order, so a permutation reorders it by design and must not
// be reported, while an element of it that changed still is.
func TestDiffOrderInvariants_ReorderedCollectionsAreNotAFinding(t *testing.T) {
	t.Parallel()
	detail, ok := diffOrderInvariants(reorderedCollections(false, ""), nil, reorderedCollections(true, ""), nil)
	assert.True(t, ok, "reordering a source-ordered collection is the permutation, not a defect: %s", detail)

	for _, of := range reorderable {
		t.Run(of, func(t *testing.T) {
			t.Parallel()
			detail, ok := diffOrderInvariants(reorderedCollections(false, ""), nil, reorderedCollections(true, of), nil)
			assert.False(t, ok, "a changed element of the %s still differs", of)
			assert.Contains(t, detail, "document depends on declaration order")
		})
	}
}

// TestDiffOrderInvariants_ASequencesOrderIsCompared holds the forgiveness to
// the collections listed: one a sequence orders, an operation's parameters or
// an enum's members, is compared in order.
func TestDiffOrderInvariants_ASequencesOrderIsCompared(t *testing.T) {
	t.Parallel()
	params := func(ids ...ir.ParameterID) *ir.Document {
		operation := ir.Operation{ID: "operation/x/get"}
		for _, id := range ids {
			operation.Params = append(operation.Params, ir.Parameter{ID: id})
		}
		return withOperation(operation)
	}
	detail, ok := diffOrderInvariants(params("parameter/x/a", "parameter/x/b"), nil, params("parameter/x/b", "parameter/x/a"), nil)
	assert.False(t, ok)
	assert.Contains(t, detail, "document depends on declaration order")

	members := func(ids ...ir.EnumMemberID) *ir.Document {
		enum := &ir.Enum{ID: "type/x/E"}
		for _, id := range ids {
			enum.Members = append(enum.Members, ir.EnumMember{ID: id})
		}
		return &ir.Document{Types: ir.TypeRegistry{enum.ID: enum}}
	}
	detail, ok = diffOrderInvariants(members("enum-member/x/a", "enum-member/x/b"), nil, members("enum-member/x/b", "enum-member/x/a"), nil)
	assert.False(t, ok)
	assert.Contains(t, detail, "document depends on declaration order")
}

// TestDiffOrderInvariants_RawBytesAreNotJSON pins the reach of the verbatim
// comparison: it reads an ir.RawValue alone, not a value's bytes, which a
// []byte option on the named type would reach too and read as JSON.
func TestDiffOrderInvariants_RawBytesAreNotJSON(t *testing.T) {
	t.Parallel()
	bytesOf := func(b string) *ir.Document {
		value := ir.Value{Kind: ir.ValueBytes, Bytes: []byte(b)}
		return &ir.Document{Types: ir.TypeRegistry{"type/x/A": &ir.Model{ID: "type/x/A",
			Properties: []ir.Property{{ID: "property/x/a", Default: &value}}}}}
	}
	detail, ok := diffOrderInvariants(bytesOf("\x00not json"), nil, bytesOf("\x00not json"), nil)
	assert.True(t, ok, detail)
	_, ok = diffOrderInvariants(bytesOf("\x00not json"), nil, bytesOf("\x00other"), nil)
	assert.False(t, ok)
}

// TestDiffOrderInvariants_ARawValueForgivesMemberOrderAlone pins what the
// comparison forgives a verbatim JSON value: the order of an object's members,
// at any depth, and nothing else.
func TestDiffOrderInvariants_ARawValueForgivesMemberOrderAlone(t *testing.T) {
	t.Parallel()
	raw := func(v string) *ir.Document {
		return &ir.Document{Unmodeled: ir.Unmodeled{"x": {Value: ir.RawValue(v)}}}
	}
	for _, tc := range []struct {
		name, a, b string
		same       bool
	}{
		{"members reordered", `{"a":1,"b":{"c":2,"d":3}}`, `{"b":{"d":3,"c":2},"a":1}`, true},
		{"an array reordered", `[1,2]`, `[2,1]`, false},
		{"a number spelled otherwise", `1.0`, `1`, false},
		{"a value changed", `{"a":1}`, `{"a":2}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			detail, ok := diffOrderInvariants(raw(tc.a), nil, raw(tc.b), nil)
			assert.Equal(t, tc.same, ok, detail)
		})
	}
}

// TestDiffOrderInvariants_ADocumentThatWillNotEncodeIsAFinding drives the
// comparison's encoding faults, on either arm: a document whose verbatim value
// is no JSON, and an element of a sorted collection that holds one.
func TestDiffOrderInvariants_ADocumentThatWillNotEncodeIsAFinding(t *testing.T) {
	t.Parallel()
	inProperty := &ir.Document{Types: ir.TypeRegistry{"type/x/A": &ir.Model{ID: "type/x/A", Properties: []ir.Property{
		{ID: "property/x/a", Unmodeled: ir.Unmodeled{"x": {Value: ir.RawValue("{invalid")}}},
	}}}}
	model := &ir.Document{Types: ir.TypeRegistry{"type/x/A": &ir.Model{ID: "type/x/A"}}}
	for _, tc := range []struct {
		name          string
		first, second *ir.Document
		want          string
	}{
		{"as written", badExtDoc(), &ir.Document{}, "encode as written: "},
		{"permuted", &ir.Document{}, badExtDoc(), "encode permuted: "},
		{"in an element of a sorted collection", model, inProperty, "encode permuted: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			detail, ok := diffOrderInvariants(tc.first, nil, tc.second, nil)
			assert.False(t, ok)
			assert.True(t, strings.HasPrefix(detail, tc.want), detail)
		})
	}
}

// numbers builds an object value whose members are numbers, in the order
// given: name, value, name, value.
func numbers(pairs ...string) ir.Value {
	value := ir.Value{Kind: ir.ValueObject}
	for i := 0; i+1 < len(pairs); i += 2 {
		value.Object = append(value.Object, ir.Field{Name: pairs[i], Value: ir.Value{Kind: ir.ValueNumber, Number: ir.BigVal(pairs[i+1])}})
	}
	return value
}

// objectValuedModel is a model whose property default and unnamed examples hold
// object values, one of them inside a list, written as the source declares them
// or with every object's members reversed, which is what reversing the source
// does to them (#683).
func objectValuedModel(reversed bool, defaultX string) *ir.Document {
	def := numbers("x", defaultX, "y", "2")
	first, second := numbers("x", "1", "y", "4"), numbers("x", "2", "y", "3")
	nested := ir.Value{Kind: ir.ValueList, List: []ir.Value{numbers("a", "1", "b", "2")}}
	if reversed {
		def = numbers("y", "2", "x", defaultX)
		first, second = numbers("y", "4", "x", "1"), numbers("y", "3", "x", "2")
		nested = ir.Value{Kind: ir.ValueList, List: []ir.Value{numbers("b", "2", "a", "1")}}
	}
	return &ir.Document{Types: ir.TypeRegistry{"type/x/A": &ir.Model{
		ID:         "type/x/A",
		Examples:   []ir.Example{{Value: &first}, {Value: &second}, {Value: &nested}},
		Properties: []ir.Property{{ID: "property/x/a", WireName: "a", Default: &def}},
	}}}
}

// TestDiffOrderInvariants_ReorderedObjectMembersAreNotAFinding pins #683. Its
// two unnamed examples order oppositely once their members are reversed, so a
// comparison that sorted examples by a rendering as written would pair each
// with the other one.
func TestDiffOrderInvariants_ReorderedObjectMembersAreNotAFinding(t *testing.T) {
	t.Parallel()
	detail, ok := diffOrderInvariants(objectValuedModel(false, "1"), nil, objectValuedModel(true, "1"), nil)
	assert.True(t, ok, "reversing an object value's members is the permutation, not a defect: %s", detail)
}

// TestDiffOrderInvariants_ChangedObjectMemberIsAFinding holds the sorting to
// what it may forgive: a member whose value changed still differs.
func TestDiffOrderInvariants_ChangedObjectMemberIsAFinding(t *testing.T) {
	t.Parallel()
	detail, ok := diffOrderInvariants(objectValuedModel(false, "1"), nil, objectValuedModel(true, "9"), nil)
	assert.False(t, ok)
	assert.Contains(t, detail, "document depends on declaration order")
}

// TestDiffOrderInvariants_NamedExamplesCompareAsAMultiset pins what the
// comparison forgives examples: the order of a list holding a named one, an
// `examples` mapping's entry, and nothing else. A list of unnamed ones, a
// schema's `examples` array, keeps its order. A repeated example is counted.
func TestDiffOrderInvariants_NamedExamplesCompareAsAMultiset(t *testing.T) {
	t.Parallel()
	one, two := ir.Example{Name: "one"}, ir.Example{Name: "two"}
	unnamedA, unnamedB := ir.Example{Summary: "a"}, ir.Example{Summary: "b"}
	withExamples := func(examples ...ir.Example) *ir.Document {
		return &ir.Document{Types: ir.TypeRegistry{"type/x/A": &ir.Model{ID: "type/x/A", Examples: examples}}}
	}
	for _, tc := range []struct {
		name string
		a, b []ir.Example
		same bool
	}{
		{"the other order", []ir.Example{one, two}, []ir.Example{two, one}, true},
		{"a repeat in either order", []ir.Example{one, two, one}, []ir.Example{two, one, one}, true},
		// As an `examples` entry keyed by the empty string is.
		{"an unnamed one among named ones, moved", []ir.Example{unnamedA, one}, []ir.Example{one, unnamedA}, true},
		{"two unnamed, in the other order", []ir.Example{unnamedA, unnamedB}, []ir.Example{unnamedB, unnamedA}, false},
		{"another example", []ir.Example{one, one}, []ir.Example{one, two}, false},
		{"another unnamed one", []ir.Example{unnamedA, unnamedA}, []ir.Example{unnamedA, unnamedB}, false},
		{"a repeat counted once", []ir.Example{one, one, two}, []ir.Example{one, two, two}, false},
		{"one more", []ir.Example{one}, []ir.Example{one, one}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, ok := diffOrderInvariants(withExamples(tc.a...), nil, withExamples(tc.b...), nil)
			assert.Equal(t, tc.same, ok)
			_, ok = diffOrderInvariants(withExamples(tc.b...), nil, withExamples(tc.a...), nil)
			assert.Equal(t, tc.same, ok, "the comparison must be symmetric")
		})
	}
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
			src: "openapi: 3.0.0\ninfo: {title: 0, version: 0}\npaths: {}\n" +
				"components:\n schemas:\n  0:\n   allOf:\n    - {A}\n",
		},
		{
			// Reversed, the alias would precede the anchor it names, which YAML
			// forbids, so the anchor moves to it and the alias takes its place.
			name: "an alias reordered above its anchor",
			src:  "openapi: 3.0.0\ninfo: {title: 0, version: 0}\npaths: {}\n0: &m\n1: *m\n",
		},
		{
			// Each object value's members come back reversed, which Value.Object
			// keeps by design (#683). The two unnamed examples order oppositely
			// once reversed, so sorting them as written would pair them crosswise.
			name: "object values with several members",
			src: "openapi: 3.1.0\ninfo: {title: t, version: v}\npaths: {}\ncomponents:\n  schemas:\n" +
				"    S:\n      type: object\n      properties:\n" +
				"        flags: {type: object, additionalProperties: {type: boolean}, default: {a: true, b: false}}\n" +
				"      examples: [{x: 1, y: 4}, {x: 2, y: 3}]\n",
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
// validation warning about a component name outside the pattern OpenAPI gives.
// Reversing the source moves that component to another line and, inside its
// flow mapping, to another column.
const positionedFindingSpec = "openapi: 3.0.0\ninfo: {title: t, version: v}\npaths: {}\n" +
	"components:\n  schemas: {ok: {type: string}, \"a b\": {type: string}}\n"

// TestOrderInvariant_LineAndColumnCaseMovesItsFinding holds the "a diagnostic
// located by line and column" case to its question. That case would go on
// passing if its fixture stopped drawing the finding or the permutation stopped
// moving it, and the finding is a rule table's, which can change with no change
// here. The line and the column are each asserted because diagnosticSet sets
// each aside.
func TestOrderInvariant_LineAndColumnCaseMovesItsFinding(t *testing.T) {
	t.Parallel()
	positionIn := func(src []byte) ir.Position {
		t.Helper()
		_, diags, err := compile(t.Context(), "positioned.yaml", src, passes())
		require.NoError(t, err)
		require.Len(t, diags, 1, "the fixture must draw exactly one finding")
		return diags[0].Provenance.Position
	}
	baseline, why := reencodeMappings([]byte(positionedFindingSpec))
	require.Empty(t, why, "the fixture must re-encode")
	reversed, why := reverseMappings([]byte(positionedFindingSpec))
	require.Empty(t, why, "the fixture must be permutable")

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
	encodeYAML = func([]*yaml.Node) ([]byte, error) { return nil, errors.New("boom") }

	got, why := reverseMappings([]byte("a: 1\nb: 2\n"))
	assert.Equal(t, DeclineUnencodable, why, "a source that will not re-encode is excluded, not reported")
	assert.Nil(t, got)
}

// TestReencodeMappings_EncodeFailureIsRefused drives the defensive re-encode
// path on the baseline arm, the counterpart of the reverseMappings case above.
func TestReencodeMappings_EncodeFailureIsRefused(t *testing.T) {
	orig := encodeYAML
	t.Cleanup(func() { encodeYAML = orig })
	encodeYAML = func([]*yaml.Node) ([]byte, error) { return nil, errors.New("boom") }

	got, why := reencodeMappings([]byte("a: 1\nb: 2\n"))
	assert.Equal(t, DeclineUnencodable, why, "a source that will not re-encode is excluded, not reported")
	assert.Nil(t, got)
}

// TestReverseMappings_APermutationThatDoesNotParseIsDeclined drives the check
// that holds the rewrite to writing YAML. Moving each anchor ahead of its
// aliases leaves no ordering rule a permutation can break, so the seam writes
// text that does not parse.
func TestReverseMappings_APermutationThatDoesNotParseIsDeclined(t *testing.T) {
	orig := encodeYAML
	t.Cleanup(func() { encodeYAML = orig })
	encodeYAML = func([]*yaml.Node) ([]byte, error) { return []byte("a: [unclosed\n"), nil }

	got, why := reverseMappings([]byte("a: 1\nb: 2\n"))
	assert.Equal(t, DeclineReencodingUnparsable, why, "the rewrite's own fault is no finding about the compiler")
	assert.Nil(t, got)
}

// TestEncodeYAML_AnEncoderFaultIsReturned drives the encoder's own fault: a
// document holding no root, which it refuses to write.
func TestEncodeYAML_AnEncoderFaultIsReturned(t *testing.T) {
	t.Parallel()
	got, err := encodeYAML([]*yaml.Node{{Kind: yaml.DocumentNode}})
	require.Error(t, err, "a document holding no root does not encode")
	assert.Nil(t, got)
}

// TestReencodeMappings_AStreamOfNoDocuments pins a source holding no document,
// only a comment: it re-encodes to nothing, and nothing is there to permute.
func TestReencodeMappings_AStreamOfNoDocuments(t *testing.T) {
	t.Parallel()
	got, why := reencodeMappings([]byte("# nothing but a comment\n"))
	assert.Empty(t, why)
	assert.Empty(t, got)
	assert.Equal(t, declined(OracleOrder, DeclineNothingToPermute),
		orderInvariant(context.Background(), "spec", []byte("# nothing but a comment\n"), true))
}

// TestCheck_OrderDependentOutcome pins that Check classifies an order-dependent
// compiler as such rather than folding it into another oracle. The seam returns
// a different registry for the permuted bytes, which is what a lowering that
// depends on declaration order does.
func TestCheck_OrderDependentOutcome(t *testing.T) {
	const src = "a: 1\nb: 2\n"
	swapCompile(t, func(_ context.Context, _ string, data []byte, _ []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		id := ir.TypeID("type/x/AsWritten")
		if string(data) != src {
			id = "type/x/Reversed"
		}
		path, _ := ir.IDPath(ir.IDKindType, string(id))
		model := &ir.Model{
			ID:         id,
			Name:       ir.Naming{Source: "M", Canonical: "m"},
			Provenance: ir.Provenance{Pointer: jsontext.Pointer("/" + path)},
		}
		sources, services, spaces := tables()
		return &ir.Document{
			IRVersion: ir.IRVersion,
			IDSpaces:  spaces,
			Sources:   sources,
			Services:  services,
			Types:     ir.TypeRegistry{model.ID: model},
		}, nil, nil
	})

	result := Check(context.Background(), "spec", []byte(src))
	assert.Equal(t, OutcomeOrderDependent, result.Outcome, "detail: %s", result.Detail)
	assert.Contains(t, result.Detail, "declaration order")
}

// TestMaxReverseDepth_ReachesEveryAdmittedDocument holds the rewrite's bound
// to the nesting the OpenAPI compiler admits, read where it is declared: a
// document nested exactly that deep is permuted whole, and one level more is
// refused, as the compiler refuses it. Below the bound, the oracle would skip
// documents the compiler lowers.
func TestMaxReverseDepth_ReachesEveryAdmittedDocument(t *testing.T) {
	t.Parallel()
	pkg, err := decls.Load("github.com/dexpace/morphic/compilers/openapi/internal/scan")
	require.NoError(t, err)
	bound, err := pkg.IntConst("MaxNesting")
	require.NoError(t, err)
	deepest := int(bound)
	nested := func(n int) []byte {
		return []byte(strings.Repeat("{k: ", n) + "1" + strings.Repeat("}", n) + "\n")
	}

	_, why := reverseMappings(nested(deepest))
	assert.Empty(t, why, "a document at the compiler's bound is permuted")
	_, why = reverseMappings(nested(deepest + 1))
	assert.Equal(t, DeclineTooDeep, why, "one past it is the compiler's to refuse, and the rewrite's")
}

// TestEncodeComparable_MoreJoinsOrderFree pins the join more goes through: a
// marshaler in it reaches a TypeRef in a Property orderFree sorts, and the
// properties are still sorted. Handed in opts instead, the second
// json.WithMarshalers replaces orderFree, which is why it is a parameter.
func TestEncodeComparable_MoreJoinsOrderFree(t *testing.T) {
	doc := &ir.Document{Types: ir.TypeRegistry{"type/x/A": &ir.Model{ID: "type/x/A", Properties: []ir.Property{
		{ID: "property/x/b", Type: ir.TypeRef{Target: "type/x/old"}}, {ID: "property/x/a"},
	}}}}
	renamed := json.MarshalToFunc(func(enc *jsontext.Encoder, typeRef ir.TypeRef) error {
		if typeRef.Target != "type/x/old" {
			return errors.ErrUnsupported
		}
		return enc.WriteToken(jsontext.String("renamed"))
	})

	joined, err := encodeComparable(doc, sortedArray, renamed)
	require.NoError(t, err)
	assert.Contains(t, string(joined), `"type":"renamed"`, "more reaches an element orderFree writes")
	assert.Less(t, bytes.Index(joined, []byte("property/x/a")), bytes.Index(joined, []byte("property/x/b")), "orderFree still sorts")

	replaced, err := encodeComparable(doc, sortedArray, nil, json.WithMarshalers(renamed))
	require.NoError(t, err)
	assert.Contains(t, string(replaced), `"type":"renamed"`)
	assert.Greater(t, bytes.Index(replaced, []byte("property/x/a")), bytes.Index(replaced, []byte("property/x/b")),
		"a second json.WithMarshalers replaces orderFree")
}

// TestCheck_OrderOraclesAskOfABlockScalarLedByIndentation holds both order
// oracles to asking of a spec whose block scalar starts with a line break or a
// space inside a block sequence item (#2153), where the encoder writes an
// indentation indicator the decoder reads as ending the scalar. The neighbours
// that never tripped it, the scalar outside a sequence item, led by text, or
// written double-quoted, stay asked. So do three the encoder trips on anywhere:
// the info's description folded around a more-indented line, led by a tab, and
// kept and ending blank before a comment. The sequence oracle reverses the
// allOf of the last rows.
func TestCheck_OrderOraclesAskOfABlockScalarLedByIndentation(t *testing.T) {
	const opening = "openapi: 3.0.1\ninfo:\n  title: t\n  version: \"1\"\npaths: {}\n"
	allOf := func(description string) string {
		return opening + "components:\n  schemas:\n    S:\n      allOf:\n        - type: object\n" +
			"          description: " + description + "\n        - title: b\n"
	}
	tests := []struct {
		name     string
		src      string
		sequence Verdict
	}{
		{
			name:     "a tag's description led by a break",
			src:      opening + "tags:\n  - name: x\n    description: |\n      \n      text after a blank first line\n",
			sequence: declined(OracleSequenceOrder, DeclineNothingToPermute),
		},
		{
			name: "the info's description led by a break",
			src: "openapi: 3.0.1\ninfo:\n  title: t\n  version: \"1\"\n" +
				"  description: |\n    \n    text after a blank first line\npaths: {}\n",
			sequence: declined(OracleSequenceOrder, DeclineNothingToPermute),
		},
		{
			name:     "a tag's description led by text",
			src:      opening + "tags:\n  - name: x\n    description: |\n      text\n",
			sequence: declined(OracleSequenceOrder, DeclineNothingToPermute),
		},
		{
			name:     "a tag's description double-quoted",
			src:      opening + "tags:\n  - name: x\n    description: \"\\ntext after a blank first line\\n\"\n",
			sequence: declined(OracleSequenceOrder, DeclineNothingToPermute),
		},
		{
			name:     "an allOf entry's description led by a break",
			src:      allOf("|\n\n            text after a blank first line"),
			sequence: passed(OracleSequenceOrder),
		},
		{
			name:     "an allOf entry's description folded and led by spaces",
			src:      allOf(">2\n             text led by a space"),
			sequence: passed(OracleSequenceOrder),
		},
		{
			name: "the info's description folded around an indented line",
			src: "openapi: 3.0.1\ninfo:\n  title: t\n  version: \"1\"\n  description: >\n    Call it like this:\n\n" +
				"      curl https://example.com\n\n    and read the reply.\npaths: {}\n" +
				"components:\n  schemas:\n    S:\n      allOf:\n        - type: object\n        - title: b\n",
			sequence: passed(OracleSequenceOrder),
		},
		{
			name: "the info's description led by a tab",
			src: "openapi: 3.0.1\ninfo:\n  title: t\n  version: \"1\"\n  description: |2\n    \ttext led by a tab\npaths: {}\n" +
				"components:\n  schemas:\n    S:\n      allOf:\n        - type: object\n        - title: b\n",
			sequence: passed(OracleSequenceOrder),
		},
		{
			name: "the info's description kept and ending blank before a comment",
			src: "openapi: 3.0.1\npaths: {}\ncomponents:\n  schemas:\n    S:\n      allOf:\n        - type: object\n        - title: b\n" +
				"info:\n  title: t\n  version: \"1\"\n  description: |+\n    text and a blank line\n\n# the end\n",
			sequence: passed(OracleSequenceOrder),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Check(context.Background(), "spec.yaml", []byte(tc.src))
			require.Equal(t, OutcomeOK, result.Outcome, "detail: %s", result.Detail)

			verdicts := map[Oracle]Verdict{}
			for _, verdict := range result.Verdicts {
				verdicts[verdict.Oracle] = verdict
			}
			assert.Equal(t, passed(OracleOrder), verdicts[OracleOrder])
			assert.Equal(t, tc.sequence, verdicts[OracleSequenceOrder])
		})
	}
}

// inSequenceItem is a stream of one document holding scalar as a value of the
// mapping a block sequence item opens, a place the encoder misplaces an
// indentation indicator at.
func inSequenceItem(scalar *yaml.Node) []*yaml.Node {
	item := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{scalarNode("k"), scalar}}
	sequence := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{item}}
	return []*yaml.Node{{Kind: yaml.DocumentNode, Content: []*yaml.Node{sequence}}}
}

// leaders are a space and every YAML line break, each leading a value. The
// encoder writes a block scalar's indentation indicator for the ones indicated
// lists, and writes the others double-quoted itself.
var (
	leaders   = []string{" ", "\r", "\n", "\u0085", "\u2028", "\u2029"}
	indicated = []string{" ", "\n", "\u2028", "\u2029"}
)

// scalarValues is every scalar's value in docs, in the order the encoder
// writes them, read independently of the walk under test.
func scalarValues(t *testing.T, docs []*yaml.Node) []string {
	t.Helper()
	var values []string
	var visit func(node *yaml.Node)
	visit = func(node *yaml.Node) {
		if node.Kind == yaml.ScalarNode {
			values = append(values, node.Value)
		}
		for _, child := range node.Content {
			visit(child)
		}
	}
	for _, doc := range docs {
		visit(doc)
	}
	return values
}

// TestEncodeYAML_MisplacesABlockScalarLedByIndentation is the record of an
// encoder behaviour restyleBlockScalars double-quotes for: a literal or
// folded scalar led by a space or a line break, as a value of the mapping a
// block sequence item opens, encodes to text that does not parse. Should the
// encoder stop doing so, this goes red and a space can leave ledByIndentation,
// and the line breaks can once
// TestEncodeYAML_AddsABreakBeforeAFootCommentAfterAKeptScalar is red too.
func TestEncodeYAML_MisplacesABlockScalarLedByIndentation(t *testing.T) {
	t.Parallel()
	for _, style := range []yaml.Style{yaml.LiteralStyle, yaml.FoldedStyle} {
		for _, leader := range indicated {
			t.Run(fmt.Sprintf("%v %q", style, leader), func(t *testing.T) {
				t.Parallel()
				out, err := encodeYAML(inSequenceItem(&yaml.Node{Kind: yaml.ScalarNode, Style: style, Value: leader + "x\n"}))
				require.NoError(t, err)

				_, why := parseStream(out)
				assert.Equal(t, DeclineNotYAML, why, "the encoder wrote %q", out)
			})
		}
	}
}

// TestEncodeYAML_OmitsTheIndicatorOfABlockScalarLedByATab is the record of an
// encoder behaviour restyleBlockScalars double-quotes for: a block scalar led
// by a tab is written with no indentation indicator, at a root mapping's value
// as in an item, and that text does not parse. Should the encoder stop doing
// so, this goes red and a tab can leave ledByIndentation.
func TestEncodeYAML_OmitsTheIndicatorOfABlockScalarLedByATab(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"a root mapping's value":  "a: |2\n  \tx\n",
		"an item's mapping value": "- k: |2\n    \tx\n",
		"an item, folded":         "- >2\n  \tx\n  y\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			docs, why := parseStream([]byte(src))
			require.Empty(t, why)
			out, err := encodeYAML(docs)
			require.NoError(t, err)

			_, why = parseStream(out)
			assert.Equal(t, DeclineNotYAML, why, "%q encodes to %q", src, out)
		})
	}
}

// TestEncodeYAML_AddsABreakBeforeAMoreIndentedFoldedLine is the record of an
// encoder behaviour restyleBlockScalars writes a folded scalar literal for: a
// folded scalar holding a more-indented, tab-led or whitespace-only line
// encodes with a line break more before that line, so its value reads back
// changed. Should the encoder stop doing so, this goes red; a folded scalar
// can keep its style once
// TestEncodeYAML_AddsABreakAfterAKeptFoldedScalarsBlankLines is red too and
// the double-quoting in restyleBlockScalars tests a folded scalar as it does a
// literal one.
func TestEncodeYAML_AddsABreakBeforeAMoreIndentedFoldedLine(t *testing.T) {
	t.Parallel()
	assertEachReadsBackChanged(t, map[string]string{
		"a more-indented line":   "a: >\n  x\n    x\n",
		"a tab-led line":         "a: >\n  x\n  \tx\n",
		"a whitespace-only line": "a: >\n  x\n   \t\n  x\n",
	})
}

// TestEncodeYAML_AddsABreakAfterAKeptFoldedScalarsBlankLines is the record of
// an encoder behaviour restyleBlockScalars writes a folded scalar literal for:
// a kept folded scalar ending in a blank line encodes with a line break more at
// its end. Should the encoder stop doing so, this goes red.
func TestEncodeYAML_AddsABreakAfterAKeptFoldedScalarsBlankLines(t *testing.T) {
	t.Parallel()
	assertEachReadsBackChanged(t, map[string]string{
		"one blank line":  "a: >+\n  x\n\n",
		"two blank lines": "a: >+\n  x\n  y\n\n\n",
	})
}

// TestEncodeYAML_AddsABreakBeforeAFootCommentAfterAKeptScalar is the record of
// the encoder behaviour restyleBlockScalars double-quotes a literal scalar
// ending in a blank line for: written kept as the last node of a document a
// comment ends, it encodes with a line break more before the comment. Should
// the encoder stop doing so, this goes red and endsInTwoLineBreaks can go.
func TestEncodeYAML_AddsABreakBeforeAFootCommentAfterAKeptScalar(t *testing.T) {
	t.Parallel()
	assertEachReadsBackChanged(t, map[string]string{
		"a root mapping's value":    "a: |+\n  x\n\n# c\n",
		"an item":                   "- |+\n  x\n\n# c\n",
		"a nested mapping's value":  "a:\n  b: |+\n    x\n\n  # c\n",
		"before another document":   "a: |+\n  x\n\n# c\n---\nb: 1\n",
		"a value of one line break": "a: |+\n\n# c\n",
	})
}

// assertEachReadsBackChanged holds each named source to an encoding that
// parses and reads back with another scalar value than the source holds.
func assertEachReadsBackChanged(t *testing.T, sources map[string]string) {
	t.Helper()
	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			docs, why := parseStream([]byte(src))
			require.Empty(t, why)
			want := scalarValues(t, docs)
			out, err := encodeYAML(docs)
			require.NoError(t, err)

			read, why := parseStream(out)
			require.Empty(t, why)
			assert.NotEqual(t, want, scalarValues(t, read), "%q encodes to %q", src, out)
		})
	}
}

// TestEncodeYAML_DropsAnEmptyFirstDocument is the record of the encoder
// behaviour tagEmptyFirstDocument exists for: a stream whose first document is
// empty encodes to text that reads back without it, while an empty document
// after the first is kept. Should the encoder keep the first, this goes red and
// the tagging can go.
func TestEncodeYAML_DropsAnEmptyFirstDocument(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]int{"---\n---\na: 1\n": 1, "---\n": 0, "a: 1\n---\n---\nb: 2\n": 3} {
		docs, why := parseStream([]byte(src))
		require.Empty(t, why)
		out, err := encodeYAML(docs)
		require.NoError(t, err)

		read, why := parseStream(out)
		require.Empty(t, why)
		assert.Len(t, read, want, "%q encodes to %q", src, out)
	}
}

// TestEncodeFaithfully_KeepsAnEmptyFirstDocument holds a stream whose first
// document is empty to reading back with it, and a stream whose first
// document is not to the encoding it had.
func TestEncodeFaithfully_KeepsAnEmptyFirstDocument(t *testing.T) {
	t.Parallel()
	tests := []struct{ src, want string }{
		{src: "---\n---\na: 1\n", want: "!!null\n---\na: 1\n"},
		{src: "---\n", want: "!!null\n"},
		{src: "--- ''\n---\na: 1\n", want: "''\n---\na: 1\n"},
		{src: "--- ~\n---\na: 1\n", want: "~\n---\na: 1\n"},
		{src: "a: 1\n---\n---\nb: 2\n", want: "a: 1\n---\n\n---\nb: 2\n"},
	}
	for _, tc := range tests {
		docs, why := parseStream([]byte(tc.src))
		require.Empty(t, why)

		out, why := encodeFaithfully(docs)
		require.Empty(t, why, "%q", tc.src)
		assert.Equal(t, tc.want, string(out), "%q", tc.src)
	}
	assert.NotPanics(t, func() { tagEmptyFirstDocument([]*yaml.Node{{Kind: yaml.DocumentNode}}) },
		"a document holding no root is the encoder's to refuse")
}

// TestEncodeFaithfully_WritesABlockScalarLedByIndentationBack holds each place
// and spelling of a block scalar led by a space, a tab or a line break to
// reading back with every scalar's value as written: written by the decoder,
// and built with each leader the decoder cannot be made to write.
func TestEncodeFaithfully_WritesABlockScalarLedByIndentationBack(t *testing.T) {
	t.Parallel()
	parsed := func(src string) func(t *testing.T) []*yaml.Node {
		return func(t *testing.T) []*yaml.Node {
			t.Helper()
			docs, why := parseStream([]byte(src))
			require.Empty(t, why)
			return docs
		}
	}
	tests := map[string]func(t *testing.T) []*yaml.Node{
		"a mapping's value in an item":        parsed("a:\n  - k: |\n      \n      x\n"),
		"a mapping's value led by spaces":     parsed("a:\n  - k: |2\n       x\n"),
		"folded and led by a break":           parsed("a:\n  - k: >\n      \n      x\n"),
		"folded and led by spaces":            parsed("a:\n  - k: >2\n       x\n"),
		"kept":                                parsed("a:\n  - k: |+\n      \n      x\n\n"),
		"stripped":                            parsed("a:\n  - k: |-\n      \n      x\n"),
		"an item itself":                      parsed("a:\n  - |\n    \n    x\n"),
		"a key in an item":                    parsed("a:\n  - ? |\n      \n      x\n    : v\n"),
		"in an item of a root sequence":       parsed("- k: |\n    \n    x\n"),
		"tagged":                              parsed("a:\n  - k: !t |\n      \n      x\n"),
		"anchored and aliased":                parsed("a:\n  - k: &n |\n      \n      x\n  - j: *n\n"),
		"a long value, which the quote folds": parsed("a:\n  - k: |\n      \n      " + strings.Repeat("word ", 40) + "\n"),
		"a root mapping's value led by a tab": parsed("a: |2\n  \tx\n"),
		"a kept line break before a comment":  parsed("a: |+\n\n# c\n"),
	}
	for _, leader := range leaders {
		tests[fmt.Sprintf("built, led by %q", leader)] = func(*testing.T) []*yaml.Node {
			return inSequenceItem(&yaml.Node{Kind: yaml.ScalarNode, Style: yaml.LiteralStyle, Value: leader + "x\n"})
		}
	}
	for name, docs := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			written := docs(t)
			want := scalarValues(t, written)

			out, why := encodeFaithfully(written)
			require.Empty(t, why, "the encoding of %q", out)

			read, why := parseStream(out)
			require.Empty(t, why)
			assert.Equal(t, want, scalarValues(t, read))
		})
	}
}

// TestEncodeFaithfully_WritesAScalarEndingInTwoLineBreaksBack holds each place
// and spelling of a block scalar ending in two line breaks, folded or the last
// node of a document a comment ends, to reading back with every scalar's value
// as written: written by the decoder, and built with each line break the
// encoder writes kept.
func TestEncodeFaithfully_WritesAScalarEndingInTwoLineBreaksBack(t *testing.T) {
	t.Parallel()
	sources := []string{
		"a: >+\n  x\n    x\n\n",
		"a: >+\n  x\n\n",
		"a: |+\n  x\n\n# c\n",
		"- |+\n  x\n\n# c\n",
		"a:\n  b: |+\n    x\n\n  # c\n",
		"a: |+\n  x\n\n# c\n---\nb: 1\n",
		"a: >+\n  x\n\n# c\n",
		"a: !t |+\n  x\n\n# c\n",
		"a: |+\n  x\n\n\n# c\n",
	}
	tests := map[string]func(t *testing.T) []*yaml.Node{}
	for _, src := range sources {
		tests[fmt.Sprintf("parsed %q", src)] = func(t *testing.T) []*yaml.Node {
			t.Helper()
			docs, why := parseStream([]byte(src))
			require.Empty(t, why)
			return docs
		}
	}
	for _, value := range []string{"x\u2028\n", "x\n\u2029", "x\u2029\u2028"} {
		tests[fmt.Sprintf("built, ending %q", value)] = func(*testing.T) []*yaml.Node {
			mapping := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalarNode("a"), {Kind: yaml.ScalarNode, Style: yaml.LiteralStyle, Value: value},
			}}
			return []*yaml.Node{{Kind: yaml.DocumentNode, FootComment: "# c", Content: []*yaml.Node{mapping}}}
		}
	}
	for name, docs := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			written := docs(t)
			want := scalarValues(t, written)

			out, why := encodeFaithfully(written)
			require.Empty(t, why, "the encoding of %q", out)

			read, why := parseStream(out)
			require.Empty(t, why)
			assert.Equal(t, want, scalarValues(t, read))
		})
	}
}

// TestEncodeFaithfully_KeepsTheTagOfAScalarItQuotes pins that a block scalar
// led by indentation keeps its tag once double-quoted: a tag a string's value
// does not resolve to is written, and a string's reads back alike unwritten.
func TestEncodeFaithfully_KeepsTheTagOfAScalarItQuotes(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{"!t", "!!binary", "!!str"} {
		docs, why := parseStream([]byte("- k: " + tag + " |\n    \n    x\n"))
		require.Empty(t, why)
		want := docs[0].Content[0].Content[0].Content[1].Tag

		out, why := encodeFaithfully(docs)
		require.Empty(t, why)

		read, why := parseStream(out)
		require.Empty(t, why)
		assert.Equal(t, want, read[0].Content[0].Content[0].Content[1].Tag, "%s is written %q", tag, out)
	}
}

// TestEncodeFaithfully_LeavesEveryOtherScalarAsWritten pins that the restyle
// reaches no scalar the encoder writes back in its own style: a quoted one led
// by a space, a literal scalar led by text and ending in one line break or in
// text, and a literal scalar led by a line break the encoder quotes itself. A
// folded scalar is written literal
// (TestEncodeFaithfully_WritesAFoldedScalarLiteral).
func TestEncodeFaithfully_LeavesEveryOtherScalarAsWritten(t *testing.T) {
	t.Parallel()
	styles := func(docs []*yaml.Node) []yaml.Style {
		item := docs[0].Content[0].Content[0]
		var out []yaml.Style
		for i := 1; i < len(item.Content); i += 2 {
			out = append(out, item.Content[i].Style)
		}
		return out
	}
	docs, why := parseStream([]byte("- k: ' x'\n  j: \" x\"\n  l: |\n    text\n  m: |-\n    text\n    x\n"))
	require.Empty(t, why)
	item := docs[0].Content[0].Content[0]
	item.Content = append(item.Content, scalarNode("r"), &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.LiteralStyle, Value: "\rx\n"})
	want := styles(docs)

	out, why := encodeFaithfully(docs)
	require.Empty(t, why)

	read, why := parseStream(out)
	require.Empty(t, why)
	assert.Equal(t, want, styles(docs), "the restyle reaches no scalar of these")
	assert.Equal(t, []yaml.Style{yaml.SingleQuotedStyle, yaml.DoubleQuotedStyle, yaml.LiteralStyle, yaml.LiteralStyle, yaml.DoubleQuotedStyle},
		styles(read), "each is written in its own style but the last, which the encoder quotes itself")
}

// TestEncodeFaithfully_WritesAFoldedScalarLiteral holds each folded scalar,
// plain or holding a line the encoder writes a break more beside, clipped,
// stripped and tagged, to reading back with every scalar's value as written,
// literal, and with its tag, written where it was. A kept one ending in two
// line breaks is double-quoted
// (TestEncodeFaithfully_WritesAScalarEndingInTwoLineBreaksBack).
func TestEncodeFaithfully_WritesAFoldedScalarLiteral(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"plain text":             "a: >\n  x\n  x\n",
		"a more-indented line":   "a: >\n  x\n    x\n",
		"a tab-led line":         "a: >\n  x\n  \tx\n",
		"a whitespace-only line": "a: >\n  x\n   \t\n  x\n",
		"stripped":               "a: >-\n  x\n    x\n",
		"tagged":                 "a: !t >\n  x\n    x\n",
		"tagged a string":        "a: !!str >\n  x\n    x\n",
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			docs, why := parseStream([]byte(src))
			require.Empty(t, why)
			want := scalarValues(t, docs)
			written := docs[0].Content[0].Content[1]
			wantStyle, wantTag := written.Style&^yaml.FoldedStyle|yaml.LiteralStyle, written.Tag

			out, why := encodeFaithfully(docs)
			require.Empty(t, why, "the encoding of %q", out)

			read, why := parseStream(out)
			require.Empty(t, why)
			assert.Equal(t, want, scalarValues(t, read), "%q encodes to %q", src, out)
			value := read[0].Content[0].Content[1]
			assert.Equal(t, wantStyle, value.Style, "%q encodes to %q", src, out)
			assert.Equal(t, wantTag, value.Tag, "%q encodes to %q", src, out)
		})
	}
}

// TestEncodeFaithfully_AnEncodingThatReadsOtherwiseIsDeclined drives the check
// holding a re-encoding to the values it was written from. The seam writes
// text that parses but reads otherwise, as no encoding of a parsed tree is
// known to, differing from the source in one of the ways the check compares
// alone.
func TestEncodeFaithfully_AnEncodingThatReadsOtherwiseIsDeclined(t *testing.T) {
	tests := []struct {
		name         string
		src, encoded string
	}{
		{name: "another value", src: "a: 1\nb: 2\n", encoded: "a: 1\nb: 3\n"},
		{name: "another kind", src: "a: ''\n", encoded: "a: {}\n"},
		{name: "more entries", src: "a: 1\n", encoded: "a: 1\nb: 2\n"},
		{name: "more documents", src: "a: 1\n", encoded: "a: 1\n---\nb: 2\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orig := encodeYAML
			t.Cleanup(func() { encodeYAML = orig })
			encodeYAML = func([]*yaml.Node) ([]byte, error) { return []byte(tc.encoded), nil }

			got, why := reencodeMappings([]byte(tc.src))
			assert.Equal(t, DeclineReencodingReadsOtherwise, why)
			assert.Nil(t, got)
		})
	}
}

// TestReverseMappings_APermutationThatReadsOtherwiseIsDeclined and its
// counterpart for sequences pin that each rewrite holds its permutation to the
// values it was written from.
func TestReverseMappings_APermutationThatReadsOtherwiseIsDeclined(t *testing.T) {
	orig := encodeYAML
	t.Cleanup(func() { encodeYAML = orig })
	encodeYAML = func([]*yaml.Node) ([]byte, error) { return []byte("b: 2\na: 3\n"), nil }

	got, why := reverseMappings([]byte("a: 1\nb: 2\n"))
	assert.Equal(t, DeclineReencodingReadsOtherwise, why)
	assert.Nil(t, got)
}

// TestEncodeFaithfully_DepthBound covers the bound of both walks
// encodeFaithfully makes. A tree reaching the comparison has passed the
// restyle's bound, so the comparison's is driven directly.
func TestEncodeFaithfully_DepthBound(t *testing.T) {
	t.Parallel()
	deep := yamlMapping()
	assert.Equal(t, DeclineTooDeep, restyleBlockScalars(&deep, maxReverseDepth+1))
	assert.Equal(t, DeclineTooDeep, readsBackAsWritten(&deep, &deep, maxReverseDepth+1))

	nested := &yaml.Node{Kind: yaml.ScalarNode, Value: "v"}
	for range maxReverseDepth + 1 {
		nested = &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{nested}}
	}
	_, why := encodeFaithfully([]*yaml.Node{{Kind: yaml.DocumentNode, Content: []*yaml.Node{nested}}})
	assert.Equal(t, DeclineTooDeep, why, "a tree past the bound is declined before it is encoded")
}
