package scan

import (
	"encoding/json/jsontext"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/sourceindex"
	"github.com/dexpace/morphic/ir"
)

// schemasHeader opens every schemas document below. It is five lines, so the
// first member is on firstMemberLine.
const schemasHeader = "openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n"

const firstMemberLine = 6

// end is a member with no $ref, where a chain terminates.
const end = "    End: {type: string}\n"

// schemasDoc is a schemas document declaring members in order.
func schemasDoc(members []string) []byte {
	return []byte(schemasHeader + strings.Join(members, ""))
}

// chainTo is n members prefix0 … prefix(n-1), each a $ref to the next and the
// last to then.
func chainTo(prefix string, n int, then string) []string {
	members := make([]string, n)
	for i := range n {
		next := prefix + strconv.Itoa(i+1)
		if i == n-1 {
			next = then
		}
		members[i] = fmt.Sprintf("    %s%d: {$ref: '#/components/schemas/%s'}\n", prefix, i, next)
	}
	return members
}

// loopOf is n members C0 … C(n-1), each a $ref to the next and the last back
// to the first.
func loopOf(n int) []string {
	return chainTo("C", n, "C0")
}

// memberLine is the line of the member named name in members.
func memberLine(t *testing.T, members []string, name string) int {
	t.Helper()
	i := slices.IndexFunc(members, func(m string) bool { return strings.HasPrefix(m, "    "+name+":") })
	require.NotEqual(t, -1, i, "no member is named %s", name)
	return firstMemberLine + i
}

// TestCycles_ChainIsJudgedWhole pins that a chain's verdict is the whole
// chain's, however long: a loop of any length is refused, since the resolver
// recurses once per hop of one (GitHub #922), and a chain that ends is not.
// Each document is declared as written and reversed, and the report falls on
// the loop's member with the least pointer in both: never on a tail leading
// into the loop, and never on whichever member comes first (GitHub #912).
func TestCycles_ChainIsJudgedWhole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		members []string
		want    string // the member reported, or "" for none
	}{
		{"self-loop", loopOf(1), "C0"},
		{"loop-of-two", loopOf(2), "C0"},
		{"loop-of-10001", loopOf(10001), "C0"},
		{"loop-of-20000", loopOf(20000), "C0"},
		{"short-tail-into-a-long-loop", slices.Concat(chainTo("S", 3, "C0"), loopOf(10001)), "C0"},
		{"long-tail-into-a-short-loop", slices.Concat(chainTo("S", 15000, "C0"), loopOf(2)), "C0"},
		{"tail-sorting-before-its-loop", slices.Concat(chainTo("A", 3, "C0"), loopOf(2)), "C0"},
		{"long-chain-that-ends", slices.Concat(chainTo("S", 15000, "End"), []string{end}), ""},
		{
			"long-chain-that-ends-beside-a-loop",
			slices.Concat(chainTo("S", 15000, "End"), []string{end}, loopOf(2)), "C0",
		},
	}
	for _, tc := range tests {
		for _, reversed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reversed=%t", tc.name, reversed), func(t *testing.T) {
				t.Parallel()
				members := slices.Clone(tc.members)
				if reversed {
					slices.Reverse(members)
				}

				diags := scanBytes(t, schemasDoc(members))

				if tc.want == "" {
					assert.Empty(t, diags, "a chain that ends is no cycle however long it is")
					return
				}
				require.Len(t, diags, 1)
				assert.Equal(t, diag.CyclicRef, diags[0].Code)
				assert.Equal(t, ir.SeverityError, diags[0].Severity)
				assert.Equal(t, memberLine(t, members, tc.want), diags[0].Provenance.Position.Line)
			})
		}
	}
}

// settled collects the references of a document and settles their chains.
func settled(t *testing.T, data []byte) (*refScan, *chainGraph) {
	t.Helper()
	root := indexOf(t, data).Root()
	s := newRefScan()
	s.collect(root)
	return s, settleChains(s.view, root, s.out, sourceindex.MaxIndexedNodes)
}

// viaRefDoc is the shape that walked a chain once per start: n schemas in a
// chain whose last hop reads its target through a reference, via, declared as
// written or reversed.
func viaRefDoc(n int, reversed bool) []byte {
	members := chainTo("S", n-1, "S"+strconv.Itoa(n-1))
	members = append(members,
		fmt.Sprintf("    S%d: {$ref: '#/components/schemas/via/t'}\n", n-1),
		"    via: {$ref: '#/components/schemas/nowhere', t: {type: string}}\n")
	if reversed {
		slices.Reverse(members)
	}
	return schemasDoc(members)
}

