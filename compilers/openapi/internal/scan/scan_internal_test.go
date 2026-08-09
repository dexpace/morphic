package scan

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

var cycleReproducers = []struct{ name, file string }{
	{"self-ref", "cycle_self_ref"},
	{"two-node-ref", "cycle_two_node_ref"},
	{"yaml-anchor", "cycle_yaml_anchor"},
	{"self-ref-sibling", "cycle_self_ref_sibling"},
	{"two-node-ref-sibling", "cycle_two_node_ref_sibling"},
	{"alias-ref-value", "cycle_alias_ref_value"},
	{"content-schema", "cycle_content_schema"},
	{"alias-ref-key", "cycle_alias_ref_key"},
	{"merge-key-ref", "cycle_merge_key_ref"},
	{"alias-schema-node", "cycle_alias_schema_node"},
	{"alias-dual-position", "cycle_alias_dual_position"},
	{"duplicate-key", "cycle_duplicate_key"},
	// Reference-object cycles spelled by document position rather than through
	// components. speakeasy guards the components spelling and faults on these.
	{"path-item-mutual", "cycle_path_item_mutual"},
	{"path-item-self", "cycle_path_item_self"},
	{"webhook-mutual", "cycle_webhook_mutual"},
	{"response-via-path", "cycle_response_via_path"},
	{"path-item-via-component", "cycle_path_item_via_component"},
	// A reference whose pointer passes through a reference already being
	// resolved. Distinct from the cycles above: the hop never completes, so
	// speakeasy's own guard cannot see it and it deadlocks rather than faulting.
	{"path-item-prefix-self", "cycle_path_item_prefix_self"},
	{"path-item-prefix-sibling", "cycle_path_item_prefix_sibling"},
	{"path-item-prefix-chain", "cycle_path_item_prefix_chain"},
	{"component-path-item-prefix", "cycle_component_path_item_prefix"},
	{"webhook-prefix-self", "cycle_webhook_prefix_self"},
	// A self-reference only the resolver's pointer normalization reveals, which
	// overflows the stack rather than deadlocking: the resolution cache ends up
	// pointing at its own reference and GetObject's delegation recurses.
	{"pointer-whitespace-self", "cycle_pointer_whitespace_self"},
}

func TestDetectCycles_Reproducers(t *testing.T) {
	t.Parallel()
	for _, tc := range cycleReproducers {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := readReproducer(t, tc.file)
			diags := scanBytes(t, data)
			require.NotEmpty(t, diags, "degenerate cycle must be diagnosed")
			assert.Equal(t, diag.CyclicRef, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.NotEmpty(t, diags[0].Provenance.Pointer, "line:col provenance")
		})
	}
}

func TestDetectCycles_LegalRecursionClean(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../../testdata/conformance/openapi/recursive.yaml")
	require.NoError(t, err)
	assert.Empty(t, scanBytes(t, data), "legal recursion is not a degenerate cycle")
}

