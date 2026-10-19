package scan

import (
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
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
	// A self-reference only the resolver's pointer normalization reveals.
	{"pointer-whitespace-self", "cycle_pointer_whitespace_self"},
	// A self-reference only a plus read as a plus reveals (GitHub #911).
	{"pointer-plus-self", "cycle_pointer_plus_self"},
	// A loop closed by a pointer passing back through a schema $ref on it
	// (GitHub #932).
	{"schema-through-own-member", "cycle_schema_through_own_member"},
	{"schema-through-member-chain", "cycle_schema_through_member_chain"},
}

// referenceObjectCycles are chains of reference objects outside any schema
// that cycle, or whose pointer passes back through a reference on the chain.
// The resolver follows such a chain in a loop and reports it, so the scan
// leaves every one to it (GitHub #574).
var referenceObjectCycles = []struct{ name, file string }{
	{"path-item-mutual", "cycle_path_item_mutual"},
	{"path-item-self", "cycle_path_item_self"},
	{"webhook-mutual", "cycle_webhook_mutual"},
	{"response-via-path", "cycle_response_via_path"},
	{"path-item-via-component", "cycle_path_item_via_component"},
	{"path-item-prefix-self", "cycle_path_item_prefix_self"},
	{"path-item-prefix-sibling", "cycle_path_item_prefix_sibling"},
	{"path-item-prefix-chain", "cycle_path_item_prefix_chain"},
	{"component-path-item-prefix", "cycle_component_path_item_prefix"},
	{"webhook-prefix-self", "cycle_webhook_prefix_self"},
	{"path-item-empty-segment", "cycle_path_item_empty_segment"},
}

// TestCycleReproducers_EveryFixtureIsExercised holds cycleReproducers and
// referenceObjectCycles to the fixtures on disk. A cycle_*.yaml added to the
// corpus and left out of both is scanned by nothing here, and the suite stays
// green while the corpus grows past it.
//
// The compiler package keeps tables of the same fixtures for its own assertion
// and carries the twin of this test. Holding both to one directory is what stops
// the two from drifting apart as well as from the corpus, which neither package
// can check directly — a test package cannot import another's.
func TestCycleReproducers_EveryFixtureIsExercised(t *testing.T) {
	t.Parallel()
	onDisk, err := filepath.Glob(filepath.Join(reproducerDir, "cycle_*.yaml"))
	require.NoError(t, err, "globbing the reproducer corpus")
	require.NotEmpty(t, onDisk, "the corpus holds cycle reproducers")

	listed := make(map[string]bool, len(cycleReproducers)+len(referenceObjectCycles))
	for _, tc := range slices.Concat(cycleReproducers, referenceObjectCycles) {
		require.False(t, listed[tc.file+".yaml"], "%s is listed twice", tc.file)
		listed[tc.file+".yaml"] = true
	}
	for _, path := range onDisk {
		assert.True(t, listed[filepath.Base(path)],
			"%s is in %s but in neither table", filepath.Base(path), reproducerDir)
	}
	assert.Len(t, listed, len(onDisk), "the tables and %s hold different numbers of fixtures", reproducerDir)
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
			assert.NotZero(t, diags[0].Provenance.Position, "position provenance")
		})
	}
}

// TestDetectCycles_ReferenceObjectCyclesAreLeftToTheResolver pins that the
// scan reports nothing on a chain of reference objects, however it fails.
func TestDetectCycles_ReferenceObjectCyclesAreLeftToTheResolver(t *testing.T) {
	t.Parallel()
	for _, tc := range referenceObjectCycles {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, scanBytes(t, readReproducer(t, tc.file)))
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
	// The cases below carry a pointer whose prefix names a reference. The first
	// two are reference objects, which the scan leaves to the resolver; the last
	// two are schema hops landing on a node with no $ref, or on nothing. The
	// resolver reports each unresolved, so refusing one as a cycle would be an
	// over-refusal.
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
	assert.Empty(t, Cycles(InSource(0), sourceindex.Build(nil, sourceindex.MaxIndexedNodes), defaultSurplus))
	assert.Empty(t, scanBytes(t, nil))
}

// defaultSurplus is the alias budget a compile gets when its caller sets none,
// openapi.DefaultMaxAliasSurplus, which this package cannot import. The fixtures
// here that straddle it were sized against it; the compiler's own tests pin
// that the default reaching the scan is this one.
const defaultSurplus = 1 << 18

// scanBytes decodes source bytes and runs the refusals over the index built from
// them — what load does around Cycles, with the compile's one decode — under the
// default alias budget.
func scanBytes(t *testing.T, data []byte) []ir.Diagnostic {
	t.Helper()
	return Cycles(InSource(0), indexOf(t, data), defaultSurplus)
}