// fanInDoc is n schemas whose hops all pass through one reference, via, whose
// own chain is n schemas long.
func fanInDoc(n int) []byte {
	members := make([]string, 0, 2*n+2)
	for i := range n {
		members = append(members, fmt.Sprintf("    F%d: {$ref: '#/components/schemas/via/x'}\n", i))
	}
	members = append(members, "    via: {$ref: '#/components/schemas/S0', x: {type: string}}\n")
	return schemasDoc(slices.Concat(members, chainTo("S", n, "End"), []string{end}))
}

// throughLoopDoc is n schemas in a chain whose last hop reads its target
// through the first, landing on a member t of it that leads back to the last.
// Every start runs into that loop of two.
func throughLoopDoc(n int) []byte {
	var b strings.Builder
	b.WriteString(schemasHeader)
	last := "S" + strconv.Itoa(n-1)
	fmt.Fprintf(&b, "    S0: {$ref: '#/components/schemas/S1', t: {$ref: '#/components/schemas/%s'}}\n", last)
	for i := 1; i < n-1; i++ {
		fmt.Fprintf(&b, "    S%d: {$ref: '#/components/schemas/S%d'}\n", i, i+1)
	}
	fmt.Fprintf(&b, "    %s: {$ref: '#/components/schemas/S0/t'}\n", last)
	return []byte(b.String())
}

// TestSettleChains_ReadsEachHopOnce counts work rather than timing it. Each
// reference's hop is read once and each node settled once, however long the
// chains are, however they end and in whichever order they are declared: the
// shapes that once walked a chain again from every start (GitHub #903, #923).
func TestSettleChains_ReadsEachHopOnce(t *testing.T) {
	t.Parallel()
	const n = 20000
	tests := []struct {
		name   string
		data   []byte
		refs   int
		cycles bool
	}{
		{"chain", schemasDoc(slices.Concat(chainTo("S", n, "End"), []string{end})), n, false},
		{"loop", schemasDoc(loopOf(n)), n, true},
		{"tail-into-a-loop", schemasDoc(slices.Concat(chainTo("S", n, "C0"), loopOf(2))), n + 2, true},
		{"through-a-ref", viaRefDoc(n, false), n + 1, false},
		{"through-a-ref-reversed", viaRefDoc(n, true), n + 1, false},
		{"fan-in-through-a-ref", fanInDoc(n), 2*n + 1, false},
		{"through-a-ref-into-a-loop", throughLoopDoc(n), n + 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, chains := settled(t, tc.data)

			require.Len(t, chains.order, tc.refs, "every reference is read, once")
			for _, start := range s.out {
				require.Equal(t, tc.cycles, chains.loopOf(start) != 0)
			}
			assert.LessOrEqual(t, chains.work, 2*tc.refs, "settling is linear in the references")
		})
	}
}

// TestCycles_ALoopOutsideAnySchemaIsLeftToTheResolver pins that a chain of
// reference objects is never judged, however long its loop: the resolver
// follows one in a loop and reports it, so only a schema chain is read.
func TestCycles_ALoopOutsideAnySchemaIsLeftToTheResolver(t *testing.T) {
	t.Parallel()
	const n = 10001
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n")
	for i := range n {
		fmt.Fprintf(&b, "  /p%d: {$ref: '#/paths/~1p%d'}\n", i, (i+1)%n)
	}
	data := []byte(b.String())

	assert.Empty(t, scanBytes(t, data), "no schema chain cycles")
	_, chains := settled(t, data)
	assert.Empty(t, chains.order, "and no reference outside a schema is read")
}