var refShapedDataSpecs = []struct {
	name string
	data string
}{
	{"example-data-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A:
      type: object
      example:
        p: {$ref: '#/components/schemas/A/example/q'}
        q: {$ref: '#/components/schemas/A/example/p'}
`},
	{"default-data-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A:
      type: object
      default:
        p: {$ref: '#/components/schemas/A/default/q'}
        q: {$ref: '#/components/schemas/A/default/p'}
`},
	{"extension-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-foo: {a: {$ref: '#/x-foo/b'}, b: {$ref: '#/x-foo/a'}}
`},
	{"responses-ref-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  responses:
    A: {$ref: '#/components/responses/B'}
    B: {$ref: '#/components/responses/A'}
`},
	{"allof-wrapped-cycle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A: {allOf: [{$ref: '#/components/schemas/B'}]}
    B: {allOf: [{$ref: '#/components/schemas/A'}]}
`},
	// The four cases below are the negative controls for GitHub #26: each uses
	// the same alias/merge/contentSchema shapes as the crashing reproducers, but
	// legally (no degenerate cycle), so the alias- and merge-aware scan must not
	// start refusing valid documents it previously accepted.
	{"legal-anchor-reuse", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {s: &s {type: object}}
components:
  schemas:
    A: *s
    B: {type: object, properties: {p: *s}}
`},
	{"legal-merge-key", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {base: &base {description: base schema}}
components:
  schemas:
    A: {<<: *base, type: object}
`},
	{"legal-content-schema", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A: {type: string, contentMediaType: application/json, contentSchema: {type: object}}
`},
	{"legal-alias-ref-terminates", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {ref: &r '#/components/schemas/B'}
components:
  schemas:
    A: {$ref: *r}
    B: {type: object}
`},
	// The cases below are the negative controls for the re-entrant-prefix
	// refusal, and they are what keeps it from widening into an over-refusal.
	// Each carries a pointer whose prefix names a reference — the shape the rule
	// keys on — without being the shape that hangs:
	//
	//   - the first two pass through a reference that is not on the chain
	//     reading it, so no lock is re-entered;
	//   - the last two re-enter from a schema position, which resolves through
	//     jsonschema rather than the openapi Reference wrapper and so never takes
	//     the lock at all.
	//
	// Each was measured against speakeasy v1.24.0 before being written down:
	// refusing any of them would be refusing a document that compiles today.
	{"legal-prefix-both-hops-dangle", `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a: {$ref: '#/paths/~1b/t'}
  /b: {$ref: '#/paths/~1a/t'}
`},
	{"legal-prefix-through-offchain-ref", `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a: {$ref: '#/paths/~1b/t'}
  /b: {$ref: '#/components/pathItems/C'}
components:
  pathItems:
    C: {get: {operationId: c, responses: {"200": {description: ok}}}}
`},
	{"legal-schema-prefix-self", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  schemas:
    A: {$ref: '#/components/schemas/A/properties', properties: {p: {type: string}}}
`},
	{"legal-schema-chain-reentry", `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a: {$ref: '#/components/schemas/S/t'}
components:
  schemas:
    S: {$ref: '#/paths/~1a'}
`},
}

func TestDetectCycles_RefShapedDataIsClean(t *testing.T) {
	t.Parallel()
	for _, tc := range refShapedDataSpecs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, scanBytes(t, []byte(tc.data)),
				"a ref-shaped structure outside a schema position is not a degenerate cycle")
		})
	}
}

// TestDetectCycles_EmptyDocumentIsNoCycle covers the index a source with nothing
// in it produces. A source that does not decode at all no longer reaches here —
// load refuses it as a parse error before it indexes anything — so the empty
// document is the only sourceless index the scan can be handed.
func TestDetectCycles_EmptyDocumentIsNoCycle(t *testing.T) {
	t.Parallel()
	assert.Empty(t, Cycles(0, sourceindex.Build(nil, sourceindex.MaxIndexedNodes)))
	assert.Empty(t, scanBytes(t, nil))
}

// scanBytes decodes source bytes and runs the refusals over the index built from
// them — what load does around Cycles, with the compile's one decode.
func scanBytes(t *testing.T, data []byte) []ir.Diagnostic {
	t.Helper()
	return Cycles(0, indexOf(t, data))
}

// indexOf decodes source bytes and indexes the tree. A fixture that does not
// decode never reaches the scan in a real compile — load returns a parse error
// first — so a decode failure here is a broken fixture, not a case to scan.
func indexOf(t *testing.T, data []byte) sourceindex.Index {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(data, &root), "the fixture must decode")
	return sourceindex.Build(&root, sourceindex.MaxIndexedNodes)
}

// rawNodes is a tree's own node count, before any alias is substituted, as the
// source index derives it.
func rawNodes(n *yaml.Node) int64 {
	return sourceindex.Build(n, sourceindex.MaxIndexedNodes).Nodes()
}

func readReproducer(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../../testdata/openapi/" + file + ".yaml")
	require.NoError(t, err)
	return data
}

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

func ymerge() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: "<<", Tag: nodeview.MergeTag}
}

