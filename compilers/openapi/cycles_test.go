package openapi

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

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// cycleReproducers are real crash inputs from GitHub #12 and #26: schema
// cycles that never reach a node with a top-level $ref, so a scan that only
// looks there misses them. Sibling-typed variants (a $ref plus a concrete
// `type`) crash the same way, since speakeasy follows the $ref regardless.
//
// The #26 shapes each probe a gap between the scan's raw yaml.Node view and
// speakeasy's resolved one: an alias for the $ref value (alias-ref-value), a
// $ref under a JSONSchema keyword the old key set missed (content-schema), an
// alias for the `$ref` key itself (alias-ref-key), a `<<` merge contributing a
// $ref no literal key does (merge-key-ref), an alias for the whole schema node
// (alias-schema-node), and one $ref node reused in two schema positions
// (alias-dual-position) — this last exposed the ref-collection walk sharing
// one visited-node set across roles, so whichever role reached the node first
// silently starved the other.
//
// duplicate-key covers a mapping that declares a key twice: speakeasy keeps
// the last occurrence, so reading first-key-wins hid the cycle. Found by
// FuzzCycleDetector.
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
}

// TestDetectCycles_Reproducers pins that each degenerate cycle is diagnosed as an
// error with line:col provenance, directly at the detector boundary.
func TestDetectCycles_Reproducers(t *testing.T) {
	t.Parallel()
	for _, tc := range cycleReproducers {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := readReproducer(t, tc.file)
			diags := detectCycles(0, data)
			require.NotEmpty(t, diags, "degenerate cycle must be diagnosed")
			assert.Equal(t, diag.CyclicRef, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.NotEmpty(t, diags[0].Provenance.Pointer, "line:col provenance")
		})
	}
}

// TestCompile_CyclicSpecDoesNotCrash drives each degenerate spec through the full
// public Compile path: it must surface an error diagnostic and a nil document
// rather than crashing the process with a fatal stack overflow (GitHub #12), and
// it must not report a Go error — a cyclic spec is a spec problem, not I/O.
func TestCompile_CyclicSpecDoesNotCrash(t *testing.T) {
	t.Parallel()
	for _, tc := range cycleReproducers {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := readReproducer(t, tc.file)
			doc, diags, err := New().Compile(t.Context(),
				[]compilers.Source{{Path: tc.file + ".yaml", Data: data}}, compilers.Options{})
			require.NoError(t, err, "cyclic spec is a spec problem, not a Go error")
			assert.Nil(t, doc, "the compiler refuses to lower a cyclic spec")
			assertHasErrorCode(t, diags, diag.CyclicRef)
		})
	}
}

// componentOnlyCycles are reference-object cycles whose every hop names a
// component. speakeasy's resolver refuses these itself, with a message that
// names the chain, so the pre-parse scan must leave them alone rather than
// pre-empt the better diagnostic.
var componentOnlyCycles = []struct{ name, data string }{
	{"component-path-items", `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a: {$ref: '#/components/pathItems/A'}
components:
  pathItems:
    A: {$ref: '#/components/pathItems/B'}
    B: {$ref: '#/components/pathItems/A'}
`},
	{"component-responses", `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /a:
    get:
      operationId: a
      responses:
        "200": {$ref: '#/components/responses/A'}
components:
  responses:
    A: {$ref: '#/components/responses/B'}
    B: {$ref: '#/components/responses/A'}
`},
}

// TestDetectCycles_ComponentOnlyCyclesLeftToResolver pins the boundary the
// outside-ref scan draws. These documents are cyclic, but every hop is a
// component reference, which speakeasy already refuses by name — so the scan
// must not claim them, and the compile must still surface the resolver's own
// error rather than falling silent.
func TestDetectCycles_ComponentOnlyCyclesLeftToResolver(t *testing.T) {
	t.Parallel()
	for _, tc := range componentOnlyCycles {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, detectCycles(0, []byte(tc.data)),
				"a components-only cycle is the resolver's to report")

			_, diags, err := New().Compile(t.Context(),
				[]compilers.Source{{Path: tc.name + ".yaml", Data: []byte(tc.data)}}, compilers.Options{})
			require.NoError(t, err)
			assertHasErrorCode(t, diags, diag.UnresolvedRef)
		})
	}
}

