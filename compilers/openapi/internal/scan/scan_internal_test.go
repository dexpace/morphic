package scan

import (
	"encoding/json/jsontext"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/speakeasy-api/openapi/jsonpointer"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
	"github.com/dexpace/morphic/ir"
)

// aliasCycleReproducers are the cycle fixtures whose loop is a YAML anchor
// naming its own ancestor, which the parser cannot decode, so Aliases refuses
// them before it is asked to.
var aliasCycleReproducers = []struct{ name, file string }{
	{"yaml-anchor", "cycle_yaml_anchor"},
}

// refCycleReproducers are the cycle fixtures whose loop is one of schema $refs,
// in every spelling the corpus holds. The compiler resolves schema $refs
// itself, so none of them is this package's to refuse: it reads aliases and
// nesting, never a $ref.
var refCycleReproducers = []struct{ name, file string }{
	{"self-ref", "cycle_self_ref"},
	{"two-node-ref", "cycle_two_node_ref"},
	{"self-ref-sibling", "cycle_self_ref_sibling"},
	{"two-node-ref-sibling", "cycle_two_node_ref_sibling"},
	{"alias-ref-value", "cycle_alias_ref_value"},
	{"content-schema", "cycle_content_schema"},
	{"alias-ref-key", "cycle_alias_ref_key"},
	{"merge-key-ref", "cycle_merge_key_ref"},
	{"alias-schema-node", "cycle_alias_schema_node"},
	{"alias-dual-position", "cycle_alias_dual_position"},
	{"duplicate-key", "cycle_duplicate_key"},
	{"pointer-whitespace-self", "cycle_pointer_whitespace_self"},
	{"pointer-plus-self", "cycle_pointer_plus_self"},
	{"schema-through-own-member", "cycle_schema_through_own_member"},
	{"schema-through-member-chain", "cycle_schema_through_member_chain"},
	{"schema-empty-key", "cycle_schema_empty_key"},
	{"schema-empty-token-stays", "cycle_schema_empty_token_stays"},
	{"schema-default-response", "cycle_schema_default_response"},
}

// referenceObjectCycles are chains of reference objects outside any schema
// that cycle, or whose pointer passes back through a reference on the chain.
// The resolver follows such a chain in a loop and reports it
// (docs/parser-library.md#reference-object-cycle), so the scan leaves every one
// to it (GitHub #574).
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

// TestCycleReproducers_EveryFixtureIsExercised holds the three fixture tables
// above to the fixtures on disk. A cycle_*.yaml added to the corpus and left out
// of all is scanned by nothing here, and the suite stays green while the corpus
// grows past it.
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

	all := slices.Concat(aliasCycleReproducers, refCycleReproducers, referenceObjectCycles)
	listed := make(map[string]bool, len(all))
	for _, tc := range all {
		require.False(t, listed[tc.file+".yaml"], "%s is listed twice", tc.file)
		listed[tc.file+".yaml"] = true
	}
	for _, path := range onDisk {
		assert.True(t, listed[filepath.Base(path)],
			"%s is in %s but in none of the tables", filepath.Base(path), reproducerDir)
	}
	assert.Len(t, listed, len(onDisk), "the tables and %s hold different numbers of fixtures", reproducerDir)
}

// TestAliases_RefusesARecursiveAnchor pins that a fixture whose loop is a YAML
// anchor naming its own ancestor is refused as one cycle, with a position.
func TestAliases_RefusesARecursiveAnchor(t *testing.T) {
	t.Parallel()
	for _, tc := range aliasCycleReproducers {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := readReproducer(t, tc.file)
			diags := scanBytes(t, data)
			require.Len(t, diags, 1, "a recursive anchor is diagnosed once")
			assert.Equal(t, diag.CyclicRef, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.NotZero(t, diags[0].Provenance.Position, "position provenance")
		})
	}
}

// TestAliases_LeavesASchemaRefCycleToTheBinder pins that no loop of schema
// $refs is refused here, in any spelling the corpus holds. The compiler
// resolves schema $refs itself and reports a loop once, at its least member,
// while the document still compiles. A scan that read $refs would refuse a
// document the compile admits.
func TestAliases_LeavesASchemaRefCycleToTheBinder(t *testing.T) {
	t.Parallel()
	for _, tc := range refCycleReproducers {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, scanBytes(t, readReproducer(t, tc.file)))
		})
	}
}

// TestAliases_ReferenceObjectCyclesAreLeftToTheResolver pins that the scan
// reports nothing on a chain of reference objects, however it fails.
func TestAliases_ReferenceObjectCyclesAreLeftToTheResolver(t *testing.T) {
	t.Parallel()
	for _, tc := range referenceObjectCycles {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, scanBytes(t, readReproducer(t, tc.file)))
		})
	}
}