func TestRecoverCycleScan_PanicYieldsWarning(t *testing.T) {
	t.Parallel()
	got := recoverCycleScan(3, func() []ir.Diagnostic {
		panic("detector bug")
	})
	require.Len(t, got, 1, "a panicking scan degrades to one diagnostic")
	assert.Equal(t, diag.CycleScanFailed, got[0].Code)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity, "the scan failure must not refuse a spec")
	assert.Equal(t, 3, got[0].Provenance.Source, "the diagnostic carries the source index")
}

func TestRecoverCycleScan_PassesThroughResult(t *testing.T) {
	t.Parallel()
	want := []ir.Diagnostic{{Code: diag.CyclicRef, Severity: ir.SeverityError}}
	got := recoverCycleScan(0, func() []ir.Diagnostic {
		return want
	})
	assert.Equal(t, want, got)
}

func TestDetectCycles_WhitespaceOnlyIsNoCycle(t *testing.T) {
	t.Parallel()
	assert.Empty(t, scanBytes(t, []byte("\n\n\n")))
	assert.Empty(t, scanBytes(t, []byte("# only a comment\n")))
}

func TestDetectCycles_LegalAliasReuseClean(t *testing.T) {
	t.Parallel()
	src := "a: &x {p: 1}\nb: *x\n"
	assert.Empty(t, scanBytes(t, []byte(src)),
		"an alias to a non-ancestor anchor is legal reuse")
}

func TestAnchorName_Cases(t *testing.T) {
	t.Parallel()
	anchored := &yaml.Node{Kind: yaml.MappingNode, Anchor: "root"}
	assert.Equal(t, "root", anchorName(yalias(anchored)))
	assert.Equal(t, "bare", anchorName(&yaml.Node{Kind: yaml.AliasNode, Value: "bare"}))
}

func TestRefScanCollect_NilRoot(t *testing.T) {
	t.Parallel()
	s := newRefScan()
	s.collect(nil)
	assert.Empty(t, s.out)
	assert.Empty(t, s.stack, "the worklist drains to empty")
}

func TestDetectCycles_MalformedSchemaShapes(t *testing.T) {
	t.Parallel()
	schemasNotMap := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n" +
		"components:\n  schemas: [1, 2]\n"
	allOfNotSeq := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n" +
		"components:\n  schemas:\n    A:\n      allOf: {x: 1}\n"
	assert.Empty(t, scanBytes(t, []byte(schemasNotMap)), "schemas as a sequence is not a schema map")
	assert.Empty(t, scanBytes(t, []byte(allOfNotSeq)), "allOf as a mapping is not a schema list")
}

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
	verdict, _ := newRefScan().followRefChain(root, nodes[0])
	assert.Equal(t, chainTerminates, verdict,
		"a chain longer than the depth cap exits without flagging a cycle")
}

func TestFollowRefChain_SafeMemoShortCircuits(t *testing.T) {
	t.Parallel()
	a := ymap(yscalar("$ref"), yscalar("#/schemas/B"))
	b := ymap(yscalar("$ref"), yscalar("#/schemas/A"))
	schemas := ymap(yscalar("A"), a, yscalar("B"), b)
	root := ymap(yscalar("schemas"), schemas)

	verdict, _ := newRefScan().followRefChain(root, a)
	assert.Equal(t, chainCycles, verdict, "A -> B -> A is cyclic with an empty memo")

	s := newRefScan()
	s.safe[b] = true
	memoed, _ := s.followRefChain(root, a)
	assert.Equal(t, chainTerminates, memoed, "a chain reaching a memoized-safe node is not a cycle")
	assert.True(t, s.safe[a], "the walk records the reaching node as terminating too")
}

func TestFollowRefChain_DanglingRefIsNotCycle(t *testing.T) {
	t.Parallel()
	a := ymap(yscalar("$ref"), yscalar("#/schemas/Missing"))
	root := ymap(yscalar("schemas"), ymap(yscalar("A"), a))
	s := newRefScan()
	verdict, _ := s.followRefChain(root, a)
	assert.Equal(t, chainTerminates, verdict, "a dangling $ref is not a cycle")
	assert.True(t, s.safe[a], "the dangling node is recorded terminating")
}

