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
	"github.com/dexpace/morphic/ir"
)

// cycleReproducers are documents from GitHub #12 and GitHub #26 whose schema
// graph cycles never reach a node without a top-level $ref. Each crashed the
// process with a fatal, unrecoverable stack overflow before the pre-parse
// detector (and, for the #26 shapes, its alias/merge/contentSchema awareness)
// was added. The sibling variants carry a concrete `type` alongside the $ref:
// speakeasy follows the top-level $ref regardless of the sibling, so they
// crash exactly like the bare-$ref forms and must be diagnosed the same way.
//
// The #26 shapes exercise the gap between the scan's raw yaml.Node model and
// the decoder's resolved view of the same document: an alias standing in for
// the $ref value (alias-ref-value), a $ref nested under a JSONSchema-typed
// keyword the old key set omitted (content-schema), an alias standing in for
// the literal `$ref` key itself (alias-ref-key), a `<<` merge key that
// contributes a $ref the decoder sees but no literal key does (merge-key-ref),
// an alias standing in for the whole schema node (alias-schema-node), and one
// anchored pure-$ref node reused in two different schema positions
// (alias-dual-position: once as a "properties" value and once as a schema in
// its own right) — a follow-up to the alias-schema-node fix that exposed a
// second bug, the ref-collection walk sharing one visited-node set across
// walkSchema/walkSchemaMap/walkSchemaList let the first role to reach the
// node consume it and the second role skip it, silently dropping the $ref the
// chain walk needed.
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
			assert.Equal(t, codeCyclicRef, diags[0].Code)
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
			assertHasErrorCode(t, diags, codeCyclicRef)
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
				assert.NotEqualf(t, codeCyclicRef, d.Code, "must not refuse as a cyclic ref: %+v", d)
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

func assertHasErrorCode(t *testing.T, diags []ir.Diagnostic, code string) {
	t.Helper()
	for _, d := range diags {
		if d.Code == code && d.Severity == ir.SeverityError {
			return
		}
	}
	t.Fatalf("expected an error diagnostic with code %q, got %+v", code, diags)
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

// TestRecoverCycleScan_PanicYieldsWarning pins the bounded-recursion backstop: a
// scan that panics must degrade to a single non-fatal warning that keeps the
// failure observable, never propagate the panic and never silently vanish.
func TestRecoverCycleScan_PanicYieldsWarning(t *testing.T) {
	t.Parallel()
	got := recoverCycleScan(3, func() []ir.Diagnostic {
		panic("detector bug")
	})
	require.Len(t, got, 1, "a panicking scan degrades to one diagnostic")
	assert.Equal(t, codeCycleScanFailed, got[0].Code)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity, "the scan failure must not refuse a spec")
	assert.Equal(t, 3, got[0].Provenance.Source, "the diagnostic carries the source index")
}

