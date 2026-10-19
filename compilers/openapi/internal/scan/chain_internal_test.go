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
	return s, settleChains(s.view, root, slices.Concat(s.out, s.outside), sourceindex.MaxIndexedNodes)
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
// own chain is n schemas long: asking of each hop whether via's chain reaches
// it, by walking that chain, would be quadratic.
func fanInDoc(n int) []byte {
	members := make([]string, 0, 2*n+2)
	for i := range n {
		members = append(members, fmt.Sprintf("    F%d: {$ref: '#/components/schemas/via/x'}\n", i))
	}
	members = append(members, "    via: {$ref: '#/components/schemas/S0', x: {type: string}}\n")
	return schemasDoc(slices.Concat(members, chainTo("S", n, "End"), []string{end}))
}

// parameterChainDoc is n component parameters, each a $ref to the next, ending
// in a loop of two that never leaves components, which outsideCycle leaves to
// the resolver.
func parameterChainDoc(n int) []byte {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  parameters:\n")
	for i := range n + 2 {
		next := i + 1
		if i == n+1 {
			next = n
		}
		fmt.Fprintf(&b, "    P%d: {$ref: '#/components/parameters/P%d'}\n", i, next)
	}
	return []byte(b.String())
}

// TestSettleChains_ReadsEachHopOnce counts work rather than timing it. Each
// reference's hop is read once, and settling costs a step per node and per
// pointer passing through one, however long the chains are, however they end
// and in whichever order they are declared: the shapes that once walked a chain
// again from every start (GitHub #903, #923, #924), and one that a naive
// question about passing pointers would make quadratic.
func TestSettleChains_ReadsEachHopOnce(t *testing.T) {
	t.Parallel()
	const n = 20000
	tests := []struct {
		name string
		data []byte
		refs int
		want chainVerdict
	}{
		{"chain", schemasDoc(slices.Concat(chainTo("S", n, "End"), []string{end})), n, chainTerminates},
		{"loop", schemasDoc(loopOf(n)), n, chainCycles},
		{"tail-into-a-loop", schemasDoc(slices.Concat(chainTo("S", n, "C0"), loopOf(2))), n + 2, chainCycles},
		{"through-a-ref", viaRefDoc(n, false), n + 1, chainTerminates},
		{"through-a-ref-reversed", viaRefDoc(n, true), n + 1, chainTerminates},
		{"fan-in-through-a-ref", fanInDoc(n), 2*n + 1, chainTerminates},
		{"parameters-into-a-loop", parameterChainDoc(n), n + 2, chainCycles},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, chains := settled(t, tc.data)

			require.Len(t, chains.order, tc.refs, "every reference is read, once")
			for _, start := range slices.Concat(s.out, s.outside) {
				require.Equal(t, tc.want, chains.outcome(start).verdict)
			}
			assert.LessOrEqual(t, chains.work, 3*tc.refs, "settling is linear in the references")
		})
	}
}

// TestOutsideRefused_LeavesALoopInsideComponents pins that the loop the sweep
// above settles once is still left to the resolver, from every start.
func TestOutsideRefused_LeavesALoopInsideComponents(t *testing.T) {
	t.Parallel()
	s, chains := settled(t, parameterChainDoc(50))

	_, found := s.refusal(InSource(0), chains.root, chains)

	assert.False(t, found, "a loop inside components is the resolver's to report")
}

// TestCycles_LongLoopOutsideComponentsIsRefused is the loop of reference
// objects spelled by document position, which the scan refuses rather than
// leaving to the resolver (see outsideRefused), at a length past the 10,000
// hops a walk once stopped at. Declared either
// way, /p0, the member with the least pointer, reports it.
func TestCycles_LongLoopOutsideComponentsIsRefused(t *testing.T) {
	t.Parallel()
	const n = 10001
	members := make([]string, n)
	for i := range n {
		members[i] = fmt.Sprintf("  /p%d: {$ref: '#/paths/~1p%d'}\n", i, (i+1)%n)
	}
	for _, reversed := range []bool{false, true} {
		t.Run(fmt.Sprintf("reversed=%t", reversed), func(t *testing.T) {
			t.Parallel()
			declared := slices.Clone(members)
			if reversed {
				slices.Reverse(declared)
			}

			diags := scanBytes(t, []byte("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths:\n"+strings.Join(declared, "")))

			require.Len(t, diags, 1)
			assert.Equal(t, diag.CyclicRef, diags[0].Code)
			assert.Equal(t, 4+slices.Index(declared, members[0]), diags[0].Provenance.Position.Line,
				"/p0, below three header lines")
		})
	}
}