// scanPlaced is scanBytes with each diagnostic placed by its pointer too, as
// load places them, for a test that names a site by pointer.
func scanPlaced(t *testing.T, data []byte) []ir.Diagnostic {
	t.Helper()
	idx := indexOf(t, data)
	return Cycles(InTree(0, Pointers(idx.Root())), idx, defaultSurplus)
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

// reproducerDir is where the cycle fixtures live. The reader below and the table
// guard both derive their paths from it, so neither can end up looking somewhere
// the other is not.
const reproducerDir = "../../../../testdata/openapi"

func readReproducer(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(reproducerDir, file+".yaml"))
	require.NoError(t, err)
	return data
}

func TestRecoverCycleScan_PanicYieldsWarning(t *testing.T) {
	t.Parallel()
	got := recoverCycleScan(InSource(3), func() []ir.Diagnostic {
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
	got := recoverCycleScan(InSource(0), func() []ir.Diagnostic {
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
	assert.Equal(t, "root", anchorName(ynode.Alias(anchored)))
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

func TestSettleChains_DanglingRefIsNotCycle(t *testing.T) {
	t.Parallel()
	a := ynode.Map(ynode.Scalar("$ref"), ynode.Scalar("#/schemas/Missing"))
	root := ynode.Map(ynode.Scalar("schemas"), ynode.Map(ynode.Scalar("A"), a))

	chains := settleChains(nodeview.New(), root, []*yaml.Node{a}, sourceindex.MaxIndexedNodes)

	assert.Zero(t, chains.loopOf(a), "a dangling $ref is not a cycle")
}

func TestMappingPairs_Cases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    *yaml.Node
		want map[string]string
	}{
		{"nil node yields no pairs", nil, nil},
		{"non-mapping node yields no pairs", ynode.Scalar("x"), nil},
		{
			"alias-valued mapping is dereferenced at entry",
			ynode.Alias(ynode.Map(ynode.Scalar("k"), ynode.Scalar("v"))),
			map[string]string{"k": "v"},
		},
		{
			"alias key and alias value are dereferenced",
			ynode.Map(ynode.Alias(ynode.Scalar("k")), ynode.Alias(ynode.Scalar("v"))),
			map[string]string{"k": "v"},
		},
		{
			"non-scalar key after nodeview.Deref is skipped",
			ynode.Map(
				ynode.Map(ynode.Scalar("x"), ynode.Scalar("1")), ynode.Scalar("ignored"),
				ynode.Scalar("real"), ynode.Scalar("kept"),
			),
			map[string]string{"real": "kept"},
		},
		{
			"key aliasing a nil target is skipped",
			ynode.Map(ynode.Alias(nil), ynode.Scalar("ignored"), ynode.Scalar("real"), ynode.Scalar("kept")),
			map[string]string{"real": "kept"},
		},
		{
			"odd trailing key without a value is ignored",
			trailingKeyNode(),
			map[string]string{"a": "1"},
		},
		{
			"duplicate explicit Key: last wins",
			ynode.Map(ynode.Scalar("k"), ynode.Scalar("first"), ynode.Scalar("k"), ynode.Scalar("second")),
			map[string]string{"k": "second"},
		},
		{
			"a merged key still yields to a repeated explicit key",
			ynode.Map(ynode.Scalar("k"), ynode.Scalar("first"), ynode.Merge(), ynode.Map(ynode.Scalar("k"), ynode.Scalar("from-merge")),
				ynode.Scalar("k"), ynode.Scalar("second")),
			map[string]string{"k": "second"},
		},
		{
			"merge key contributes a mapping's pairs",
			ynode.Map(ynode.Merge(), ynode.Alias(ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))), ynode.Scalar("b"), ynode.Scalar("2")),
			map[string]string{"a": "1", "b": "2"},
		},
		{
			"merge value that is not a mapping contributes nothing",
			ynode.Map(ynode.Merge(), ynode.Scalar("not-a-mapping"), ynode.Scalar("b"), ynode.Scalar("2")),
			map[string]string{"b": "2"},
		},
		{
			"explicit key wins over merged key",
			ynode.Map(ynode.Scalar("a"), ynode.Scalar("explicit"), ynode.Merge(), ynode.Map(ynode.Scalar("a"), ynode.Scalar("from-merge"))),
			map[string]string{"a": "explicit"},
		},
		{
			"merge sequence: earlier source wins on a shared key",
			ynode.Map(ynode.Merge(), ynode.Seq(
				ynode.Map(ynode.Scalar("a"), ynode.Scalar("from-first")),
				ynode.Map(ynode.Scalar("a"), ynode.Scalar("from-second"), ynode.Scalar("b"), ynode.Scalar("only-in-second")),
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
		n := ynode.Map(
			ynode.Scalar("k"), ynode.Scalar("first"),
			ynode.Scalar("other"), ynode.Scalar("o"),
			ynode.Scalar("k"), ynode.Scalar("second"),
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
		got := v.MappingPairs(ynode.MergeChain(nodeview.MergeDepthLimit))
		assert.Equal(t, map[string]string{"leaf": "v"}, pairMap(got),
			"a chain exactly at the bound expands in full")
		assert.False(t, v.Exhausted(), "expanding to the bound is not exceeding it")
	})

	t.Run("merge chain past the depth bound stops at the bound", func(t *testing.T) {
		t.Parallel()
		v := nodeview.New()
		assert.Empty(t, v.MappingPairs(ynode.MergeChain(nodeview.MergeDepthLimit+2)),
			"a merge chain longer than the bound never reaches the leaf pair")
		assert.True(t, v.Exhausted(), "exceeding the bound is recorded for refCycles")
	})
}

func trailingKeyNode() *yaml.Node {
	n := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
	n.Content = append(n.Content, ynode.Scalar("dangling"))
	return n
}

func mergeSeqReuseNode() *yaml.Node {
	base := ynode.Map(ynode.Scalar("a"), ynode.Scalar("1"))
	return ynode.Map(ynode.Merge(), ynode.Seq(ynode.Alias(base), ynode.Alias(base)))
}

func selfReferentialMergeNode() *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	n.Content = []*yaml.Node{ynode.Merge(), ynode.Alias(n)}
	return n
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
	ref := ynode.Map(ynode.Scalar("$ref"), ynode.Scalar("#/components/schemas/B"))
	root := ynode.Map(ynode.Scalar("schemas"), ynode.Map(
		ynode.Scalar("A"), ynode.Map(ynode.Scalar("allOf"), ynode.Seq(ynode.Alias(ref), ynode.Alias(ref))),
		ynode.Scalar("B"), ynode.Map(ynode.Scalar("properties"), ynode.Alias(ref)),
		ynode.Scalar("C"), ynode.Alias(ref),
		// allOf whose value is the mapping itself, not a sequence: the only way
		// this node is entered in the schema-list role.
		ynode.Scalar("D"), ynode.Map(ynode.Scalar("allOf"), ynode.Alias(ref)),
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
		s.stack = append(s.stack, refTask{n: ynode.Map(), role: roleCount})
		s.collect(nil)
	}, "a task carrying an unhandled role is a programmer error")
}

func TestRefScanCollect_DeepNestingIsNotTruncated(t *testing.T) {
	t.Parallel()
	ref := ynode.Map(ynode.Scalar("$ref"), ynode.Scalar("#/components/schemas/A"))
	deep := ref
	for range 10010 { // past the 10,000 levels the recursive walk stopped at
		deep = ynode.Map(ynode.Scalar("items"), deep)
	}
	root := ynode.Map(ynode.Scalar("schemas"), ynode.Map(ynode.Scalar("A"), deep))

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

func TestDetectCycles_MergeChainWithinBoundIsClean(t *testing.T) {
	t.Parallel()
	diags := scanWithin(t, ynode.MergeChainSpec(nodeview.MergeDepthLimit), "blowup on an in-bound merge chain")
	assert.Empty(t, diags, "a merge chain the scan can expand in full is clean")
}

func TestDetectCycles_MergeChainPastBoundStaysFastAndWarns(t *testing.T) {
	t.Parallel()
	diags := scanWithin(t, ynode.MergeChainSpec(1600), "super-linear blowup on a long merge chain")
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
		done <- Cycles(InSource(0), idx, defaultSurplus)
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
	inSeq := ynode.Map(ynode.Scalar("$ref"), ynode.Scalar("#/components/schemas/A"))
	underSchema := ynode.Map(ynode.Scalar("$ref"), ynode.Scalar("#/components/schemas/B"))
	notASchema := ynode.Map(ynode.Scalar("$ref"), ynode.Scalar("#/components/schemas/C"))

	root := ynode.Map(
		// a sequence at an outside position: each element stays outside
		ynode.Scalar("parameters"), ynode.Seq(ynode.Map(ynode.Scalar("schema"), underSchema)),
		// a data key: everything beneath it is data, whatever it looks like
		ynode.Scalar("example"), notASchema,
		// a mapping at an outside position that is a reference object itself
		ynode.Scalar("requestBody"), inSeq,
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

// TestDetectCycles_ATrailingEmptyTokenLandsNowhere pins the reading of a
// pointer ending in an empty token. RFC 6901 reads '/A/' as naming the key ""
// under A, where each of these documents holds a $ref back to A. The library
// lands on no such member: in the schema model it fails the pointer, and the
// second document, whose '/u/' it reads as u itself, compiles cleanly.
func TestDetectCycles_ATrailingEmptyTokenLandsNowhere(t *testing.T) {
	t.Parallel()
	members := map[string]string{
		"a-schema":             "    A: {$ref: '#/components/schemas/A/', '': {$ref: '#/components/schemas/A'}}\n",
		"a-member-of-a-schema": "    A: {$ref: '#/components/schemas/A/u/', u: {'': {$ref: '#/components/schemas/A'}}}\n",
	}
	for name, member := range members {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, scanBytes(t, schemasDoc([]string{member})))
		})
	}
}

// TestDetectCycles_PointerIsNormalizedLikeTheResolver pins the scan's pointer
// reading to speakeasy's. It splits a $ref on '#', trims whitespace from both
// halves and percent-decodes the pointer (references/reference.go GetURI and
// GetJSONPointer), so '#/components/schemas/A ' names A there. A scan that
// reads the raw value instead calls that pointer dangling and lets a
// self-reference through. A '+' is the one byte it reads otherwise (see
// TestDetectCycles_PlusInAPointerIsAPlus).
func TestDetectCycles_PointerIsNormalizedLikeTheResolver(t *testing.T) {
	t.Parallel()
	refs := map[string]string{
		"trailing space":  "'#/components/schemas/A '",
		"leading space":   "' #/components/schemas/A'",
		"percent-encoded": "'#/components/schemas/%41'",
	}
	for name, ref := range refs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			diags := scanBytes(t, schemasDoc([]string{"    A: {$ref: " + ref + "}\n"}))
			require.NotEmpty(t, diags, "the resolver reads this pointer as naming A")
			assert.Equal(t, diag.CyclicRef, diags[0].Code)
		})
	}
}