// TestDetectCycles_LegalRecursionClean is the control: a legal recursive schema
// (a concrete node whose property $refs itself) has a concrete node in the cycle
// and must not be flagged.
func TestDetectCycles_LegalRecursionClean(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../testdata/conformance/openapi/recursive.yaml")
	require.NoError(t, err)
	assert.Empty(t, detectCycles(0, data), "legal recursion is not a degenerate cycle")
}

// refShapedDataSpecs are legal documents that speakeasy compiles without a
// crash: a $ref-shaped mapping inside example/default data or an x-* extension
// is opaque data (never resolved), and a pure-$ref cycle among non-schema
// reference objects is caught by speakeasy's own resolver guard. The pre-parse
// detector must leave all of them alone rather than refuse a valid source.
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
}

// TestDetectCycles_RefShapedDataIsClean pins that the detector does not fire on a
// $ref-shaped structure outside a schema position: those never reach the
// crashing schema resolver, so flagging them would refuse a legal document.
func TestDetectCycles_RefShapedDataIsClean(t *testing.T) {
	t.Parallel()
	for _, tc := range refShapedDataSpecs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, detectCycles(0, []byte(tc.data)),
				"a ref-shaped structure outside a schema position is not a degenerate cycle")
		})
	}
}

// TestCompile_RefShapedDataNotRefused drives the same legal documents through the
// full public Compile path: the compiler must lower them (non-nil document) and
// must not raise a cyclic-ref diagnostic, since speakeasy handles them cleanly.
func TestCompile_RefShapedDataNotRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range refShapedDataSpecs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, diags, err := New().Compile(t.Context(),
				[]compilers.Source{{Path: tc.name + ".yaml", Data: []byte(tc.data)}}, compilers.Options{})
			require.NoError(t, err)
			assert.NotNil(t, doc, "a legal ref-shaped-data spec must still lower")
			for _, d := range diags {
				assert.NotEqualf(t, diag.CyclicRef, d.Code, "must not refuse as a cyclic ref: %+v", d)
			}
		})
	}
}

// TestDetectCycles_NonYAMLIsNoCycle pins that undecodable input yields no cycle
// diagnostics: the main parser owns reporting a parse problem.
func TestDetectCycles_NonYAMLIsNoCycle(t *testing.T) {
	t.Parallel()
	assert.Empty(t, detectCycles(0, nil))
	assert.Empty(t, detectCycles(0, []byte("\t\x00: [")))
}

func readReproducer(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/openapi/" + file + ".yaml")
	require.NoError(t, err)
	return data
}

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

// ymerge is a `<<` key node as a parser produces one. yaml.v3 resolves every
// plain, non-specific, and explicitly tagged `<<` scalar to !!merge, and that
// resolved tag is what speakeasy — and so isMergeKey — requires, so a hand-built
// merge key must carry it or it is an ordinary string key.
func ymerge() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: "<<", Tag: mergeTag}
}

// TestRecoverCycleScan_PanicYieldsWarning pins the bounded-recursion backstop: a
// scan that panics must degrade to a single non-fatal warning that keeps the
// failure observable, never propagate the panic and never silently vanish.
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