// TestRecoverCycleScan_PassesThroughResult pins that a scan that does not panic
// returns its diagnostics unchanged.
func TestRecoverCycleScan_PassesThroughResult(t *testing.T) {
	t.Parallel()
	want := []ir.Diagnostic{{Code: codeCyclicRef, Severity: ir.SeverityError}}
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

// TestWalkOutsideSchema_NilNode pins that a nil node terminates the outside-schema
// walk without collecting refs.
func TestWalkOutsideSchema_NilNode(t *testing.T) {
	t.Parallel()
	var out []*yaml.Node
	s := newRefScan(&out)
	s.walkOutsideSchema(nil, 0)
	assert.Empty(t, out)
}

// newRefScan builds a refScan with fresh, empty visited-node sets, for tests
// that drive the ref-collection walk's methods directly.
func newRefScan(out *[]*yaml.Node) *refScan {
	return &refScan{out: out, outsideSeen: map[*yaml.Node]bool{}, schemaSeen: map[*yaml.Node]bool{}}
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
	assert.False(t, followRefChain(root, nodes[0], map[*yaml.Node]bool{}),
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

	assert.True(t, followRefChain(root, a, map[*yaml.Node]bool{}),
		"A -> B -> A is cyclic with an empty memo")

	safe := map[*yaml.Node]bool{b: true}
	assert.False(t, followRefChain(root, a, safe),
		"a chain reaching a memoized-safe node is not a cycle")
	assert.True(t, safe[a], "the walk records the reaching node as terminating too")
}

// TestFollowRefChain_DanglingRefIsNotCycle pins that a $ref whose target does not
// resolve ends the chain as terminating rather than cyclic: the missing target is
// reported downstream, and the node is recorded safe so a later chain reusing it
// short-circuits.
func TestFollowRefChain_DanglingRefIsNotCycle(t *testing.T) {
	t.Parallel()
	a := ymap(yscalar("$ref"), yscalar("#/schemas/Missing"))
	root := ymap(yscalar("schemas"), ymap(yscalar("A"), a))
	safe := map[*yaml.Node]bool{}
	assert.False(t, followRefChain(root, a, safe), "a dangling $ref is not a cycle")
	assert.True(t, safe[a], "the dangling node is recorded terminating")
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

// TestMappingPairs_Cases covers mappingPairs/collectMappingPairs/
// mergeSourcePairs on shapes a real parse cannot produce (a non-scalar key,
// a merge sequence) alongside the ones it can (alias key, alias value,
// single-mapping merge, duplicate explicit key).
func TestMappingPairs_Cases(t *testing.T) {
	t.Parallel()

	t.Run("nil node yields no pairs", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, mappingPairs(nil))
	})

	t.Run("non-mapping node yields no pairs", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, mappingPairs(yscalar("x")))
	})

	t.Run("alias key and alias value are dereferenced", func(t *testing.T) {
		t.Parallel()
		keyTarget := yscalar("k")
		valTarget := yscalar("v")
		n := ymap(yalias(keyTarget), yalias(valTarget))
		got := mappingPairs(n)
		require.Len(t, got, 1)
		assert.Equal(t, "k", got[0].key)
		assert.Same(t, valTarget, got[0].val)
	})

	t.Run("non-scalar key after deref is skipped", func(t *testing.T) {
		t.Parallel()
		n := ymap(
			ymap(yscalar("x"), yscalar("1")), yscalar("ignored"),
			yscalar("real"), yscalar("kept"),
		)
		got := mappingPairs(n)
		require.Len(t, got, 1)
		assert.Equal(t, "real", got[0].key)
	})

	t.Run("duplicate explicit key: first wins", func(t *testing.T) {
		t.Parallel()
		first := yscalar("first")
		n := ymap(yscalar("k"), first, yscalar("k"), yscalar("second"))
		got := mappingPairs(n)
		require.Len(t, got, 1)
		assert.Same(t, first, got[0].val)
	})

	t.Run("merge key contributes a mapping's pairs", func(t *testing.T) {
		t.Parallel()
		base := ymap(yscalar("a"), yscalar("1"))
		n := ymap(yscalar("<<"), yalias(base), yscalar("b"), yscalar("2"))
		got := mappingPairs(n)
		keys := make(map[string]string, len(got))
		for _, p := range got {
			keys[p.key] = p.val.Value
		}
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, keys)
	})

	t.Run("explicit key wins over merged key", func(t *testing.T) {
		t.Parallel()
		base := ymap(yscalar("a"), yscalar("from-merge"))
		n := ymap(yscalar("a"), yscalar("explicit"), yscalar("<<"), base)
		got := mappingPairs(n)
		require.Len(t, got, 1)
		assert.Equal(t, "explicit", got[0].val.Value)
	})

	t.Run("merge sequence: earlier source wins on a shared key", func(t *testing.T) {
		t.Parallel()
		first := ymap(yscalar("a"), yscalar("from-first"))
		second := ymap(yscalar("a"), yscalar("from-second"), yscalar("b"), yscalar("only-in-second"))
		n := ymap(yscalar("<<"), yseq(first, second))
		got := mappingPairs(n)
		keys := make(map[string]string, len(got))
		for _, p := range got {
			keys[p.key] = p.val.Value
		}
		assert.Equal(t, map[string]string{"a": "from-first", "b": "only-in-second"}, keys)
	})

	t.Run("self-referential merge is bounded, not infinite", func(t *testing.T) {
		t.Parallel()
		n := &yaml.Node{Kind: yaml.MappingNode}
		n.Content = []*yaml.Node{yscalar("<<"), yalias(n)}
		assert.Empty(t, mappingPairs(n), "a merge that references its own mapping contributes nothing")
	})

	t.Run("merge chain longer than the depth cap stops at the cap", func(t *testing.T) {
		t.Parallel()
		const n = maxCycleDepth + 2
		nodes := make([]*yaml.Node, n)
		for i := range nodes {
			nodes[i] = &yaml.Node{Kind: yaml.MappingNode}
		}
		for i := 0; i < n-1; i++ {
			nodes[i].Content = []*yaml.Node{yscalar("<<"), yalias(nodes[i+1])}
		}
		nodes[n-1].Content = []*yaml.Node{yscalar("leaf"), yscalar("v")}
		assert.Empty(t, mappingPairs(nodes[0]),
			"a merge chain longer than the depth cap never reaches the leaf pair")
	})
}