// TestCycles_AHopThroughAReferenceLandsWhereItsPointerEnds is GitHub #932. A
// pointer passing a schema $ref reads that schema's own members, as the
// resolver reads a schema, so the hop lands on the member it names and the
// chain goes on from there; one that comes back is a loop the resolver recurses
// round until the stack runs out. Each loop is reported at its member with the
// least pointer, in both orders. The controls land on a member with no $ref,
// or on nothing.
func TestCycles_AHopThroughAReferenceLandsWhereItsPointerEnds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		members []string
		want    jsontext.Pointer // the site reported, or "" for none
	}{
		{"through-itself-to-a-member-back", []string{
			"    A: {$ref: '#/components/schemas/A/properties/p', properties: {p: {$ref: '#/components/schemas/A'}}}\n",
		}, "/components/schemas/A"},
		{"through-itself-to-an-unknown-key-back", []string{
			"    A: {$ref: '#/components/schemas/A/x', x: {$ref: '#/components/schemas/A'}}\n",
		}, "/components/schemas/A"},
		{"through-itself-to-a-member-into-another", []string{
			"    A: {$ref: '#/components/schemas/A/properties/p', properties: {p: {$ref: '#/components/schemas/B'}}}\n",
			"    B: {$ref: '#/components/schemas/A'}\n",
		}, "/components/schemas/A"},
		{"through-a-member-passing-itself", []string{
			"    A: {$ref: '#/components/schemas/B/u'}\n",
			"    B: {u: {$ref: '#/components/schemas/B/u/v', v: {$ref: '#/components/schemas/A'}}}\n",
		}, "/components/schemas/A"},
		{"through-a-chain-of-members", []string{
			"    S0: {$ref: '#/components/schemas/S1/u'}\n",
			"    S1: {$ref: '#/components/schemas/S1/u', u: {$ref: '#/components/schemas/S1/u/u/u', " +
				"u: {$ref: '#/components/schemas/S1/u', u: {$ref: '#/components/schemas/S0'}}}}\n",
		}, "/components/schemas/S0"},
		{"through-itself-to-a-member-that-ends", []string{
			"    A: {$ref: '#/components/schemas/A/properties/p', properties: {p: {type: string}}}\n",
		}, ""},
		{"through-itself-to-a-scalar", []string{
			"    A: {$ref: '#/components/schemas/A/description', description: d}\n",
		}, ""},
		{"through-itself-to-nothing", []string{
			"    A: {$ref: '#/components/schemas/A/t'}\n",
		}, ""},
		{"through-another-to-a-member-that-ends", []string{
			"    A: {$ref: '#/components/schemas/B/u'}\n",
			"    B: {$ref: '#/components/schemas/A', u: {type: string}}\n",
		}, ""},
	}
	for _, tc := range tests {
		for order, data := range bothOrders(schemasHeader, tc.members) {
			t.Run(tc.name+"/"+order, func(t *testing.T) {
				t.Parallel()
				if tc.want == "" {
					assert.Empty(t, scanPlaced(t, data), "the chain ends")
					return
				}
				d := onlyRefusal(t, data)
				assert.Equal(t, diag.CyclicRef, d.Code)
				assert.Equal(t, tc.want, d.Provenance.Pointer)
			})
		}
	}
}

// TestCycles_EveryStartRunsIntoTheLoopItLandsOn pins that a hop is read once
// for every chain through it: from S0, the last hop passes S0 on its way to t,
// and from any later start too, and every one of them runs into the loop of t
// and S19. That loop's member with the least pointer, t, is reported.
func TestCycles_EveryStartRunsIntoTheLoopItLandsOn(t *testing.T) {
	t.Parallel()
	s, chains := settled(t, throughLoopDoc(20))
	for _, start := range s.out {
		require.NotZero(t, chains.loopOf(start), "the chain from line %d", start.Line)
	}

	diags := scanPlaced(t, throughLoopDoc(20))

	require.Len(t, diags, 1)
	assert.Equal(t, diag.CyclicRef, diags[0].Code)
	assert.Equal(t, jsontext.Pointer("/components/schemas/S0/t"), diags[0].Provenance.Pointer)
}

// walkFrom is what settleChains answers for start: a walk one hop at a time,
// remembering nothing between starts. It stops at the end, or at the first
// node it revisits, and then returns the nodes from that one round to it.
func walkFrom(view *nodeview.View, root, start *yaml.Node) []*yaml.Node {
	at := map[*yaml.Node]int{}
	var chain []*yaml.Node
	for cur := start; len(chain) <= sourceindex.MaxIndexedNodes; {
		if i, revisit := at[cur]; revisit {
			return chain[i:]
		}
		ref, ok := view.PureRefTarget(cur)
		if !ok {
			return nil
		}
		at[cur] = len(chain)
		chain = append(chain, cur)
		next, landed := view.PointerTarget(root, ref)
		if !landed {
			return nil
		}
		cur = next
	}
	panic("walkFrom: a chain longer than the tree")
}

// randomRefDoc is a small document of schemas, path items and parameters
// whose members are $refs to each other, to members of each other read through
// a $ref, or to nothing, and carry $refs of their own beside them.
func randomRefDoc(r *rand.Rand) []byte {
	sections := []struct{ name, prefix string }{{"schemas", "S"}, {"pathItems", "P"}, {"parameters", "Q"}}
	counts := []int{1 + r.IntN(6), r.IntN(5), r.IntN(5)}
	paths := r.IntN(5)
	targets := []string{"#/components/schemas/missing", "#/components", "#"}
	for k, sec := range sections {
		for i := range counts[k] {
			at := fmt.Sprintf("#/components/%s/%s%d", sec.name, sec.prefix, i)
			targets = append(targets, at, at, at+"/t", at+"/t/t")
		}
	}
	for i := range paths {
		targets = append(targets, fmt.Sprintf("#/paths/~1x%d", i), fmt.Sprintf("#/paths/~1x%d/t", i))
	}
	member := func() string {
		target, sibling := targets[r.IntN(len(targets))], targets[r.IntN(len(targets))]
		switch r.IntN(8) {
		case 0:
			return "{type: string}"
		case 1, 2:
			return fmt.Sprintf("{$ref: '%s', t: {$ref: '%s'}}", target, sibling)
		case 3:
			return fmt.Sprintf("{$ref: '%s', t: {type: string}}", target)
		default:
			return fmt.Sprintf("{$ref: '%s'}", target)
		}
	}
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:")
	if paths == 0 {
		b.WriteString(" {}")
	}
	for i := range paths {
		fmt.Fprintf(&b, "\n  /x%d: %s", i, member())
	}
	b.WriteString("\ncomponents:\n")
	for k, sec := range sections {
		if counts[k] > 0 {
			fmt.Fprintf(&b, "  %s:\n", sec.name)
		}
		for i := range counts[k] {
			fmt.Fprintf(&b, "    %s%d: %s\n", sec.prefix, i, member())
		}
	}
	return []byte(b.String())
}