// TestRecoverCycleScan_PassesThroughResult pins that a scan that does not panic
// returns its diagnostics unchanged.
func TestRecoverCycleScan_PassesThroughResult(t *testing.T) {
	t.Parallel()
	want := []ir.Diagnostic{{Code: diag.CyclicRef, Severity: ir.SeverityError}}
	got := recoverCycleScan(0, func() []ir.Diagnostic {
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

// TestRefScanCollect_NilRoot pins that a nil root terminates the collection walk
// without collecting refs — push drops it before anything is enqueued.
func TestRefScanCollect_NilRoot(t *testing.T) {
	t.Parallel()
	s := newRefScan()
	s.collect(nil)
	assert.Empty(t, s.out)
	assert.Empty(t, s.stack, "the worklist drains to empty")
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
	cyclic, _ := newRefScan().followRefChain(root, nodes[0])
	assert.False(t, cyclic,
		"a chain longer than the depth cap exits without flagging a cycle")
}

// TestFollowRefChain_SafeMemoShortCircuits pins the memoization: a node already
// proven chain-terminating is trusted without re-walking its edges, and every
// node that reaches it is recorded as terminating too. The same graph is a
// genuine cycle with an empty memo, which isolates the short-circuit.
func TestFollowRefChain_SafeMemoShortCircuits(t *testing.T) {
	t.Parallel()
	a := ymap(yscalar("$ref"), yscalar("#/schemas/B"))
	b := ymap(yscalar("$ref"), yscalar("#/schemas/A"))
	schemas := ymap(yscalar("A"), a, yscalar("B"), b)
	root := ymap(yscalar("schemas"), schemas)

	cyclic, _ := newRefScan().followRefChain(root, a)
	assert.True(t, cyclic, "A -> B -> A is cyclic with an empty memo")

	s := newRefScan()
	s.safe[b] = true
	memoed, _ := s.followRefChain(root, a)
	assert.False(t, memoed, "a chain reaching a memoized-safe node is not a cycle")
	assert.True(t, s.safe[a], "the walk records the reaching node as terminating too")
}

// TestFollowRefChain_DanglingRefIsNotCycle pins that a $ref whose target does not
// resolve ends the chain as terminating rather than cyclic: the missing target is
// reported downstream, and the node is recorded safe so a later chain reusing it
// short-circuits.
func TestFollowRefChain_DanglingRefIsNotCycle(t *testing.T) {
	t.Parallel()
	a := ymap(yscalar("$ref"), yscalar("#/schemas/Missing"))
	root := ymap(yscalar("schemas"), ymap(yscalar("A"), a))
	s := newRefScan()
	cyclic, _ := s.followRefChain(root, a)
	assert.False(t, cyclic, "a dangling $ref is not a cycle")
	assert.True(t, s.safe[a], "the dangling node is recorded terminating")
}

// TestChildByToken_NilNode pins that a nil node has no child for any token.
func TestChildByToken_NilNode(t *testing.T) {
	t.Parallel()
	assert.Nil(t, newNodeView().childByToken(nil, "anything"))
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
			got := newNodeView().resolvePointer(root, tc.ref)
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

// TestMappingPairs_Cases covers nodeView's expansion: shapes a real parse
// cannot produce (non-scalar key, merge sequence, self-referential merge) and
// ones it can (alias key/value, single-mapping merge, duplicate key). Three
// precedence rules only show up in the result, so they're pinned here: a
// duplicate explicit key resolves to its last occurrence (matching speakeasy,
// which applies every occurrence in turn); an explicit key still outranks a
// merged one; and a merge whose `<<` aliases its own mapping contributes
// nothing rather than looping forever.
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
			"non-scalar key after deref is skipped",
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
			"duplicate explicit key: last wins",
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
			got := newNodeView().mappingPairs(tc.n)
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
		got := newNodeView().mappingPairs(n)
		require.Len(t, got, 2)
		assert.Equal(t, "other", got[0].key)
		assert.Equal(t, "k", got[1].key)
		assert.Equal(t, "second", got[1].val.Value)
	})

	t.Run("merge chain at the depth bound still reaches the leaf", func(t *testing.T) {
		t.Parallel()
		v := newNodeView()
		got := v.mappingPairs(mergeChain(maxMergeDepth))
		assert.Equal(t, map[string]string{"leaf": "v"}, pairMap(got),
			"a chain exactly at the bound expands in full")
		assert.False(t, v.exhausted, "expanding to the bound is not exceeding it")
	})

	t.Run("merge chain past the depth bound stops at the bound", func(t *testing.T) {
		t.Parallel()
		v := newNodeView()
		assert.Empty(t, v.mappingPairs(mergeChain(maxMergeDepth+2)),
			"a merge chain longer than the bound never reaches the leaf pair")
		assert.True(t, v.exhausted, "exceeding the bound is recorded for refCycles")
	})
}

// trailingKeyNode returns a mapping with one well-formed pair followed by a
// dangling key with no value, the shape mappingPairs must ignore instead of
// reading past the end of Content.
func trailingKeyNode() *yaml.Node {
	n := ymap(yscalar("a"), yscalar("1"))
	n.Content = append(n.Content, yscalar("dangling"))
	return n
}

// mergeSeqReuseNode returns a merge sequence whose two sources are the exact
// same mapping node, pinning that revisiting one node twice within a single
// merge expansion is read twice, not mistaken for a cycle.
func mergeSeqReuseNode() *yaml.Node {
	base := ymap(yscalar("a"), yscalar("1"))
	return ymap(ymerge(), yseq(yalias(base), yalias(base)))
}

// selfReferentialMergeNode returns a mapping whose sole `<<` merge aliases
// itself.
func selfReferentialMergeNode() *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	n.Content = []*yaml.Node{ymerge(), yalias(n)}
	return n
}