// TestDetectCycles_PlusInAPointerIsAPlus pins that the scan reads '+' in a $ref
// fragment as itself (RFC 3986 section 3.5), not as the space the resolver's own
// decoder makes of it: a key spelled with a plus is reached by a pointer spelled
// with one, a key spelled with a space is not, and load hands the resolver the
// same reading (GitHub #911). The escaped spelling is the control, which both
// readings agree on.
func TestDetectCycles_PlusInAPointerIsAPlus(t *testing.T) {
	t.Parallel()
	const head = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\n"
	tests := []struct {
		name    string
		src     string
		isCycle bool
	}{
		{name: "a plus reaches the key spelled with a plus", isCycle: true,
			src: head + "x-a+b: {$ref: '#/x-a+b'}\ncomponents:\n  schemas:\n    A: {$ref: '#/x-a+b'}\n"},
		{name: "an escaped plus reaches the same key", isCycle: true,
			src: head + "x-a+b: {$ref: '#/x-a%2Bb'}\ncomponents:\n  schemas:\n    A: {$ref: '#/x-a+b'}\n"},
		{name: "a plus does not reach the key spelled with a space",
			src: head + "x-a b: {$ref: '#/x-a+b'}\ncomponents:\n  schemas:\n    A: {$ref: '#/x-a%20b'}\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diags := scanBytes(t, []byte(tc.src))
			if !tc.isCycle {
				assert.Empty(t, diags, "the chain ends at a key that is not there")
				return
			}
			require.NotEmpty(t, diags, "the chain closes on the key it starts at")
			assert.Equal(t, diag.CyclicRef, diags[0].Code)
		})
	}
}