// TestAliases_LegalRecursionClean pins that a schema recursing through $refs,
// which is legal, is not a cycle to the scan.
func TestAliases_LegalRecursionClean(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../../testdata/conformance/openapi/recursive.yaml")
	require.NoError(t, err)
	assert.Empty(t, scanBytes(t, data), "legal recursion is not a degenerate cycle")
}

// legalAliasSpecs are documents that use anchors, aliases and merge keys the
// way a real spec does, none of them a cycle. They are the negative controls to
// the refusals above: a scan that refused every document using an alias would
// pass every refusal case.
var legalAliasSpecs = []struct {
	name string
	data string
}{
	{"anchor-reuse", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {s: &s {type: object}}
components:
  schemas:
    A: *s
    B: {type: object, properties: {p: *s}}
`},
	{"merge-key", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {base: &base {description: base schema}}
components:
  schemas:
    A: {<<: *base, type: object}
`},
	{"alias-as-a-ref-value", `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
x-anchors: {ref: &r '#/components/schemas/B'}
components:
  schemas:
    A: {$ref: *r}
    B: {type: object}
`},
}

// TestAliases_LegalAliasAndMergeUseIsClean pins that ordinary anchor reuse, a
// merge key and an alias standing for a scalar are not refused.
func TestAliases_LegalAliasAndMergeUseIsClean(t *testing.T) {
	t.Parallel()
	for _, tc := range legalAliasSpecs {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, scanBytes(t, []byte(tc.data)), "a legal use of aliases is not a degenerate cycle")
		})
	}
}

// TestAliases_EmptyDocumentIsNoCycle covers the index a source with nothing
// in it produces. A source that does not decode at all no longer reaches here —
// load refuses it as a parse error before it indexes anything — so the empty
// document is the only sourceless index the scan can be handed.
func TestAliases_EmptyDocumentIsNoCycle(t *testing.T) {
	t.Parallel()
	assert.Empty(t, Aliases(t.Context(), InSource(0), sourceindex.Build(t.Context(), nil, sourceindex.MaxIndexedNodes), budget.AtMost(defaultSurplus)))
	assert.Empty(t, scanBytes(t, nil))
}

// defaultSurplus is the alias budget a compile gets when its caller sets none,
// openapi.DefaultMaxAliasSurplus, which this package cannot import. The fixtures
// here that straddle it were sized against it; the compiler's own tests pin
// that the default reaching the scan is this one.
const defaultSurplus = 1 << 18

// scanBytes decodes source bytes and runs the refusals over the index built from
// them — what load does around Aliases, with the compile's one decode — under
// the default alias budget.
func scanBytes(t *testing.T, data []byte) []ir.Diagnostic {
	t.Helper()
	return Aliases(t.Context(), InSource(0), indexOf(t, data), budget.AtMost(defaultSurplus))
}

// scanPlaced is scanBytes with each diagnostic placed by its pointer too, as
// load places them, for a test that names a site by pointer.
func scanPlaced(t *testing.T, data []byte) []ir.Diagnostic {
	t.Helper()
	idx := indexOf(t, data)
	return Aliases(t.Context(), InTree(0, Pointers(t.Context(), idx.Root())), idx, budget.AtMost(defaultSurplus))
}

// indexOf decodes source bytes and indexes the tree. A fixture that does not
// decode never reaches the scan in a real compile — load returns a parse error
// first — so a decode failure here is a broken fixture, not a case to scan.
func indexOf(t *testing.T, data []byte) sourceindex.Index {
	t.Helper()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(data, &root), "the fixture must decode")
	return sourceindex.Build(t.Context(), &root, sourceindex.MaxIndexedNodes)
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

func TestScanRecovered_PanicYieldsWarning(t *testing.T) {
	t.Parallel()
	got := scanRecovered(InSource(3), func() []ir.Diagnostic {
		panic("detector bug")
	})
	require.Len(t, got, 1, "a panicking scan degrades to one diagnostic")
	assert.Equal(t, diag.CycleScanFailed, got[0].Code)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity, "the scan failure must not refuse a spec")
	assert.Equal(t, 3, got[0].Provenance.Source, "the diagnostic carries the source index")
	assert.Equal(t, "alias pre-scan aborted (detector bug); alias-cycle protection is incomplete for this source",
		got[0].Message, "the warning says what is left unprotected")
}

// TestScanRecovered_QuotesAPanicOnOneBoundedLine pins that the warning
// quotes a fault as diag.Catch does, whatever the scan panicked with.
func TestScanRecovered_QuotesAPanicOnOneBoundedLine(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("the detector faulted here\n", 1<<10)

	got := scanRecovered(InSource(0), func() []ir.Diagnostic { panic(huge) })

	require.Len(t, got, 1)
	assert.Equal(t, "alias pre-scan aborted ("+diag.OneLine(errors.New(huge))+
		"); alias-cycle protection is incomplete for this source", got[0].Message)
	assert.NotContains(t, got[0].Message, "\n")
	assert.Less(t, len(got[0].Message), len(huge)/4, "the bound cut the value")
}