// mergeChain builds a chain of `levels` mappings, each merging the next, ending
// in a mapping carrying the single pair leaf: v. Expanding the head therefore
// yields that one pair exactly when the whole chain is followed.
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

// pairMap collapses an expansion to key→scalar-value for order-insensitive
// assertions.
func pairMap(pairs []yamlPair) map[string]string {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		out[p.key] = p.val.Value
	}
	return out
}

// TestIsMergeKey_MatchesResolver pins isMergeKey against the test speakeasy's
// yml.IsMergeKey applies, since speakeasy is what reads the document the scan is
// protecting: a scalar `<<` whose resolved tag is !!merge, and nothing else. A
// quoted '<<' resolves to !!str, an alias key is not a scalar, and a key tagged
// as anything else is an ordinary key — expanding any of them would invent pairs
// speakeasy never sees and refuse a document that parses cleanly.
func TestIsMergeKey_MatchesResolver(t *testing.T) {
	t.Parallel()
	tagged := func(tag string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: "<<", Tag: tag}
	}
	tests := []struct {
		name string
		in   *yaml.Node
		want bool
	}{
		{"resolved merge tag", tagged(mergeTag), true},
		{"quoted string tag", tagged("!!str"), false},
		{"untagged scalar", yscalar("<<"), false},
		{"non-specific tag", tagged("!"), false},
		{"long-form merge tag", tagged("tag:yaml.org,2002:merge"), false},
		{"other value", yscalar("$ref"), false},
		{"alias key", yalias(ymerge()), false},
		{"mapping key", ymap(), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isMergeKey(tc.in))
		})
	}
}

// TestIsMergeKey_AgreesWithParsedTags is the other half of that pin, and the
// reason the table above can be as strict as it is: it takes the tags from a
// real parse rather than asserting them. Every syntactic form of a merge key —
// plain, non-specifically tagged, explicitly tagged — resolves to !!merge, and
// only the quoted form does not, so no parsed document can reach the shapes the
// table rejects for their tag.
func TestIsMergeKey_AgreesWithParsedTags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, src string
		want      bool
	}{
		{"plain", "b: &b {k: v}\nA:\n  <<: *b\n", true},
		{"non-specific tag", "b: &b {k: v}\nA:\n  ! <<: *b\n", true},
		{"explicit merge tag", "b: &b {k: v}\nA:\n  !!merge <<: *b\n", true},
		{"quoted", "b: &b {k: v}\nA:\n  '<<': *b\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var root yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.src), &root))
			key := findScalar(&root, "<<")
			require.NotNil(t, key, "no '<<' scalar in the parsed tree")
			assert.Equal(t, tc.want, isMergeKey(key), "parsed tag was %q", key.Tag)
		})
	}
}

// findScalar returns the first scalar node with the given value anywhere in the
// tree, or nil when there is none.
func findScalar(n *yaml.Node, value string) *yaml.Node {
	if n.Kind == yaml.ScalarNode && n.Value == value {
		return n
	}
	for _, c := range n.Content {
		if found := findScalar(c, value); found != nil {
			return found
		}
	}
	return nil
}

// TestDetectCycles_NonMergeKeyShapesAreClean pins the resolver-fidelity half of
// the merge handling end to end: a quoted '<<' and an alias standing in for the
// key are ordinary keys, so the $ref they carry never becomes a top-level $ref
// and the document must not be refused. Treating either as a merge would flag a
// cycle that speakeasy never encounters.
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
	assert.Empty(t, detectCycles(0, []byte(quoted)),
		"a quoted '<<' is a plain key to speakeasy, not a merge")
	assert.Empty(t, detectCycles(0, []byte(aliasedKey)),
		"an alias standing in for the key is a plain key to speakeasy, not a merge")
}