func TestMappingPairs_Cases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    *yaml.Node
		want map[string]string
	}{
		{"nil node yields no pairs", nil, nil},
		{"non-mapping node yields no pairs", yscalar("x"), nil},
		{
			"alias-valued mapping is dereferenced at entry",
			yalias(ymap(yscalar("k"), yscalar("v"))),
			map[string]string{"k": "v"},
		},
		{
			"alias key and alias value are dereferenced",
			ymap(yalias(yscalar("k")), yalias(yscalar("v"))),
			map[string]string{"k": "v"},
		},
		{
			"non-scalar key after nodeview.Deref is skipped",
			ymap(
				ymap(yscalar("x"), yscalar("1")), yscalar("ignored"),
				yscalar("real"), yscalar("kept"),
			),
			map[string]string{"real": "kept"},
		},
		{
			"key aliasing a nil target is skipped",
			ymap(yalias(nil), yscalar("ignored"), yscalar("real"), yscalar("kept")),
			map[string]string{"real": "kept"},
		},
		{
			"odd trailing key without a value is ignored",
			trailingKeyNode(),
			map[string]string{"a": "1"},
		},
		{
			"duplicate explicit Key: last wins",
			ymap(yscalar("k"), yscalar("first"), yscalar("k"), yscalar("second")),
			map[string]string{"k": "second"},
		},
		{
			"a merged key still yields to a repeated explicit key",
			ymap(yscalar("k"), yscalar("first"), ymerge(), ymap(yscalar("k"), yscalar("from-merge")),
				yscalar("k"), yscalar("second")),
			map[string]string{"k": "second"},
		},
		{
			"merge key contributes a mapping's pairs",
			ymap(ymerge(), yalias(ymap(yscalar("a"), yscalar("1"))), yscalar("b"), yscalar("2")),
			map[string]string{"a": "1", "b": "2"},
		},
		{
			"merge value that is not a mapping contributes nothing",
			ymap(ymerge(), yscalar("not-a-mapping"), yscalar("b"), yscalar("2")),
			map[string]string{"b": "2"},
		},
		{
			"explicit key wins over merged key",
			ymap(yscalar("a"), yscalar("explicit"), ymerge(), ymap(yscalar("a"), yscalar("from-merge"))),
			map[string]string{"a": "explicit"},
		},
		{
			"merge sequence: earlier source wins on a shared key",
			ymap(ymerge(), yseq(
				ymap(yscalar("a"), yscalar("from-first")),
				ymap(yscalar("a"), yscalar("from-second"), yscalar("b"), yscalar("only-in-second")),
			)),
			map[string]string{"a": "from-first", "b": "only-in-second"},
		},
		{
			"merge sequence reusing one source keeps its pairs",
			mergeSeqReuseNode(),
			map[string]string{"a": "1"},
		},
		{
			"self-referential merge is bounded, not infinite",
			selfReferentialMergeNode(),
			map[string]string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := nodeview.New().MappingPairs(tc.n)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Equal(t, tc.want, pairMap(got))
		})
	}

	// The remaining two shapes assert something pairMap erases (slice order,
	// and the view's own exhausted bookkeeping across a depth-bound merge
	// chain), so they stay as dedicated subtests rather than table rows.

	t.Run("duplicate explicit key keeps the last occurrence's position", func(t *testing.T) {
		t.Parallel()
		n := ymap(
			yscalar("k"), yscalar("first"),
			yscalar("other"), yscalar("o"),
			yscalar("k"), yscalar("second"),
		)
		got := nodeview.New().MappingPairs(n)
		require.Len(t, got, 2)
		assert.Equal(t, "other", got[0].Key)
		assert.Equal(t, "k", got[1].Key)
		assert.Equal(t, "second", got[1].Val.Value)
	})

	t.Run("merge chain at the depth bound still reaches the leaf", func(t *testing.T) {
		t.Parallel()
		v := nodeview.New()
		got := v.MappingPairs(mergeChain(nodeview.MergeDepthLimit))
		assert.Equal(t, map[string]string{"leaf": "v"}, pairMap(got),
			"a chain exactly at the bound expands in full")
		assert.False(t, v.Exhausted(), "expanding to the bound is not exceeding it")
	})

	t.Run("merge chain past the depth bound stops at the bound", func(t *testing.T) {
		t.Parallel()
		v := nodeview.New()
		assert.Empty(t, v.MappingPairs(mergeChain(nodeview.MergeDepthLimit+2)),
			"a merge chain longer than the bound never reaches the leaf pair")
		assert.True(t, v.Exhausted(), "exceeding the bound is recorded for refCycles")
	})
}