// TestDetectCycles_AnchorFragmentIsNotWalked pins the fix for GitHub #523: a
// fragment with no leading '/' names a $anchor, not a pointer, and the resolver
// never walks it as one. Reading it as though '#x-s' were the pointer 'x-s' made
// the scan walk it from the root as a key, refusing a document the resolver
// never treats as a cycle.
//
// The pointer-spelled twin is the point of the pair. The anchor case fails if
// the leading-'/' refusal is reverted; the pointer case fails if a fix refuses
// the key rather than the anchor spelling, which would swallow this real cycle.
func TestDetectCycles_AnchorFragmentIsNotWalked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		isCycle bool
	}{
		{name: "anchor spelling names a $anchor, not a pointer", src: `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-s:
  $ref: '#x-s'
components:
  schemas:
    A:
      $ref: '#x-s'
`},
		{name: "the pointer spelling is a real cycle through the document root", isCycle: true, src: `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-s:
  $ref: '#/x-s'
components:
  schemas:
    A:
      $ref: '#/x-s'
`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diags := scanBytes(t, []byte(tc.src))
			if !tc.isCycle {
				assert.Empty(t, diags, "'#x-s' names a $anchor, which the resolver's pointer walk never enters")
				return
			}
			require.NotEmpty(t, diags, "'#/x-s' is a real pointer cycle through the document root")
			assert.Equal(t, diag.CyclicRef, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
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

// TestInSource_AnchorsANodeAndNamesTheSourceForNone pins the plain locator: a
// placed node is reported at its own line and column, a node the parser never
// placed carries no position, and no node at all names the source alone —
// never a position fabricated from a nil.
func TestInSource_AnchorsANodeAndNamesTheSourceForNone(t *testing.T) {
	t.Parallel()
	locate := InSource(4)

	tests := map[string]struct {
		node *yaml.Node
		want ir.Provenance
	}{
		"a placed node reports its line and column": {
			node: &yaml.Node{Kind: yaml.ScalarNode, Line: 12, Column: 7},
			want: ir.Provenance{Source: 4, Position: ir.Position{Line: 12, Column: 7}},
		},
		"a node the parser never placed carries no position": {
			node: &yaml.Node{},
			want: ir.Provenance{Source: 4},
		},
		"no node names the source alone": {
			node: nil,
			want: ir.Provenance{Source: 4},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, locate(tt.node))
		})
	}
}

// TestCycles_AnchorsThroughTheLocator pins that a refusal takes its provenance
// from the locator it is handed rather than from the node it found. A locator
// the caller composes over an overlay's attribution is how a refusal on a
// grafted node — one with no line and column of its own — comes to name the
// overlay (GitHub #476); a scan that read the node directly would bypass it.
func TestCycles_AnchorsThroughTheLocator(t *testing.T) {
	t.Parallel()
	for name, fixture := range map[string]string{
		"a $ref cycle":           "cycle_self_ref",
		"a recursive anchor":     "cycle_yaml_anchor",
		"an alias amplification": "amplification_alias_bomb",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var seen *yaml.Node
			locate := func(n *yaml.Node) ir.Provenance {
				seen = n
				return ir.Provenance{Source: 9, Pointer: "/somewhere/the/locator/decided"}
			}

			got := Cycles(locate, indexOf(t, readReproducer(t, fixture)), defaultSurplus)
			require.Len(t, got, 1)
			assert.Equal(t, ir.Provenance{Source: 9, Pointer: "/somewhere/the/locator/decided"}, got[0].Provenance)
			assert.NotNil(t, seen, "the locator was handed the node the refusal is about")
		})
	}
}

