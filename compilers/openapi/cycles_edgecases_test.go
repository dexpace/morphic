package openapi

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// yscalar, ymap, yseq, and yalias build bare yaml.Node values for the whitebox
// tests below, which exercise the cycle detector's helpers on shapes the parser
// never produces from a real document.
func yscalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: v}
}

func ymap(pairs ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Content: pairs}
}

func yseq(items ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Content: items}
}

func yalias(target *yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.AliasNode, Alias: target}
}

// TestRecoverCycleScan_PanicYieldsNoDiags pins the bounded-recursion backstop: a
// scan that panics must degrade to an empty result, never propagate the panic.
func TestRecoverCycleScan_PanicYieldsNoDiags(t *testing.T) {
	t.Parallel()
	got := recoverCycleScan(func() []ir.Diagnostic {
		panic("detector bug")
	})
	assert.Nil(t, got, "a panicking scan degrades to no diagnostics")
}

// TestRecoverCycleScan_PassesThroughResult pins that a scan that does not panic
// returns its diagnostics unchanged.
func TestRecoverCycleScan_PassesThroughResult(t *testing.T) {
	t.Parallel()
	want := []ir.Diagnostic{{Code: codeCyclicRef, Severity: ir.SeverityError}}
	got := recoverCycleScan(func() []ir.Diagnostic {
		return want
	})
	assert.Equal(t, want, got)
}

// TestDocumentRoot_Cases covers every branch of documentRoot: a nil node and an
// empty document node select nothing, a document node unwraps to its content,
// and any other node is returned as-is.
func TestDocumentRoot_Cases(t *testing.T) {
	t.Parallel()
	content := yscalar("x")
	tests := []struct {
		name string
		in   *yaml.Node
		want *yaml.Node
	}{
		{"nil", nil, nil},
		{"empty document", &yaml.Node{Kind: yaml.DocumentNode}, nil},
		{"document unwraps to content", &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{content}}, content},
		{"plain node returned as-is", content, content},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := documentRoot(tc.in)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Same(t, tc.want, got)
		})
	}
}

// TestDetectCycles_WhitespaceOnlyIsNoCycle pins that a comment/whitespace-only
// source decodes to an empty (non-document) root and reports no cycle: this is
// the real-input path through documentRoot's non-document-node branch.
func TestDetectCycles_WhitespaceOnlyIsNoCycle(t *testing.T) {
	t.Parallel()
	assert.Empty(t, detectCycles(0, []byte("\n\n\n")))
	assert.Empty(t, detectCycles(0, []byte("# only a comment\n")))
}

// TestWalkAnchors_NilNode pins that a nil node terminates the anchor walk without
// a diagnostic (the depth-cap arm of the same guard is unreachable in practice).
func TestWalkAnchors_NilNode(t *testing.T) {
	t.Parallel()
	_, ok := walkAnchors(0, nil, map[*yaml.Node]bool{}, 0)
	assert.False(t, ok)
}

// TestDetectCycles_LegalAliasReuseClean pins that an alias to a sibling anchor
// (not an ancestor) is legal YAML reuse, not a recursive anchor: the walk reaches
// the alias branch and returns clean rather than flagging a cycle.
func TestDetectCycles_LegalAliasReuseClean(t *testing.T) {
	t.Parallel()
	src := "a: &x {p: 1}\nb: *x\n"
	assert.Empty(t, detectCycles(0, []byte(src)),
		"an alias to a non-ancestor anchor is legal reuse")
}

// TestAnchorName_Cases covers both arms: the anchor label when the alias resolves
// to an anchored node, and the fallback to the alias value otherwise.
func TestAnchorName_Cases(t *testing.T) {
	t.Parallel()
	anchored := &yaml.Node{Kind: yaml.MappingNode, Anchor: "root"}
	assert.Equal(t, "root", anchorName(yalias(anchored)))
	assert.Equal(t, "bare", anchorName(&yaml.Node{Kind: yaml.AliasNode, Value: "bare"}))
}