// TestPureRefTarget_Cases covers every arm of the $ref read: a node with no
// pairs at all, a non-$ref key skipped on the way to one, and the three ways a
// $ref value fails to be an internal pointer.
func TestPureRefTarget_Cases(t *testing.T) {
	t.Parallel()

	t.Run("non-mapping node has no target", func(t *testing.T) {
		t.Parallel()
		_, ok := newNodeView().pureRefTarget(yscalar("x"))
		assert.False(t, ok)
	})

	tests := []struct {
		name string
		n    *yaml.Node
		want string
	}{
		{"sibling key before the ref", ymap(yscalar("type"), yscalar("object"),
			yscalar("$ref"), yscalar("#/components/schemas/A")), "#/components/schemas/A"},
		{"external ref is not internal", ymap(yscalar("$ref"), yscalar("other.yaml#/A")), ""},
		{"non-scalar ref value", ymap(yscalar("$ref"), ymap(yscalar("a"), yscalar("b"))), ""},
		{"nil ref value via broken alias", ymap(yscalar("$ref"), yalias(nil)), ""},
		{"no ref key at all", ymap(yscalar("type"), yscalar("object")), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := newNodeView().pureRefTarget(tc.n)
			assert.Equal(t, tc.want != "", ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestChildByToken_MappingResolvesThroughAliasKey pins that childByToken's
// mapping arm resolves an alias-valued key through the view, matching
// pureRefTarget's alias-key handling.
func TestChildByToken_MappingResolvesThroughAliasKey(t *testing.T) {
	t.Parallel()
	keyTarget := yscalar("k")
	val := yscalar("v")
	n := ymap(yalias(keyTarget), val)
	assert.Same(t, val, newNodeView().childByToken(n, "k"))
}

// TestChildByToken_ScalarNodeHasNoChild pins the switch's fall-through arm: a
// node that is neither a mapping nor a sequence has no child for any token.
func TestChildByToken_ScalarNodeHasNoChild(t *testing.T) {
	t.Parallel()
	assert.Nil(t, newNodeView().childByToken(yscalar("x"), "0"))
}

// TestNodeView_CachesOnlyReproducibleExpansions pins the memoization contract
// that keeps the scan from re-expanding without letting one traversal order lose
// a $ref. Two things may be cached: any complete expansion, and any expansion
// entered at the top level, which is reproducible even when a bound truncated it
// because nothing was in flight around it. What may not be cached is a truncated
// expansion produced from inside a deeper one, where how much survived depends
// on where the walk came in.
func TestNodeView_CachesOnlyReproducibleExpansions(t *testing.T) {
	t.Parallel()

	t.Run("complete expansion is cached", func(t *testing.T) {
		t.Parallel()
		base := ymap(yscalar("a"), yscalar("1"))
		n := ymap(ymerge(), yalias(base), yscalar("b"), yscalar("2"))
		v := newNodeView()
		first := v.mappingPairs(n)
		require.Contains(t, v.pairs, n, "a complete expansion is memoized")
		require.Contains(t, v.pairs, base, "so is every complete sub-expansion")
		assert.Equal(t, pairMap(first), pairMap(v.mappingPairs(n)), "the cached read matches")
	})

	// outer -> shared -> deep -> outer, all by `<<`. Reaching deep through outer
	// breaks the cycle at outer, so deep contributes only its own pair.
	// Expanding deep first instead breaks the cycle at deep, and it contributes
	// all three — so deep's truncated value must not be cached by the first read.
	mergeCycle := func() (outer, shared, deep *yaml.Node) {
		outer = &yaml.Node{Kind: yaml.MappingNode}
		shared = &yaml.Node{Kind: yaml.MappingNode}
		deep = &yaml.Node{Kind: yaml.MappingNode}
		outer.Content = []*yaml.Node{yscalar("outerkey"), yscalar("o"), ymerge(), yalias(shared)}
		shared.Content = []*yaml.Node{yscalar("keep"), yscalar("v"), ymerge(), yalias(deep)}
		deep.Content = []*yaml.Node{yscalar("deepkey"), yscalar("d"), ymerge(), yalias(outer)}
		return outer, shared, deep
	}

	t.Run("a truncation reached from inside a chain is not cached", func(t *testing.T) {
		t.Parallel()
		outer, shared, deep := mergeCycle()
		v := newNodeView()
		assert.Equal(t, map[string]string{"outerkey": "o", "keep": "v", "deepkey": "d"},
			pairMap(v.mappingPairs(outer)), "the cycle is broken, not followed")
		assert.NotContains(t, v.pairs, shared, "an inner truncated expansion may not be cached")
		assert.NotContains(t, v.pairs, deep, "nor the one below it")

		assert.Equal(t, map[string]string{"deepkey": "d", "outerkey": "o", "keep": "v"},
			pairMap(v.mappingPairs(deep)),
			"entering at deep breaks the cycle elsewhere and sees more pairs than the first read gave it")
	})

	t.Run("a top-level truncation is cached and stable", func(t *testing.T) {
		t.Parallel()
		outer, _, _ := mergeCycle()
		v := newNodeView()
		first := pairMap(v.mappingPairs(outer))
		require.Contains(t, v.pairs, outer,
			"the entry point's own expansion does not depend on any caller")
		assert.Equal(t, first, pairMap(v.mappingPairs(outer)), "so re-reading it agrees")
	})

	t.Run("nothing is cached while another expansion is in flight", func(t *testing.T) {
		t.Parallel()
		outer, _, _ := mergeCycle()
		v := newNodeView()
		other := ymap(yscalar("k"), yscalar("v"))
		v.inFlight[other] = true // as if this read came from inside other's

		assert.NotEmpty(t, v.mappingPairs(outer), "the read still answers")
		assert.NotContains(t, v.pairs, outer,
			"but a truncation under an in-flight expansion is not reproducible, so it is not kept")
	})
}

// TestRefScanCollect_VisitsEachNodeOncePerRole pins the walk's memoization keys.
// One anchored pure-$ref mapping is reached twice in the same schema role (two
// allOf entries) and once in each of the other two schema roles. It must be
// collected exactly once — the same-role repeat is deduplicated — while each
// distinct role is still entered, which is what cycle_alias_dual_position.yaml
// exercises end to end.
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

// TestDetectCycles_TruncationDoesNotDisableTheRestOfTheScan pins the property
// that decides whether the merge-depth bound is safe: it applies to the one
// chain that exceeds it, not to the document. A contagious bound would let
// anyone disable the crash protection for a spec by prefixing an over-deep merge
// chain to it, so the cycle in this document — declared after the chain, and
// reached only after the truncation has already happened — must still be caught,
// and caught as the error rather than reported as the warning.
func TestDetectCycles_TruncationDoesNotDisableTheRestOfTheScan(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-anchors:\n")
	b.WriteString("  m0: &m0 {type: object}\n")
	for i := 1; i <= maxMergeDepth+10; i++ {
		fmt.Fprintf(&b, "  m%d: &m%d {<<: *m%d, p%d: %d}\n", i, i, i-1, i, i)
	}
	b.WriteString("components:\n  schemas:\n")
	fmt.Fprintf(&b, "    Deep: {properties: {x: *m%d}}\n", maxMergeDepth+10)
	b.WriteString("    A: {$ref: '#/components/schemas/B'}\n")
	b.WriteString("    B: {$ref: '#/components/schemas/A'}\n")

	diags := detectCycles(0, []byte(b.String()))
	require.NotEmpty(t, diags)
	assert.Equal(t, diag.CyclicRef, diags[0].Code,
		"a cycle outside the truncated chain is still found, and outranks the warning")
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
}

// TestNodeView_TruncationIsPerNode is the unit-level statement of the same rule:
// a view that had to stop on one chain still expands an unrelated mapping in
// full, so exhausted records what happened without changing what the view does
// next.
func TestNodeView_TruncationIsPerNode(t *testing.T) {
	t.Parallel()
	v := newNodeView()
	require.Empty(t, v.mappingPairs(mergeChain(maxMergeDepth+2)))
	require.True(t, v.exhausted)

	other := ymap(yscalar("$ref"), yscalar("#/components/schemas/A"))
	assert.Equal(t, map[string]string{"$ref": "#/components/schemas/A"},
		pairMap(v.mappingPairs(other)), "an unrelated mapping still expands in full")
	assert.Equal(t, map[string]string{"leaf": "v"},
		pairMap(v.mappingPairs(mergeChain(maxMergeDepth))),
		"so does a chain that fits inside the bound")
}

// TestNodeView_MemoizeRespectsPairBudget pins the cache's memory bound. The
// cache is what stops a merge chain from re-expanding on every read, but it
// retains a full pair list per mapping, so without a ceiling a pathological
// document turns bounded work into unbounded heap. Declining to cache is always
// safe — the expansion is recomputed identically — which is what lets the budget
// be a hard limit rather than a heuristic.
func TestNodeView_MemoizeRespectsPairBudget(t *testing.T) {
	t.Parallel()
	pairs := []yamlPair{{key: "a", val: yscalar("1")}, {key: "b", val: yscalar("2")}}

	t.Run("within budget: retained and counted", func(t *testing.T) {
		t.Parallel()
		v := newNodeView()
		n := ymap()
		v.memoize(n, pairs)
		assert.Contains(t, v.pairs, n)
		assert.Equal(t, len(pairs), v.cachedPairs)
	})

	t.Run("past budget: dropped, count unchanged", func(t *testing.T) {
		t.Parallel()
		v := newNodeView()
		v.cachedPairs = maxCachedPairs - 1
		n := ymap()
		v.memoize(n, pairs)
		assert.NotContains(t, v.pairs, n, "an entry that would overrun the budget is not kept")
		assert.Equal(t, maxCachedPairs-1, v.cachedPairs, "and does not count against it")
	})

	t.Run("a dropped entry still reads correctly", func(t *testing.T) {
		t.Parallel()
		base := ymap(yscalar("a"), yscalar("1"))
		n := ymap(ymerge(), yalias(base), yscalar("b"), yscalar("2"))
		v := newNodeView()
		v.cachedPairs = maxCachedPairs
		want := map[string]string{"a": "1", "b": "2"}
		assert.Equal(t, want, pairMap(v.mappingPairs(n)), "an uncached read is a full read")
		assert.Equal(t, want, pairMap(v.mappingPairs(n)), "and repeats identically")
		assert.Empty(t, v.pairs, "nothing was retained")
	})
}

// TestRefScanCollect_UnhandledRolePanics pins the walk's completeness guard. The
// visited sets are an array sized by roleCount so a new role cannot be added
// without one, and this is the matching guarantee for the dispatch: a role with
// no case must fail loudly rather than be walked as whichever kind of node the
// switch happens to fall through to. recoverCycleScan is what keeps that a
// warning rather than a crash, so the two halves are asserted together.
func TestRefScanCollect_UnhandledRolePanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() {
		s := newRefScan()
		s.stack = append(s.stack, refTask{n: ymap(), role: roleCount})
		s.collect(nil)
	}, "a task carrying an unhandled role is a programmer error")
}

// TestRefScanCollect_DeepNestingIsNotTruncated pins the reason the collection
// walk is iterative. A recursive walk needs a depth cap, and a depth cap plus
// per-node memoization silently drops refs: a node first reached below the cap
// has its descent truncated and is then skipped when a shallow path reaches it
// again. This document nests a $ref far deeper than maxCycleDepth, so a capped
// walk would never collect it and the cycle would reach speakeasy uncaught.
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

// TestDetectCycles_ChainedAliasFanOutIsRefusedFast and the merge-chain tests
// below pin the scan's time bound against documents whose node count grows
// linearly but whose naive expansion doesn't. Chained aliases (&a1 {type:
// string}, &a2 {allOf: [*a1, *a1]}, ...) fan a schema walk out exponentially
// unless the walk memoizes; a merge chain (&m1 {a: 1}, &m2 {<<: *m1, b: 2},
// ...) re-materializes every pair at each level. Either would turn the crash
// this scan prevents into a hang.
//
// This document must stay linear to scan and still be refused: it used to be
// pinned clean, which was a live hole — the same shape blows past 3 GiB inside
// soa.Unmarshal at 18 levels, and this one runs 40.
//
// The scan runs on a goroutine and the test fails on timeout rather than
// blocking on the package's own timeout; a regression leaks that goroutine for
// the test binary's lifetime, which is the accepted trade for a fast, legible
// failure.
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

// mergeChainSpec builds a document whose anchors form a merge chain `levels`
// deep and in which every level is also a schema, so the walk expands each of
// them rather than only the head. Schemas are emitted deepest-first, which is
// the order that defeats a cache filled bottom-up: the first expansion is the
// longest one and every level below it is still cold.
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

// TestDetectCycles_MergeChainWithinBoundIsClean pins the ordinary case: a merge
// chain the view can model in full expands, caches, and reports nothing. It runs
// at the depth bound exactly, so it is also the boundary control for the test
// below — one level shallower than the shape that starts truncating.
func TestDetectCycles_MergeChainWithinBoundIsClean(t *testing.T) {
	t.Parallel()
	diags := scanWithin(t, mergeChainSpec(maxMergeDepth), "blowup on an in-bound merge chain")
	assert.Empty(t, diags, "a merge chain the scan can expand in full is clean")
}

// TestDetectCycles_MergeChainPastBoundStaysFastAndWarns guards the cost of
// truncation: past the depth bound each level re-expands on arrival, and stays
// affordable only because a re-expansion costs O(maxMergeDepth²) rather than
// scaling with chain length. Without that bound the descent runs away — this
// document took ~29s. Do not lower 1600: it's sized so a regression in the
// merge-expansion bound blows the 10s budget this test enforces.
//
// The scan must also report its own incompleteness rather than claim a clean
// document is protected. That warning is now joined by an amplification
// refusal — this document expands ~19,227 raw nodes to ~10.3 million, past
// either bound. TestCompile_MergeChainPastBoundStillCompiles pins the
// shallower level (200) that must stay accepted.
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

// TestCompile_MergeChainPastBoundStillCompiles is the other half of that
// contract, end to end: the document is legal — speakeasy expands merge chains
// of any depth — so the scan reporting its own incompleteness must not cost the
// user their compile.
func TestCompile_MergeChainPastBoundStillCompiles(t *testing.T) {
	t.Parallel()
	doc, diags, err := New().Compile(t.Context(),
		[]compilers.Source{{Path: "deep-merge.yaml", Data: []byte(mergeChainSpec(200))}},
		compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc, "a legal document is still compiled")
	assertHasCode(t, diags, diag.CycleScanFailed, ir.SeverityWarning)
	for _, d := range diags {
		assert.NotEqual(t, ir.SeverityError, d.Severity, "no diagnostic refuses the source")
	}
}

// scanWithin runs detectCycles on src, requires it to finish inside the bound,
// and returns its diagnostics for the caller to assert on. See the note on
// goroutine lifetime above.
func scanWithin(t *testing.T, src, blowup string) []ir.Diagnostic {
	t.Helper()
	const bound = 10 * time.Second
	done := make(chan []ir.Diagnostic, 1)
	go func() {
		done <- detectCycles(0, []byte(src))
	}()
	select {
	case diags := <-done:
		return diags
	case <-time.After(bound):
		t.Fatalf("detectCycles did not return within %v — likely %s", bound, blowup)
		return nil
	}
}

// FuzzCycleDetector is the standing regression guard for GitHub #12: no
// input, however degenerate, may crash the process with a fatal stack
// overflow. The contract is process survival, not a particular verdict —
// Compile may return a document, diagnostics, or an error, but must never
// fault.
//
// Unlike FuzzCompile in fuzz_test.go, which asserts structural oracles on
// cleanly-compiled specs, this target seeds the known degenerate
// reproducers, the legal ref-shaped controls, and the parser-panic inputs,
// so a plain `go test` replays them and a detector regression that lets a
// cycle reach the parser faults here. Mutating from those seeds is also the
// surest way to surface a new crashing shape after a speakeasy bump changes
// which reference positions the resolver recurses through:
//
//	go test -run x -fuzz FuzzCycleDetector ./compilers/openapi
func FuzzCycleDetector(f *testing.F) {
	for _, tc := range cycleReproducers {
		if data, err := os.ReadFile("../../testdata/openapi/" + tc.file + ".yaml"); err == nil {
			f.Add(data)
		}
	}
	for _, tc := range refShapedDataSpecs {
		f.Add([]byte(tc.data))
	}
	if data, err := os.ReadFile("../../testdata/openapi/amplification_alias_bomb.yaml"); err == nil {
		f.Add(data) // the GitHub #27 reproducer: refused for amplification, not a cycle
	}
	f.Add([]byte(" "))         // whitespace-only: recoverable parser panic
	f.Add([]byte("\t\x00: [")) // undecodable: reported as a parse error, not a fault

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = New().Compile(t.Context(),
			[]compilers.Source{{Path: "fuzz.yaml", Data: data}}, compilers.Options{})
	})
}