// recordDoc holds two chains of reference objects, each ending in a loop. The
// first leaves components before its loop, at In; the second leaves it inside
// its loop, at E.
const recordDoc = `openapi: 3.1.0
info: {title: t, version: '1'}
paths:
  /in: {$ref: '#/components/pathItems/In'}
  /out: {$ref: '#/components/pathItems/B'}
  /q: {$ref: '#/components/pathItems/D'}
components:
  pathItems:
    In: {$ref: '#/paths/~1out'}
    B: {$ref: '#/components/pathItems/C'}
    C: {$ref: '#/components/pathItems/B'}
    D: {$ref: '#/components/pathItems/E'}
    E: {$ref: '#/paths/~1q'}
    F: {$ref: '#/components/pathItems/D'}
`

// TestSettleChains_LeftComponentsCoversTheWholeChain pins which nodes' chains
// leave components: a node's does if its own hop does or any hop after it,
// which on a loop is every hop of the loop.
func TestSettleChains_LeftComponentsCoversTheWholeChain(t *testing.T) {
	t.Parallel()
	want := map[jsontext.Pointer]chainOutcome{
		"/paths/~1in":              {chainCycles, true},
		"/components/pathItems/In": {chainCycles, true},
		"/paths/~1out":             {chainCycles, false},
		"/components/pathItems/B":  {chainCycles, false},
		"/components/pathItems/C":  {chainCycles, false},
		"/components/pathItems/F":  {chainCycles, true},
		"/components/pathItems/D":  {chainCycles, true},
		"/components/pathItems/E":  {chainCycles, true},
		"/paths/~1q":               {chainCycles, true},
	}
	s, chains := settled(t, []byte(recordDoc))
	root := chains.root

	for p, w := range want {
		path, complete := s.view.PointerPath(root, p)
		require.True(t, complete, "%s names nothing", p)
		assert.Equal(t, w, chains.outcome(path[len(path)-1]), "the chain from %s", p)
	}
}