// TestPureRefTarget_NonMappingNode pins that a non-mapping node (a shape
// mappingPairs cannot turn into pairs) reports no $ref target.
func TestPureRefTarget_NonMappingNode(t *testing.T) {
	t.Parallel()
	_, ok := pureRefTarget(yscalar("x"))
	assert.False(t, ok)
}

// TestChildByToken_MappingResolvesThroughAliasKey pins that childByToken's
// mapping arm resolves an alias-valued key via mappingPairs, matching
// pureRefTarget's alias-key handling.
func TestChildByToken_MappingResolvesThroughAliasKey(t *testing.T) {
	t.Parallel()
	keyTarget := yscalar("k")
	val := yscalar("v")
	n := ymap(yalias(keyTarget), val)
	assert.Same(t, val, childByToken(n, "k"))
}

// TestDetectCycles_ChainedAliasFanOutStaysLinear pins the walk's linear-time
// bound against a "billion laughs" style chained-alias document: each level
// aliases the previous level twice inside allOf, so an unmemoized schema walk
// would explore an exponential number of paths even though the node count
// grows only linearly. Without refScan's visited-node sets this document
// would hang rather than crash — the availability regression the sets guard
// against (deref'ing an alias edge, added for GitHub #26, is what first makes
// the walk cross alias edges at all). The goroutine/timeout wraps the call so
// a regression fails this test instead of hanging the whole suite.
func TestDetectCycles_ChainedAliasFanOutStaysLinear(t *testing.T) {
	t.Parallel()
	const levels = 40

	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\nx-anchors:\n")
	b.WriteString("  a0: &a0 {type: string}\n")
	for i := 1; i <= levels; i++ {
		fmt.Fprintf(&b, "  a%d: &a%d {allOf: [*a%d, *a%d]}\n", i, i, i-1, i-1)
	}
	fmt.Fprintf(&b, "components:\n  schemas:\n    Root: *a%d\n", levels)

	done := make(chan []ir.Diagnostic, 1)
	go func() {
		done <- detectCycles(0, []byte(b.String()))
	}()
	select {
	case diags := <-done:
		assert.Empty(t, diags, "deep alias reuse without a $ref cycle is not a degenerate cycle")
	case <-time.After(10 * time.Second):
		t.Fatal("detectCycles did not return within the bound — likely exponential blowup on chained aliases")
	}
}

// TestHasErrorDiag_Cases pins the severity gate the load phase relies on: only an
// error-severity diagnostic signals a refusal; empty and warning-only sets do not.
// FuzzCycleDetector is the standing regression guard for GitHub #12: no input,
// however degenerate, may crash the process with a fatal stack overflow. The
// contract is process survival, not a particular verdict — Compile may return a
// document, diagnostics, or a Go error, but must never fault.
//
// It is distinct from the corpus-seeded FuzzCompile in fuzz_test.go, which asserts
// structural oracles on cleanly-compiled specs: this target instead seeds the
// known degenerate reproducers, the legal ref-shaped controls, and the
// parser-panic inputs, so a plain `go test` replays them and a detector regression
// that lets a cycle reach the parser faults here. Mutating from those cycle shapes
// is also the surest way to surface a new crashing shape after a speakeasy bump
// changes which reference positions the resolver recurses through:
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
	f.Add([]byte(" "))         // whitespace-only: recoverable parser panic
	f.Add([]byte("\t\x00: [")) // undecodable: reported as a parse error, not a fault

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = New().Compile(t.Context(),
			[]compilers.Source{{Path: "fuzz.yaml", Data: data}}, compilers.Options{})
	})
}