func trailingKeyNode() *yaml.Node {
	n := ymap(yscalar("a"), yscalar("1"))
	n.Content = append(n.Content, yscalar("dangling"))
	return n
}

func mergeSeqReuseNode() *yaml.Node {
	base := ymap(yscalar("a"), yscalar("1"))
	return ymap(ymerge(), yseq(yalias(base), yalias(base)))
}

func selfReferentialMergeNode() *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	n.Content = []*yaml.Node{ymerge(), yalias(n)}
	return n
}

func mergeChain(levels int) *yaml.Node {
	nodes := make([]*yaml.Node, levels+1)
	for i := range nodes {
		nodes[i] = &yaml.Node{Kind: yaml.MappingNode}
	}
	for i := range levels {
		nodes[i].Content = []*yaml.Node{ymerge(), yalias(nodes[i+1])}
	}
	nodes[levels].Content = []*yaml.Node{yscalar("leaf"), yscalar("v")}
	return nodes[0]
}

func pairMap(pairs []nodeview.Pair) map[string]string {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		out[p.Key] = p.Val.Value
	}
	return out
}

func TestDetectCycles_NonMergeKeyShapesAreClean(t *testing.T) {
	t.Parallel()
	quoted := `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {base: &base {$ref: '#/components/schemas/A'}}
components: {schemas: {A: {'<<': *base}}}
`
	aliasedKey := `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {k: &k '<<', base: &base {$ref: '#/components/schemas/A'}}
components: {schemas: {A: {*k : *base}}}
`
	assert.Empty(t, scanBytes(t, []byte(quoted)),
		"a quoted '<<' is a plain key to speakeasy, not a merge")
	assert.Empty(t, scanBytes(t, []byte(aliasedKey)),
		"an alias standing in for the key is a plain key to speakeasy, not a merge")
}

func TestRefScanCollect_VisitsEachNodeOncePerRole(t *testing.T) {
	t.Parallel()
	ref := ymap(yscalar("$ref"), yscalar("#/components/schemas/B"))
	root := ymap(yscalar("schemas"), ymap(
		yscalar("A"), ymap(yscalar("allOf"), yseq(yalias(ref), yalias(ref))),
		yscalar("B"), ymap(yscalar("properties"), yalias(ref)),
		yscalar("C"), yalias(ref),
		// allOf whose value is the mapping itself, not a sequence: the only way
		// this node is entered in the schema-list role.
		yscalar("D"), ymap(yscalar("allOf"), yalias(ref)),
	))

	s := newRefScan()
	s.collect(root)

	assert.Equal(t, []*yaml.Node{ref}, s.out,
		"the shared node is collected once despite repeated schema-role arrivals")
	assert.True(t, s.seen[roleSchema][ref], "entered as a schema")
	assert.True(t, s.seen[roleSchemaMap][ref], "and as a name->schema map")
	assert.True(t, s.seen[roleSchemaList][ref], "and as a schema list, each in its own set")
	assert.False(t, s.seen[roleOutside][ref], "never reached outside a schema")
}