// TestAliases_RefusesTheYAMLShapesAndNotTheOpenAPIOnes pins what separates
// Aliases from Cycles. Both refuse an anchor naming its own ancestor and an
// alias expansion far past the document's size, because those are properties
// of YAML and any document carries them. Only Cycles follows $ref chains,
// because that is what an OpenAPI resolver does — and an overlay, which
// Aliases exists for, is a document nothing resolves.
func TestAliases_RefusesTheYAMLShapesAndNotTheOpenAPIOnes(t *testing.T) {
	t.Parallel()
	aliases := func(src string) []ir.Diagnostic { return Aliases(InSource(0), indexOf(t, []byte(src)), defaultSurplus) }

	anchor := aliases("a: &x [*x]\n")
	require.Len(t, anchor, 1)
	assert.Equal(t, diag.CyclicRef, anchor[0].Code, "a recursive anchor expands without end")

	bomb := aliases(string(readReproducer(t, "amplification_alias_bomb")))
	require.Len(t, bomb, 1)
	assert.Equal(t, diag.AliasAmplification, bomb[0].Code, "an alias bomb expands far past its size")

	refCycle := string(readReproducer(t, "cycle_self_ref"))
	require.NotEmpty(t, scanBytes(t, []byte(refCycle)), "Cycles refuses the $ref cycle")
	assert.Empty(t, aliases(refCycle), "Aliases leaves it: nothing resolves an overlay's references")

	assert.Empty(t, aliases("a: &x {p: 1}\nb: *x\nc: *x\n"), "ordinary anchor reuse is not a refusal")
}

// TestAliases_DegradesAScanFaultToAWarning pins that Aliases runs under the
// same barrier as Cycles: a fault in the walk is reported as an incomplete
// scan, never raised through a caller that has not yet refused anything.
//
// The locator faults only when asked about a node, which is what the walk asks
// it; asked about no node, as the barrier's own report does, it answers. A
// locator that faulted on both would fault inside the recovery too, and that
// tests a locator this compiler does not have rather than the barrier it does.
func TestAliases_DegradesAScanFaultToAWarning(t *testing.T) {
	t.Parallel()
	panicking := func(n *yaml.Node) ir.Provenance {
		if n != nil {
			panic("locator fault")
		}
		return ir.Provenance{Source: 0}
	}

	got := Aliases(panicking, indexOf(t, []byte("a: &x [*x]\n")), defaultSurplus)

	require.Len(t, got, 1)
	assert.Equal(t, diag.CycleScanFailed, got[0].Code)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity)
}

// inTreeFixture holds each shape of node whose pointer InTree answers: keys that
// need escaping, a sequence, an anchor with an alias to it, and a `<<` merge
// spelled inline and through an alias.
const inTreeFixture = `plain: 1
a/b: 2
c~d: 3
~1: 4
a~/b: 5
seq:
  - first
  - k: v
anchored: &base
  p: 1
copy: *base
merged:
  <<: {m: 1}
  own: 2
aliased:
  <<: *base
  own: 3
named: &nm renamed
*nm : 6
!!merge tagged: 7
`

// decodeTree decodes inTreeFixture afresh into the document node a parse gives
// the compiler, so no case shares nodes with another.
func decodeTree(t *testing.T) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(inTreeFixture), &doc), "the fixture must decode")
	return &doc
}