// TestWalkOutsideSchema_NilNode pins that a nil node terminates the outside-schema
// walk without collecting refs.
func TestWalkOutsideSchema_NilNode(t *testing.T) {
	t.Parallel()
	var out []*yaml.Node
	walkOutsideSchema(nil, &out, 0)
	assert.Empty(t, out)
}

// TestDetectCycles_MalformedSchemaShapes pins that a schema-entry map whose value
// is a sequence, and a sub-schema list key whose value is a mapping, are ignored
// by the walk (wrong node kind) rather than mistaken for schema references.
func TestDetectCycles_MalformedSchemaShapes(t *testing.T) {
	t.Parallel()
	schemasNotMap := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n" +
		"components:\n  schemas: [1, 2]\n"
	allOfNotSeq := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n" +
		"components:\n  schemas:\n    A:\n      allOf: {x: 1}\n"
	assert.Empty(t, detectCycles(0, []byte(schemasNotMap)), "schemas as a sequence is not a schema map")
	assert.Empty(t, detectCycles(0, []byte(allOfNotSeq)), "allOf as a mapping is not a schema list")
}

// TestFollowRefChain_DepthCapReturnsFalse pins the loop bound: a ref chain longer
// than maxCycleDepth that never revisits a node exits at the cap and reports no
// cycle rather than looping unbounded.
func TestFollowRefChain_DepthCapReturnsFalse(t *testing.T) {
	t.Parallel()
	const n = maxCycleDepth + 2
	schemas := &yaml.Node{Kind: yaml.MappingNode}
	root := ymap(yscalar("schemas"), schemas)
	nodes := make([]*yaml.Node, n)
	for i := range nodes {
		nodes[i] = &yaml.Node{Kind: yaml.MappingNode}
	}
	for i := range nodes {
		schemas.Content = append(schemas.Content, yscalar(strconv.Itoa(i)), nodes[i])
		if i < n-1 {
			nodes[i].Content = []*yaml.Node{yscalar("$ref"), yscalar("#/schemas/" + strconv.Itoa(i+1))}
		}
	}
	assert.False(t, followRefChain(root, nodes[0]),
		"a chain longer than the depth cap exits without flagging a cycle")
}

// TestChildByToken_NilNode pins that a nil node has no child for any token.
func TestChildByToken_NilNode(t *testing.T) {
	t.Parallel()
	assert.Nil(t, childByToken(nil, "anything"))
}

// TestResolvePointer_Cases covers the pointer-navigation branches the schema
// corpus does not reach: sequence indexing (valid, out of range, non-numeric),
// alias dereferencing along the path, and RFC 6901 escaped tokens (~0 -> ~,
// ~1 -> /).
func TestResolvePointer_Cases(t *testing.T) {
	t.Parallel()
	leaf := yscalar("leaf")
	target := ymap(yscalar("b"), leaf)
	root := ymap(
		yscalar("arr"), yseq(yscalar("zero"), yscalar("one")),
		yscalar("via"), yalias(target),
		yscalar("a/b"), yscalar("slash"),
		yscalar("c~d"), yscalar("tilde"),
	)
	tests := []struct {
		name string
		ref  string
		want *yaml.Node
	}{
		{"sequence index in range", "#/arr/1", root.Content[1].Content[1]},
		{"sequence index out of range", "#/arr/9", nil},
		{"sequence index non-numeric", "#/arr/x", nil},
		{"alias dereferenced along path", "#/via/b", leaf},
		{"escaped slash token", "#/a~1b", root.Content[5]},
		{"escaped tilde token", "#/c~0d", root.Content[7]},
		{"missing key", "#/nope", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolvePointer(root, tc.ref)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Same(t, tc.want, got)
		})
	}
}

// TestDeref_FollowsAliasChain pins that deref walks an alias to its anchored
// target and returns a non-alias node unchanged.
func TestDeref_FollowsAliasChain(t *testing.T) {
	t.Parallel()
	target := ymap(yscalar("k"), yscalar("v"))
	require.Same(t, target, deref(yalias(target)))
	require.Same(t, target, deref(target))
}