func TestDetectCycles_TruncationDoesNotDisableTheRestOfTheScan(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-anchors:\n")
	b.WriteString("  m0: &m0 {type: object}\n")
	for i := 1; i <= nodeview.MergeDepthLimit+10; i++ {
		fmt.Fprintf(&b, "  m%d: &m%d {<<: *m%d, p%d: %d}\n", i, i, i-1, i, i)
	}
	b.WriteString("components:\n  schemas:\n")
	fmt.Fprintf(&b, "    Deep: {properties: {x: *m%d}}\n", nodeview.MergeDepthLimit+10)
	b.WriteString("    A: {$ref: '#/components/schemas/B'}\n")
	b.WriteString("    B: {$ref: '#/components/schemas/A'}\n")

	diags := scanBytes(t, []byte(b.String()))
	require.NotEmpty(t, diags)
	assert.Equal(t, diag.CyclicRef, diags[0].Code,
		"a cycle outside the truncated chain is still found, and outranks the warning")
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

func TestRefScanCollect_UnhandledRolePanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() {
		s := newRefScan()
		s.stack = append(s.stack, refTask{n: ymap(), role: roleCount})
		s.collect(nil)
	}, "a task carrying an unhandled role is a programmer error")
}

func TestRefScanCollect_DeepNestingIsNotTruncated(t *testing.T) {
	t.Parallel()
	ref := ymap(yscalar("$ref"), yscalar("#/components/schemas/A"))
	deep := ref
	for range maxCycleDepth + 10 {
		deep = ymap(yscalar("items"), deep)
	}
	root := ymap(yscalar("schemas"), ymap(yscalar("A"), deep))

	s := newRefScan()
	s.collect(root)
	assert.Contains(t, s.out, ref, "a ref nested past the old depth cap is still collected")
}