// reentryDoc is a chain S0…S(n-1) whose last hop reads its target through S0,
// at a member t referring back to S(n-1). From S0 that hop re-enters the chain;
// from any later start it passes S0 as an ordinary reference and lands on a
// loop of t and S(n-1).
func reentryDoc(n int) []byte {
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

// TestCycles_ReentryIsTheStartsOwn pins that a re-entry is judged for each
// start apart. S0's walk re-enters, which refCycles does not report for a
// schema; S1's passes S0 as an ordinary reference, meets the loop of S0's t and
// S19 and is refused, at t, the loop's member with the least pointer. Handing S1
// the re-entry S0 met would report nothing.
func TestCycles_ReentryIsTheStartsOwn(t *testing.T) {
	t.Parallel()

	diags := scanPlaced(t, reentryDoc(20))

	require.Len(t, diags, 1)
	assert.Equal(t, diag.CyclicRef, diags[0].Code)
	assert.Equal(t, jsontext.Pointer("/components/schemas/S0/t"), diags[0].Provenance.Pointer)
}

// walkFrom is what settleChains answers for start: a walk one hop at a time,
// remembering nothing between starts. It stops at the first node it revisits,
// the first pointer passing through a node it followed, or the end, and says
// what it failed on: the nodes from the one revisited round to it, or the
// first node along the pointer that passed through one followed.
func walkFrom(view *nodeview.View, root, start *yaml.Node) (chainOutcome, []*yaml.Node) {
	at := map[*yaml.Node]int{}
	var chain []*yaml.Node
	var left []bool
	for cur := start; len(left) <= sourceindex.MaxIndexedNodes; {
		if i, revisit := at[cur]; revisit {
			return chainOutcome{verdict: chainCycles, leftComponents: slices.Contains(left, true)}, chain[i:]
		}
		ref, ok := view.PureRefTarget(cur)
		if !ok {
			return chainOutcome{verdict: chainTerminates}, nil
		}
		at[cur] = len(left)
		chain = append(chain, cur)
		left = append(left, !componentsRef(ref))
		path, complete := view.PointerPath(root, ref)
		through := path
		if complete {
			through = path[:len(path)-1]
		}
		if i := slices.IndexFunc(through, func(n *yaml.Node) bool { _, on := at[n]; return on }); i >= 0 {
			return chainOutcome{verdict: chainReenters}, through[i : i+1]
		}
		if !complete {
			return chainOutcome{verdict: chainTerminates}, nil
		}
		cur = path[len(path)-1]
	}
	panic("walkFrom: a chain longer than the tree")
}

// randomRefDoc is a small document of schemas, path items and parameters
// whose members are $refs to each other, to members of each other read through
// a $ref, or to nothing, and carry $refs of their own beside them.
func randomRefDoc(r *rand.Rand) []byte {
	sections := []struct{ name, prefix string }{{"schemas", "S"}, {"pathItems", "P"}, {"parameters", "Q"}}
	counts := []int{r.IntN(7), r.IntN(5), r.IntN(5)}
	paths := r.IntN(5)
	targets := []string{"#/components/schemas/missing", "#/components", "#"}
	for k, sec := range sections {
		for i := range counts[k] {
			at := fmt.Sprintf("#/components/%s/%s%d", sec.name, sec.prefix, i)
			targets = append(targets, at, at, at+"/t")
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
	b.WriteString("\ncomponents:\n  x-none: {}\n") // a mapping even with no section under it
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

// TestSettleChains_AgreesWithAWalkFromEachNode holds every settled outcome,
// and what it fails on, to walkFrom, over the cycle reproducers and thousands
// of generated documents. The kinds of outcome it must meet are counted, so a
// generator that stopped reaching one would fail here rather than pass on less.
func TestSettleChains_AgreesWithAWalkFromEachNode(t *testing.T) {
	t.Parallel()
	const generated = 4000
	docs := make([][]byte, 0, len(cycleReproducers)+generated)
	for _, tc := range cycleReproducers {
		docs = append(docs, readReproducer(t, tc.file))
	}
	r := rand.New(rand.NewPCG(903, 923))
	for range generated {
		docs = append(docs, randomRefDoc(r))
	}
	verdicts := map[chainVerdict]string{chainTerminates: "ends", chainCycles: "cycles", chainReenters: "re-enters"}
	met := map[string]int{}
	for _, data := range docs {
		s, chains := settled(t, data)
		for _, n := range chains.order {
			got, h := chains.outcome(n), chains.hops[n]
			want, wantFails := walkFrom(s.view, chains.root, n)
			require.Equal(t, want, got, "the chain from line %d of\n%s", n.Line, data)
			_, fails := chains.failsOn(n)
			require.ElementsMatch(t, wantFails, fails, "what the chain from line %d fails on, in\n%s", n.Line, data)
			kind := "off a loop, " + verdicts[got.verdict]
			if h.loop != 0 {
				kind = "on a loop, " + verdicts[got.verdict]
			}
			if got.leftComponents {
				kind += ", leaving components"
			}
			met[kind]++
			if h.selfThrough {
				met["through itself"]++
			}
		}
	}
	for _, kind := range []string{
		"on a loop, cycles", "on a loop, cycles, leaving components", "on a loop, re-enters",
		"off a loop, ends", "off a loop, cycles", "off a loop, cycles, leaving components",
		"off a loop, re-enters", "through itself",
	} {
		assert.Positive(t, met[kind], "no node settled as %q", kind)
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

// TestSettleChains_ReadsNothingPastAPointerThroughItsOwnNode pins that a hop
// every walk re-enters on ends its chain: A's pointer passes through A, so no
// walk ever lands on A/t, and the node there, which no start reaches, is not
// read.
func TestSettleChains_ReadsNothingPastAPointerThroughItsOwnNode(t *testing.T) {
	t.Parallel()
	_, chains := settled(t, schemasDoc([]string{
		"    A: {$ref: '#/components/schemas/A/t', t: {$ref: '#/components/schemas/End'}}\n", end,
	}))

	require.Len(t, chains.order, 1, "only A is read")
	assert.Equal(t, chainOutcome{verdict: chainReenters}, chains.outcome(chains.order[0]))
}

// settledFrom settles a document's chains from the nodes at starts alone, in
// that order, and returns each node's outcome by pointer.
func settledFrom(t *testing.T, data []byte, starts []jsontext.Pointer, at []jsontext.Pointer) map[jsontext.Pointer]chainOutcome {
	t.Helper()
	root := indexOf(t, data).Root()
	view := nodeview.New()
	node := func(p jsontext.Pointer) *yaml.Node {
		path, complete := view.PointerPath(root, p)
		require.True(t, complete, "%s names nothing", p)
		return path[len(path)-1]
	}
	from := make([]*yaml.Node, 0, len(starts))
	for _, p := range starts {
		from = append(from, node(p))
	}
	chains := settleChains(view, root, from, sourceindex.MaxIndexedNodes)
	outcomes := make(map[jsontext.Pointer]chainOutcome, len(at))
	for _, p := range at {
		outcomes[p] = chains.outcome(node(p))
	}
	return outcomes
}

// TestSettleChains_LoopReentryCountsTheTurn pins a loop of L0, L1 and L0's
// member t, where L1's hop passes through L0. A walk from L0 or from t follows
// L0 before L1's hop and re-enters; one from L1 follows it only after, and
// cycles. Which node the loop is read from decides whether a re-entry is found
// before or after the turn, so each is a start in turn.
func TestSettleChains_LoopReentryCountsTheTurn(t *testing.T) {
	t.Parallel()
	data := schemasDoc([]string{
		"    L0: {$ref: '#/components/schemas/L1', t: {$ref: '#/components/schemas/L0'}}\n",
		"    L1: {$ref: '#/components/schemas/L0/t'}\n",
	})
	nodes := []jsontext.Pointer{"/components/schemas/L0", "/components/schemas/L1", "/components/schemas/L0/t"}
	want := map[jsontext.Pointer]chainOutcome{
		nodes[0]: {verdict: chainReenters},
		nodes[1]: {verdict: chainCycles},
		nodes[2]: {verdict: chainReenters},
	}
	for _, start := range nodes {
		t.Run(string(start), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, settledFrom(t, data, []jsontext.Pointer{start}, nodes))
		})
	}
}

// TestSettleChains_TailPassedOnItsLoopReenters pins a node off a loop whose
// pointer a hop on the loop passes through: T lands on the loop of L, M and
// T's member x, and M's hop passes through T. A walk from T has T on it when M's
// hop comes and re-enters; a walk from the loop never follows T, and cycles.
func TestSettleChains_TailPassedOnItsLoopReenters(t *testing.T) {
	t.Parallel()
	data := schemasDoc([]string{
		"    T: {$ref: '#/components/schemas/L', x: {$ref: '#/components/schemas/L'}}\n",
		"    L: {$ref: '#/components/schemas/M'}\n",
		"    M: {$ref: '#/components/schemas/T/x'}\n",
	})
	nodes := []jsontext.Pointer{"/components/schemas/T", "/components/schemas/L", "/components/schemas/M", "/components/schemas/T/x"}

	got := settledFrom(t, data, nodes[:1], nodes)

	assert.Equal(t, map[jsontext.Pointer]chainOutcome{
		nodes[0]: {verdict: chainReenters},
		nodes[1]: {verdict: chainCycles},
		nodes[2]: {verdict: chainCycles},
		nodes[3]: {verdict: chainCycles},
	}, got)
}

// failsFrom settles data's chains from the node at start and returns, by
// pointer, what the chain from each node at a pointer of at fails on.
func failsFrom(t *testing.T, data []byte, start jsontext.Pointer, at []jsontext.Pointer) map[jsontext.Pointer][]jsontext.Pointer {
	t.Helper()
	root := indexOf(t, data).Root()
	view := nodeview.New()
	node := func(p jsontext.Pointer) *yaml.Node {
		path, complete := view.PointerPath(root, p)
		require.True(t, complete, "%s names nothing", p)
		return path[len(path)-1]
	}
	chains := settleChains(view, root, []*yaml.Node{node(start)}, sourceindex.MaxIndexedNodes)
	pointers := Pointers(root)
	out := make(map[jsontext.Pointer][]jsontext.Pointer, len(at))
	for _, p := range at {
		_, fails := chains.failsOn(node(p))
		for _, f := range fails {
			fp, _ := pointers(f)
			out[p] = append(out[p], fp)
		}
	}
	return out
}

// TestSettleChains_AReentryFollowsThePointersOrder pins which of two nodes one
// hop passes again is the one re-entered: the one its pointer reaches first,
// where the resolver reaches it sooner. T's member k is E, which W aliases; T
// lands on it there, and E's pointer passes T and then E itself. A walk from E
// re-enters E; one from T meets T first along that same pointer.
func TestSettleChains_AReentryFollowsThePointersOrder(t *testing.T) {
	t.Parallel()
	data := schemasDoc([]string{
		"    T: {$ref: '#/components/schemas/W', k: &e {$ref: '#/components/schemas/T/k/z', z: {type: string}}}\n",
		"    W: *e\n",
	})
	got := failsFrom(t, data, "/components/schemas/T", []jsontext.Pointer{"/components/schemas/T", "/components/schemas/T/k"})

	assert.Equal(t, map[jsontext.Pointer][]jsontext.Pointer{
		"/components/schemas/T":   {"/components/schemas/T"},
		"/components/schemas/T/k": {"/components/schemas/T/k"},
	}, got)
}

// TestSettleChains_ATailPassedOnItsLoopIsCountedRoundFromWhereItLands pins how
// far round its loop a hop passing a tail comes. N lands on R, of a loop of R,
// A, N's x, B, R's y and C; A's hop passes N one hop after R, and B's passes R
// three hops after it. Counting N's own hop as hop 0, a walk from N passes N
// at hop 2, before R's re-entry at hop 4, so N is what it re-enters.
func TestSettleChains_ATailPassedOnItsLoopIsCountedRoundFromWhereItLands(t *testing.T) {
	t.Parallel()
	data := schemasDoc([]string{
		"    N: {$ref: '#/components/schemas/R', x: {$ref: '#/components/schemas/B'}}\n",
		"    R: {$ref: '#/components/schemas/A', y: {$ref: '#/components/schemas/C'}}\n",
		"    A: {$ref: '#/components/schemas/N/x'}\n",
		"    B: {$ref: '#/components/schemas/R/y'}\n",
		"    C: {$ref: '#/components/schemas/R'}\n",
	})
	got := failsFrom(t, data, "/components/schemas/N", []jsontext.Pointer{"/components/schemas/N", "/components/schemas/R"})

	assert.Equal(t, map[jsontext.Pointer][]jsontext.Pointer{
		"/components/schemas/N": {"/components/schemas/N"},
		"/components/schemas/R": {"/components/schemas/R"},
	}, got)
}

// TestSettleChains_ALoopReentryFollowsThePointersOrder is the test above on a
// loop: A, B's member k, lands on B through an alias, B on C, C's pointer passes
// B and then A to land on A's z, and z on A. A walk from A meets both at C's hop,
// B first along the pointer, though A comes first on the loop.
func TestSettleChains_ALoopReentryFollowsThePointersOrder(t *testing.T) {
	t.Parallel()
	data := schemasDoc([]string{
		"    B: &b {$ref: '#/components/schemas/C', k: &a {$ref: '#/components/schemas/WB', z: {$ref: '#/components/schemas/WA'}}}\n",
		"    C: {$ref: '#/components/schemas/B/k/z'}\n",
		"    WB: *b\n",
		"    WA: *a\n",
	})
	got := failsFrom(t, data, "/components/schemas/B/k", []jsontext.Pointer{"/components/schemas/B/k"})

	assert.Equal(t, map[jsontext.Pointer][]jsontext.Pointer{"/components/schemas/B/k": {"/components/schemas/B"}}, got)
}

// TestSettleChains_ALoopPassAfterTheLoopsOwnReentryComesTooLate is the test
// above with the pass further round: N lands on R, of a loop of R, X, B, R's y,
// A, N's x and C, where B's hop passes R two hops after it and A's passes N
// four after. Counting N's own hop as hop 0, a walk from N re-enters R at hop
// 3, before A's would pass N at hop 5.
func TestSettleChains_ALoopPassAfterTheLoopsOwnReentryComesTooLate(t *testing.T) {
	t.Parallel()
	data := schemasDoc([]string{
		"    N: {$ref: '#/components/schemas/R', x: {$ref: '#/components/schemas/C'}}\n",
		"    R: {$ref: '#/components/schemas/X', y: {$ref: '#/components/schemas/A'}}\n",
		"    X: {$ref: '#/components/schemas/B'}\n",
		"    B: {$ref: '#/components/schemas/R/y'}\n",
		"    A: {$ref: '#/components/schemas/N/x'}\n",
		"    C: {$ref: '#/components/schemas/R'}\n",
	})
	got := failsFrom(t, data, "/components/schemas/N", []jsontext.Pointer{"/components/schemas/N"})

	assert.Equal(t, map[jsontext.Pointer][]jsontext.Pointer{"/components/schemas/N": {"/components/schemas/R"}}, got)
}

// TestSettleChains_ATailIsReenteredAtItsEarliestPass pins that of two passes
// through a tail N, the one earlier on N's walk counts, whichever was read
// first. Counting N's own hop as hop 0, P's hop passes N at hop 1 and T's at
// hop 6, around Q's re-entry at hop 4. Every schema is a start, so the
// declaration order decides which pass is read first, and either way round N
// is re-entered.
func TestSettleChains_ATailIsReenteredAtItsEarliestPass(t *testing.T) {
	t.Parallel()
	members := []string{
		"    N: {$ref: '#/components/schemas/P', k: {$ref: '#/components/schemas/Q'}, z: {type: string}}\n",
		"    P: {$ref: '#/components/schemas/N/k'}\n",
		"    Q: {$ref: '#/components/schemas/S', t: {$ref: '#/components/schemas/T'}}\n",
		"    S: {$ref: '#/components/schemas/Q/t'}\n",
		"    T: {$ref: '#/components/schemas/N/z'}\n",
	}
	reversed := slices.Clone(members)
	slices.Reverse(reversed)
	for _, order := range [][]string{members, reversed} {
		s, chains := settled(t, schemasDoc(order))
		n := nodesAt(t, chains.root, []jsontext.Pointer{"/components/schemas/N"})[0]
		require.Len(t, s.out, len(members), "every schema is a start")
		_, fails := chains.failsOn(n)
		assert.Equal(t, []*yaml.Node{n}, fails, "in the order %q", order)
	}
}

// TestSettleChains_ATailYieldsToAReentryEarlierOnOnePointer pins the path
// order at a tail: T, E's member k, lands on E through an alias, and E's
// pointer passes E and then T. A walk from T meets E first along it.
func TestSettleChains_ATailYieldsToAReentryEarlierOnOnePointer(t *testing.T) {
	t.Parallel()
	data := schemasDoc([]string{
		"    E: &e {$ref: '#/components/schemas/E/k/z', k: {$ref: '#/components/schemas/WE', z: {type: string}}}\n",
		"    WE: *e\n",
	})
	got := failsFrom(t, data, "/components/schemas/E/k", []jsontext.Pointer{"/components/schemas/E/k", "/components/schemas/E"})

	assert.Equal(t, map[jsontext.Pointer][]jsontext.Pointer{
		"/components/schemas/E/k": {"/components/schemas/E"},
		"/components/schemas/E":   {"/components/schemas/E"},
	}, got)
}