func TestScanRecovered_PassesThroughResult(t *testing.T) {
	t.Parallel()
	want := []ir.Diagnostic{{Code: diag.CyclicRef, Severity: ir.SeverityError}}
	got := scanRecovered(InSource(0), func() []ir.Diagnostic {
		return want
	})
	assert.Equal(t, want, got)
}

func TestAliases_WhitespaceOnlyIsNoCycle(t *testing.T) {
	t.Parallel()
	assert.Empty(t, scanBytes(t, []byte("\n\n\n")))
	assert.Empty(t, scanBytes(t, []byte("# only a comment\n")))
}

func TestAliases_LegalAliasReuseClean(t *testing.T) {
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

// resolveFirstItem resolves the path item /a in src with the library and
// returns it with the error of the resolution, which is the cycle or the
// pointer that led nowhere: what the resolver reached is read from the item.
func resolveFirstItem(t *testing.T, src string) (*soa.ReferencedPathItem, error) {
	t.Helper()
	doc, _, err := soa.Unmarshal(t.Context(), strings.NewReader(src))
	require.NoError(t, err)
	item, ok := doc.Paths.Get("/a")
	require.True(t, ok)
	require.True(t, item.IsReference())

	_, err = item.Resolve(t.Context(), soa.ResolveOptions{RootDocument: doc, TargetLocation: "spec.yaml", DisableExternalRefs: true})
	return item, err
}

// walkEnd is how the library's resolution of a path item's $ref ended. The first
// three are told apart by the library's own errors; a step through a reference
// still being resolved fails with none of them, which is the fourth.
type walkEnd string

const (
	endReached walkEnd = "reached a node"
	endMissing walkEnd = "ran out of document"
	endRefused walkEnd = "refused the pointer"
	endBlocked walkEnd = "stopped at a reference it was resolving"
)

// resolverWalk reports how the library's resolution of the path item /a in src
// ended.
func resolverWalk(t *testing.T, src string) walkEnd {
	t.Helper()
	_, err := resolveFirstItem(t, src)
	switch {
	case err == nil:
		return endReached
	case errors.Is(err, jsonpointer.ErrNotFound):
		return endMissing
	case errors.Is(err, jsonpointer.ErrValidation), errors.Is(err, jsonpointer.ErrInvalidPath):
		return endRefused
	default:
		return endBlocked
	}
}

// TestAliases_TheResolverEndsAWalkThroughAReferenceObject pins the premise the
// scan leaves reference objects on (GitHub #574): the library ends its walk of
// a path item's pointer through the reference it is resolving, stopped there,
// as it ends one below another item for want of a key, whether or not the last
// token is UTF-8. The end column pins today's library; the scan refuses none of
// them, and the resolver reports each.
func TestAliases_TheResolverEndsAWalkThroughAReferenceObject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, ref string
		end       walkEnd
	}{
		{"through the reference being resolved", "'#/paths/~1a/t'", endBlocked},
		{"a token that is not UTF-8, through it", "'#/paths/~1a/%FF'", endBlocked},
		{"below another item", "'#/paths/~1b/t'", endMissing},
		{"a token that is not UTF-8, below another item", "'#/paths/~1b/%FF'", endMissing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n  /a: {$ref: " + tc.ref + "}\n" +
				"  /b: {get: {operationId: b, responses: {\"200\": {description: ok}}}}\n"
			assert.Equal(t, tc.end, resolverWalk(t, src), "how the library's walk of this pointer ends")
			assert.Empty(t, scanBytes(t, []byte(src)), "a chain of reference objects is the resolver's to report")
		})
	}
}

// TestAliases_AcceptsATreeWithNoCycle is the control the refusal cases need:
// without it, a suite made only of refusals would pass on a scan that refused
// everything handed to it.
func TestAliases_AcceptsATreeWithNoCycle(t *testing.T) {
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

// TestAliases_AnchorsThroughTheLocator pins that a refusal takes its provenance
// from the locator it is handed rather than from the node it found. A locator
// the caller composes over an overlay's attribution is how a refusal on a
// grafted node — one with no line and column of its own — comes to name the
// overlay (GitHub #476); a scan that read the node directly would bypass it.
func TestAliases_AnchorsThroughTheLocator(t *testing.T) {
	t.Parallel()
	for name, fixture := range map[string]string{
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

			got := Aliases(t.Context(), locate, indexOf(t, readReproducer(t, fixture)), budget.AtMost(defaultSurplus))
			require.Len(t, got, 1)
			assert.Equal(t, ir.Provenance{Source: 9, Pointer: "/somewhere/the/locator/decided"}, got[0].Provenance)
			assert.NotNil(t, seen, "the locator was handed the node the refusal is about")
		})
	}
}