func TestDetectCycles_ChainedAliasFanOutIsRefusedFast(t *testing.T) {
	t.Parallel()
	const levels = 40

	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-anchors:\n")
	b.WriteString("  a0: &a0 {type: string}\n")
	for i := 1; i <= levels; i++ {
		fmt.Fprintf(&b, "  a%d: &a%d {allOf: [*a%d, *a%d]}\n", i, i, i-1, i-1)
	}
	fmt.Fprintf(&b, "components:\n  schemas:\n    Root: *a%d\n", levels)

	diags := scanWithin(t, b.String(), "exponential blowup on chained aliases")
	require.Len(t, diags, 1, "a fan-out this deep is refused, not silently accepted")
	assert.Equal(t, diag.AliasAmplification, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

func mergeChainSpec(levels int) string {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-anchors:\n")
	b.WriteString("  m0: &m0 {type: object}\n")
	for i := 1; i <= levels; i++ {
		fmt.Fprintf(&b, "  m%d: &m%d {<<: *m%d, p%d: %d}\n", i, i, i-1, i, i)
	}
	b.WriteString("components:\n  schemas:\n")
	for i := levels; i >= 0; i-- {
		fmt.Fprintf(&b, "    S%d: {properties: {x: *m%d}}\n", i, i)
	}
	return b.String()
}

func TestDetectCycles_MergeChainWithinBoundIsClean(t *testing.T) {
	t.Parallel()
	diags := scanWithin(t, mergeChainSpec(nodeview.MergeDepthLimit), "blowup on an in-bound merge chain")
	assert.Empty(t, diags, "a merge chain the scan can expand in full is clean")
}

func TestDetectCycles_MergeChainPastBoundStaysFastAndWarns(t *testing.T) {
	t.Parallel()
	diags := scanWithin(t, mergeChainSpec(1600), "super-linear blowup on a long merge chain")
	require.Len(t, diags, 2, "both the truncation warning and the amplification refusal are reported")
	assert.Equal(t, diag.CycleScanFailed, diags[0].Code)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity,
		"incomplete protection is a warning, never a refusal")
	assert.Equal(t, diag.AliasAmplification, diags[1].Code)
	assert.Equal(t, ir.SeverityError, diags[1].Severity,
		"the document's alias expansion is also refused outright")
}

func scanWithin(t *testing.T, src, blowup string) []ir.Diagnostic {
	t.Helper()
	const bound = 10 * time.Second
	// Indexed on this goroutine: the walk is linear in the tree, and require
	// must not be called from the one below.
	idx := indexOf(t, []byte(src))
	done := make(chan []ir.Diagnostic, 1)
	go func() {
		done <- Cycles(0, idx)
	}()
	select {
	case diags := <-done:
		return diags
	case <-time.After(bound):
		t.Fatalf("Cycles did not return within %v — likely %s", bound, blowup)
		return nil
	}
}

// TestRefScanCollect_OutsidePositions covers the two ways the walk moves through
// a document position that is not itself a schema: a sequence, whose elements
// are each still outside, and a `schema` key, which is where schema context
// begins.
//
// Both matter to what the scan can see. A sequence not descended into hides
// every reference under a parameter or server list, and a `schema` key not
// entered as one leaves the schema beneath it read as ordinary data, where a
// $ref is collected under no role at all.
func TestRefScanCollect_OutsidePositions(t *testing.T) {
	t.Parallel()
	inSeq := ymap(yscalar("$ref"), yscalar("#/components/schemas/A"))
	underSchema := ymap(yscalar("$ref"), yscalar("#/components/schemas/B"))
	notASchema := ymap(yscalar("$ref"), yscalar("#/components/schemas/C"))

	root := ymap(
		// a sequence at an outside position: each element stays outside
		yscalar("parameters"), yseq(ymap(yscalar("schema"), underSchema)),
		// a data key: everything beneath it is data, whatever it looks like
		yscalar("example"), notASchema,
		// a mapping at an outside position that is a reference object itself
		yscalar("requestBody"), inSeq,
	)

	s := newRefScan()
	s.collect(root)

	assert.True(t, s.seen[roleSchema][underSchema],
		"a schema key inside a sequence element is entered as a schema")
	assert.True(t, s.seen[roleOutside][inSeq],
		"a reference object outside any schema is still walked")
	assert.False(t, s.seen[roleSchema][notASchema],
		"nothing under a data key is entered as a schema")
	assert.False(t, s.seen[roleOutside][notASchema],
		"nor walked as an outside position")
}

func TestDetectCycles_PointerThroughOwnReferenceIsRefused(t *testing.T) {
	t.Parallel()
	const src = `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /a: {$ref: '#/paths/~1a/t'}
`
	diags := scanBytes(t, []byte(src))
	require.NotEmpty(t, diags, "a pointer that resolves through its own reference must be refused")
	assert.Equal(t, diag.CyclicRef, diags[0].Code)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

// TestDetectCycles_PointerIsNormalizedLikeTheResolver pins the scan's pointer
// reading to speakeasy's. It splits a $ref on '#', trims whitespace from both
// halves and percent-decodes the pointer (references/reference.go GetURI and
// GetJSONPointer, v1.24.0), so '#/paths/~1a ' names /a there. A scan that reads
// the raw value instead calls that pointer dangling and lets a self-reference
// through — one trailing space was enough to walk past the refusal.
func TestDetectCycles_PointerIsNormalizedLikeTheResolver(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n  /a:\n    $ref: "
	const tail = "\n    get: {operationId: a, responses: {\"200\": {description: ok}}}\n"

	refs := map[string]string{
		"trailing space":  "'#/paths/~1a '",
		"leading space":   "' #/paths/~1a'",
		"percent-encoded": "'#/paths/%7E1a'",
	}
	for name, ref := range refs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			diags := scanBytes(t, []byte(head+ref+tail))
			require.NotEmpty(t, diags, "the resolver reads this pointer as naming /a")
			assert.Equal(t, diag.CyclicRef, diags[0].Code)
		})
	}
}

// TestDetectCycles_AcceptsATreeWithNoCycle is the control the refusal cases
// need: without it, a suite made only of refusals would pass on a scan that
// refused everything handed to it.
func TestDetectCycles_AcceptsATreeWithNoCycle(t *testing.T) {
	t.Parallel()
	assert.Empty(t, scanBytes(t, []byte(
		"openapi: 3.1.0\ncomponents: {schemas: {A: {$ref: '#/components/schemas/B'}, B: {type: string}}}\n")))
}