// memberOf returns the key and value nodes of the member of mapping m named
// key, read straight off Content so the lookup shares nothing with InTree.
func memberOf(t *testing.T, m *yaml.Node, key string) (k, v *yaml.Node) {
	t.Helper()
	require.Equal(t, yaml.MappingNode, m.Kind, "looking up %q in a non-mapping", key)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	require.Failf(t, "no such member", "%q", key)
	return nil, nil
}

// valueAt follows path from doc's content: a key into a mapping, a decimal
// index into a sequence. It does not follow an alias.
func valueAt(t *testing.T, doc *yaml.Node, path ...string) *yaml.Node {
	t.Helper()
	n := doc.Content[0]
	for _, step := range path {
		if n.Kind == yaml.SequenceNode {
			i, err := strconv.Atoi(step)
			require.NoError(t, err)
			n = n.Content[i]
			continue
		}
		_, n = memberOf(t, n, step)
	}
	return n
}

// keyAt is valueAt for the key of the last member on path.
func keyAt(t *testing.T, doc *yaml.Node, path ...string) *yaml.Node {
	t.Helper()
	k, _ := memberOf(t, valueAt(t, doc, path[:len(path)-1]...), path[len(path)-1])
	return k
}

// TestInTree_PointsAtEachKindOfNode pins the pointer a finding on each kind of
// node carries, whether the tree is rooted at its document node or at the
// content: a key shares its member's pointer, a sequence item is its index,
// tokens are RFC 6901-escaped, an alias is a leaf indexed where it is written
// while its target keeps the anchor's pointer, and nothing under a `<<` merge
// key has one. A pair is keyed as the parser keys it: an alias key by the
// scalar it names, a !!merge-tagged key other than `<<` as written. Every row
// keeps its position.
func TestInTree_PointsAtEachKindOfNode(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		node    func(t *testing.T, doc *yaml.Node) *yaml.Node
		pointer jsontext.Pointer
	}{
		"the document node": {func(_ *testing.T, doc *yaml.Node) *yaml.Node { return doc }, ""},
		"the document's content": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc)
		}, ""},
		"a mapping value": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "plain")
		}, "/plain"},
		"a mapping key": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return keyAt(t, doc, "plain")
		}, "/plain"},
		"a key holding a slash": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "a/b")
		}, "/a~1b"},
		"a key holding a tilde": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "c~d")
		}, "/c~0d"},
		"a key spelling an escape": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "~1")
		}, "/~01"},
		"a key holding a tilde and a slash": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return keyAt(t, doc, "a~/b")
		}, "/a~0~1b"},
		"a sequence item": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "seq", "0")
		}, "/seq/0"},
		"a mapping in a sequence": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "seq", "1")
		}, "/seq/1"},
		"a value in a mapping in a sequence": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "seq", "1", "k")
		}, "/seq/1/k"},
		"a key in a mapping in a sequence": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return keyAt(t, doc, "seq", "1", "k")
		}, "/seq/1/k"},
		"an anchored mapping": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "anchored")
		}, "/anchored"},
		"an alias is indexed where it is written": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "copy")
		}, "/copy"},
		"an alias's target is indexed at its anchor": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "copy").Alias
		}, "/anchored"},
		"a member of an anchor": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "anchored", "p")
		}, "/anchored/p"},
		"a mapping holding a merge": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "merged")
		}, "/merged"},
		"the merge key": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return keyAt(t, doc, "merged", "<<")
		}, ""},
		"the mapping a merge key holds": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "merged", "<<")
		}, ""},
		"a value under a merge": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "merged", "<<", "m")
		}, ""},
		"a key under a merge": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return keyAt(t, doc, "merged", "<<", "m")
		}, ""},
		"a member beside a merge": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "merged", "own")
		}, "/merged/own"},
		"an alias a merge key holds": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "aliased", "<<")
		}, ""},
		"a member beside an aliased merge": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "aliased", "own")
		}, "/aliased/own"},
		"a value under an alias key": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "nm")
		}, "/renamed"},
		"an alias key": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return keyAt(t, doc, "nm")
		}, "/renamed"},
		"a value under a key tagged !!merge": {func(t *testing.T, doc *yaml.Node) *yaml.Node {
			return valueAt(t, doc, "tagged")
		}, "/tagged"},
	}
	for name, tt := range tests {
		for rooted, root := range map[string]func(*yaml.Node) *yaml.Node{
			"at the document": func(doc *yaml.Node) *yaml.Node { return doc },
			"at its content":  func(doc *yaml.Node) *yaml.Node { return doc.Content[0] },
		} {
			t.Run(name+", "+rooted, func(t *testing.T) {
				t.Parallel()
				doc := decodeTree(t)
				node := tt.node(t, doc)
				want := ir.Provenance{
					Source:   3,
					Pointer:  tt.pointer,
					Position: ir.Position{Line: node.Line, Column: node.Column},
				}

				got := InTree(3, Pointers(root(doc)))(node)

				assert.Empty(t, cmp.Diff(want, got))
				assert.NotZero(t, got.Position, "a parsed node has a position")
			})
		}
	}
}