// TestSettleChains_AgreesWithAWalkFromEachNode holds every settled chain, and
// the loop it runs into, to walkFrom, over the cycle reproducers and thousands
// of generated documents. The kinds of chain it must meet are counted, so a
// generator that stopped reaching one would fail here rather than pass on less.
func TestSettleChains_AgreesWithAWalkFromEachNode(t *testing.T) {
	t.Parallel()
	const generated = 4000
	docs := make([][]byte, 0, len(cycleReproducers)+generated)
	for _, tc := range cycleReproducers {
		docs = append(docs, readReproducer(t, tc.file))
	}
	r := rand.New(rand.NewPCG(903, 932))
	for range generated {
		docs = append(docs, randomRefDoc(r))
	}
	met := map[string]int{}
	for _, data := range docs {
		s, chains := settled(t, data)
		pointers := Pointers(chains.root)
		for _, n := range chains.order {
			want := walkFrom(s.view, chains.root, n)
			var got []*yaml.Node
			if loop := chains.loopOf(n); loop != 0 {
				got = chains.loops[loop-1]
			}
			require.ElementsMatch(t, want, got, "the loop the chain from line %d runs into, in\n%s", n.Line, data)
			met[chainKind(chains, pointers, n)]++
		}
	}
	for _, kind := range []string{"on a loop", "into a loop", "ends", "through itself"} {
		assert.Positive(t, met[kind], "no chain settled as %q", kind)
	}
}

// chainKind names how the chain from n settled, for
// TestSettleChains_AgreesWithAWalkFromEachNode: on a loop, into one, or ending,
// unless n's own hop lands beneath n, passing n on the way.
func chainKind(chains *chainGraph, pointers func(*yaml.Node) (jsontext.Pointer, bool), n *yaml.Node) string {
	h := chains.hops[n]
	at, _ := pointers(n)
	if lands, _ := pointers(h.next); h.next != nil && at.Contains(lands) && lands != at {
		return "through itself"
	}
	switch {
	case h.loop == 0:
		return "ends"
	case slices.Contains(chains.loops[h.loop-1], n):
		return "on a loop"
	default:
		return "into a loop"
	}
}

// TestSettleChains_PanicsPastItsNodeBound pins the bound at its edge: a chain
// of twenty references reads twenty, so a bound of 20 lets it finish and one of
// 19 stops it with the panic recoverCycleScan turns into a warning.
func TestSettleChains_PanicsPastItsNodeBound(t *testing.T) {
	t.Parallel()
	root := indexOf(t, schemasDoc(slices.Concat(chainTo("S", 20, "End"), []string{end}))).Root()
	settle := func(bound int) func() {
		s := newRefScan()
		s.collect(root)
		return func() { settleChains(s.view, root, s.out, bound) }
	}

	assert.NotPanics(t, settle(20))
	assert.PanicsWithValue(t, "cycle scan: $ref chains outgrew their 19-node bound", settle(19))
}

// TestSettleChains_APointerThroughItsOwnNodeReadsWhereItLands pins that a hop
// passing its own node is read on to where it lands: A's pointer passes A on
// its way to A/t, whose own $ref names nothing, so both are read and the chain
// ends there.
func TestSettleChains_APointerThroughItsOwnNodeReadsWhereItLands(t *testing.T) {
	t.Parallel()
	_, chains := settled(t, schemasDoc([]string{
		"    A: {$ref: '#/components/schemas/A/t', t: {$ref: '#/components/schemas/missing'}}\n",
	}))

	require.Len(t, chains.order, 2, "A, and the member it lands on")
	a, landed := chains.order[0], chains.order[1]
	assert.Same(t, landed, chains.hops[a].next)
	assert.Nil(t, chains.hops[landed].next, "a pointer naming nothing lands nowhere")
	assert.Zero(t, chains.loopOf(a))
}