// TestAliases_RefusesTheYAMLShapesAndNotTheOpenAPIOnes pins what Aliases
// refuses and what it leaves. It refuses an anchor naming its own ancestor and
// an alias expansion far past the document's size, because those are
// properties of YAML and any document carries them, an overlay included. It
// follows no $ref chain: the compiler's binder resolves a schema $ref, and the
// parser's resolver reports a loop of reference objects.
func TestAliases_RefusesTheYAMLShapesAndNotTheOpenAPIOnes(t *testing.T) {
	t.Parallel()
	aliases := func(src string) []ir.Diagnostic {
		return Aliases(t.Context(), InSource(0), indexOf(t, []byte(src)), budget.AtMost(defaultSurplus))
	}

	anchor := aliases("a: &x [*x]\n")
	require.Len(t, anchor, 1)
	assert.Equal(t, diag.CyclicRef, anchor[0].Code, "a recursive anchor expands without end")

	bomb := aliases(string(readReproducer(t, "amplification_alias_bomb")))
	require.Len(t, bomb, 1)
	assert.Equal(t, diag.AliasAmplification, bomb[0].Code, "an alias bomb expands far past its size")

	refCycle := string(readReproducer(t, "cycle_self_ref"))
	assert.Empty(t, aliases(refCycle), "a $ref cycle is the binder's to report, not a YAML shape")

	assert.Empty(t, aliases("a: &x {p: 1}\nb: *x\nc: *x\n"), "ordinary anchor reuse is not a refusal")
}

// TestAliases_DegradesAScanFaultToAWarning pins that Aliases runs under
// scanRecovered: a fault in the walk is reported as an incomplete scan,
// never raised through a caller that has not yet refused anything.
//
// The locator faults only when asked about a node, which is what the walk asks
// it; asked about no node, as the barrier's own report does, it answers. A
// locator that faulted on both would fault in that report too, and that tests
// a locator this compiler does not have rather than the barrier it does.
func TestAliases_DegradesAScanFaultToAWarning(t *testing.T) {
	t.Parallel()
	panicking := func(n *yaml.Node) ir.Provenance {
		if n != nil {
			panic("locator fault")
		}
		return ir.Provenance{Source: 0}
	}

	got := Aliases(t.Context(), panicking, indexOf(t, []byte("a: &x [*x]\n")), budget.AtMost(defaultSurplus))

	require.Len(t, got, 1)
	assert.Equal(t, diag.CycleScanFailed, got[0].Code)
	assert.Equal(t, ir.SeverityWarning, got[0].Severity)
	assert.Equal(t, "alias pre-scan aborted (locator fault); alias-cycle protection is incomplete for this source",
		got[0].Message)
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

				got := InTree(3, Pointers(t.Context(), root(doc)))(node)

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
			got := InTree(3, Pointers(t.Context(), tt.root(t)))(tt.node(t))

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
	steps, complete := stepsIn(t.Context(), root, nodes)
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

	steps, complete := stepsIn(t.Context(), root, 3)
	assert.False(t, complete, "five nodes do not fit a limit of three")
	assert.Len(t, steps, 3)
	_, indexed := steps[second]
	assert.False(t, indexed, "the walk enters nothing past its limit")
	got := []jsontext.Pointer{pointerOf(steps, root), pointerOf(steps, second)}
	assert.Equal(t, []jsontext.Pointer{"", ""}, got)

	steps, complete = stepsIn(t.Context(), root, 5)
	assert.True(t, complete, "five nodes fit a limit of five")
	assert.Equal(t, jsontext.Pointer("/b"), pointerOf(steps, second))

	empty := &yaml.Node{Kind: yaml.DocumentNode}
	steps, complete = stepsIn(t.Context(), empty, 1)
	assert.True(t, complete)
	assert.Equal(t, map[*yaml.Node]step{empty: {}}, steps)
	assert.Equal(t, ir.Provenance{Source: 0, Position: ir.Position{Line: 1, Column: 1}},
		InTree(0, Pointers(t.Context(), empty))(&yaml.Node{Kind: yaml.ScalarNode, Line: 1, Column: 1}),
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
	locate := InTree(0, Pointers(t.Context(), root))
	got := locate(deep)
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(root)

	// Compared as a bool, so a failure reports the lengths rather than printing
	// two pointers of tens of kilobytes each.
	same := got.Pointer == want
	assert.True(t, same, "the pointer asked for is %d bytes, want %d", len(got.Pointer), len(want))
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
	pointer := Pointers(t.Context(), root)

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
	assert.Equal(t, answer{"", false}, ask(Pointers(t.Context(), nil), value))
}