// TestInTree_AnswersWithoutAPointerWhereTheTreeHasNone pins the answers that
// are not a position in the tree: a node the tree does not hold, as one built
// elsewhere or by hand, has its position and no pointer; no node names the
// source alone; and no tree leaves the position alone. A node's identity, not
// its value, decides whether the tree holds it.
func TestInTree_AnswersWithoutAPointerWhereTheTreeHasNone(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		root func(t *testing.T) *yaml.Node
		node func(t *testing.T) *yaml.Node
		want ir.Provenance
	}{
		"a node built by hand": {
			root: decodeTree,
			node: func(*testing.T) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: "1", Line: 5, Column: 6} },
			want: ir.Provenance{Source: 3, Position: ir.Position{Line: 5, Column: 6}},
		},
		"an equal node of another tree": {
			root: decodeTree,
			node: func(t *testing.T) *yaml.Node { return valueAt(t, decodeTree(t), "plain") },
			want: ir.Provenance{Source: 3, Position: ir.Position{Line: 1, Column: 8}},
		},
		"no node": {
			root: decodeTree,
			node: func(*testing.T) *yaml.Node { return nil },
			want: ir.Provenance{Source: 3},
		},
		"no tree": {
			root: func(*testing.T) *yaml.Node { return nil },
			node: func(*testing.T) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Line: 2, Column: 3} },
			want: ir.Provenance{Source: 3, Position: ir.Position{Line: 2, Column: 3}},
		},
		"no tree and no node": {
			root: func(*testing.T) *yaml.Node { return nil },
			node: func(*testing.T) *yaml.Node { return nil },
			want: ir.Provenance{Source: 3},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := InTree(3, Pointers(tt.root(t)))(tt.node(t))

			assert.Empty(t, cmp.Diff(tt.want, got))
		})
	}
}

// stepsWithin indexes root under a limit of exactly the nodes it holds,
// requiring the walk to have indexed them all: one that entered a node twice
// would spend the limit before it ran out of nodes, so it reports itself cut
// short rather than walking on.
func stepsWithin(t *testing.T, root *yaml.Node, nodes int) map[*yaml.Node]step {
	t.Helper()
	steps, complete := stepsIn(root, nodes)
	require.True(t, complete, "the walk entered more than the tree's %d nodes", nodes)
	return steps
}

// TestStepsIn_IndexesEachNodeOnce pins that the index holds every node of a
// hand-built tree once, at the step that first reaches it, whatever the tree
// shape: a node placed twice is indexed where it is first written, a ladder of
// mappings each holding the next twice is walked once per rung, and a node
// inside its own Content ends the walk. Entering a node twice would make the
// shared node's step the later one, and spend the limit stepsWithin sets on
// the cycle, which reports the walk cut short.
func TestStepsIn_IndexesEachNodeOnce(t *testing.T) {
	t.Parallel()

	t.Run("a node placed twice", func(t *testing.T) {
		t.Parallel()
		shared := ynode.Scalar("v")
		first, second := ynode.Scalar("a"), ynode.Scalar("b")
		root := ynode.Map(first, shared, second, shared)

		steps := stepsWithin(t, root, 4)

		assert.Equal(t, jsontext.Pointer("/a"), pointerOf(steps, shared), "indexed where it is first written")
		for name, n := range map[string]*yaml.Node{"the root": root, "a key": first, "the other key": second} {
			_, indexed := steps[n]
			assert.True(t, indexed, name)
		}
		assert.Len(t, steps, 4, "the root, two keys and the shared value")
	})

	t.Run("a ladder of nodes each placed twice", func(t *testing.T) {
		t.Parallel()
		const rungs = 20
		leaf := ynode.Scalar("leaf")
		rung := leaf
		for range rungs {
			rung = ynode.Map(ynode.Scalar("a"), rung, ynode.Scalar("b"), rung)
		}

		steps := stepsWithin(t, rung, 1+3*rungs)

		assert.Equal(t, jsontext.Pointer(strings.Repeat("/a", rungs)), pointerOf(steps, leaf))
		assert.Len(t, steps, 1+3*rungs, "each rung is indexed once, with its two keys and the leaf")
	})

	t.Run("a mapping inside its own Content", func(t *testing.T) {
		t.Parallel()
		key := ynode.Scalar("self")
		root := ynode.Map(key, nil)
		root.Content[1] = root

		steps := stepsWithin(t, root, 2)

		assert.Equal(t, jsontext.Pointer(""), pointerOf(steps, root), "the root stays the root")
		assert.Equal(t, jsontext.Pointer("/self"), pointerOf(steps, key))
		assert.Len(t, steps, 2)
	})

	t.Run("a sequence inside its own Content", func(t *testing.T) {
		t.Parallel()
		item := ynode.Scalar("x")
		root := ynode.Seq(item, nil)
		root.Content[1] = root

		steps := stepsWithin(t, root, 2)

		assert.Equal(t, jsontext.Pointer("/0"), pointerOf(steps, item))
		assert.Equal(t, jsontext.Pointer(""), pointerOf(steps, root))
		assert.Len(t, steps, 2)
	})
}

// TestStepsIn_StopsAtItsLimit pins the bound on the walk: past limit nodes it
// enters no more, reports itself cut short, and leaves each node it did index
// with its own pointer, so a node past the bound is located by position alone.
// An empty document node holds nothing to walk, and is indexed whole.
func TestStepsIn_StopsAtItsLimit(t *testing.T) {
	t.Parallel()
	first, second := ynode.Scalar("v1"), ynode.Scalar("v2")
	root := ynode.Map(ynode.Scalar("a"), first, ynode.Scalar("b"), second)

	steps, complete := stepsIn(root, 3)
	assert.False(t, complete, "five nodes do not fit a limit of three")
	assert.Len(t, steps, 3)
	_, indexed := steps[second]
	assert.False(t, indexed, "the walk enters nothing past its limit")
	got := []jsontext.Pointer{pointerOf(steps, root), pointerOf(steps, second)}
	assert.Equal(t, []jsontext.Pointer{"", ""}, got)

	steps, complete = stepsIn(root, 5)
	assert.True(t, complete, "five nodes fit a limit of five")
	assert.Equal(t, jsontext.Pointer("/b"), pointerOf(steps, second))

	empty := &yaml.Node{Kind: yaml.DocumentNode}
	steps, complete = stepsIn(empty, 1)
	assert.True(t, complete)
	assert.Equal(t, map[*yaml.Node]step{empty: {}}, steps)
	assert.Equal(t, ir.Provenance{Source: 0, Position: ir.Position{Line: 1, Column: 1}},
		InTree(0, Pointers(empty))(&yaml.Node{Kind: yaml.ScalarNode, Line: 1, Column: 1}),
		"a node of no tree, asked of an empty document, is located by position alone")
}

// TestInTree_AllocatesInTheTreesSizeNotItsPointers pins the cost of locating a
// node: one long key over many children allocates in proportion to the nodes,
// not to the key once per node. It builds the locator, asks about one deep
// node, and holds the bytes allocated to 32 MiB. The index holds a holder and
// a token per node and the one pointer asked for spells the key once, while a
// pointer stored per node spells it for every node beneath the key: a copy
// per child alone is 256 MiB.
//
// It is not parallel: a parallel test would allocate during the measurement.
func TestInTree_AllocatesInTheTreesSizeNotItsPointers(t *testing.T) {
	const (
		keyLen   = 1 << 16
		children = 4096
		bound    = 32 << 20
	)
	require.Less(t, bound, children*keyLen, "the bound must be under what a stored pointer per value allocates")
	longKey := strings.Repeat("k", keyLen)
	var deep *yaml.Node
	members := make([]*yaml.Node, 0, 2*children)
	for i := range children {
		deep = ynode.Scalar("v")
		members = append(members, ynode.Scalar("c"+strconv.Itoa(i)), ynode.Map(ynode.Scalar("leaf"), deep))
	}
	root := ynode.Map(ynode.Scalar(longKey), ynode.Map(members...))
	want := jsontext.Pointer("/" + longKey + "/c" + strconv.Itoa(children-1) + "/leaf")

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	locate := InTree(0, Pointers(root))
	got := locate(deep)
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(root)

	assert.True(t, got.Pointer == want, "the pointer asked for is %d bytes, want %d", len(got.Pointer), len(want))
	alloc := after.TotalAlloc - before.TotalAlloc
	assert.Less(t, alloc, uint64(bound), "indexing the tree and asking for one pointer allocated %d bytes", alloc)
}

// TestPointers_SaysWhetherTheTreeHoldsTheNode pins the answer a caller other
// than InTree reads: the node's pointer and that the tree holds it, the
// document's own content included at "", and no pointer, held by nothing, for
// a node another tree holds or for a nil node or root.
func TestPointers_SaysWhetherTheTreeHoldsTheNode(t *testing.T) {
	t.Parallel()
	value := ynode.Scalar("v")
	root := ynode.Map(ynode.Scalar("k"), value)
	pointer := Pointers(root)

	type answer struct {
		pointer jsontext.Pointer
		held    bool
	}
	ask := func(f func(*yaml.Node) (jsontext.Pointer, bool), n *yaml.Node) answer {
		p, held := f(n)
		return answer{p, held}
	}
	assert.Equal(t, answer{"/k", true}, ask(pointer, value))
	assert.Equal(t, answer{"", true}, ask(pointer, root), "the root is held, at the whole document")
	assert.Equal(t, answer{"", false}, ask(pointer, ynode.Scalar("v")), "an equal node of another tree is not")
	assert.Equal(t, answer{"", false}, ask(pointer, nil))
	assert.Equal(t, answer{"", false}, ask(Pointers(nil), value))
}
