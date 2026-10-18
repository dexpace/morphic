package scan

import (
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ynode"
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
// the first member declared whose chain cycles, as it does for a short loop:
// the scan's site rule, which GitHub #912 would move onto the loop itself.
func TestCycles_ChainIsJudgedWhole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		members []string
		// want and wantReversed name the member reported in each order, or "".
		want, wantReversed string
	}{
		{"self-loop", loopOf(1), "C0", "C0"},
		{"loop-of-two", loopOf(2), "C0", "C1"},
		{"loop-of-10001", loopOf(10001), "C0", "C10000"},
		{"loop-of-20000", loopOf(20000), "C0", "C19999"},
		{"short-tail-into-a-long-loop", slices.Concat(chainTo("S", 3, "C0"), loopOf(10001)), "S0", "C10000"},
		{"long-tail-into-a-short-loop", slices.Concat(chainTo("S", 15000, "C0"), loopOf(2)), "S0", "C1"},
		{"long-chain-that-ends", slices.Concat(chainTo("S", 15000, "End"), []string{end}), "", ""},
		{
			"long-chain-that-ends-beside-a-loop",
			slices.Concat(chainTo("S", 15000, "End"), []string{end}, loopOf(2)), "C0", "C1",
		},
	}
	for _, tc := range tests {
		for _, reversed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reversed=%t", tc.name, reversed), func(t *testing.T) {
				t.Parallel()
				members, want := slices.Clone(tc.members), tc.want
				if reversed {
					slices.Reverse(members)
					want = tc.wantReversed
				}

				diags := scanBytes(t, schemasDoc(members))

				if want == "" {
					assert.Empty(t, diags, "a chain that ends is no cycle however long it is")
					return
				}
				require.Len(t, diags, 1)
				assert.Equal(t, diag.CyclicRef, diags[0].Code)
				assert.Equal(t, ir.SeverityError, diags[0].Severity)
				assert.Equal(t, memberLine(t, members, want), diags[0].Provenance.Position.Line)
			})
		}
	}
}

// TestFollowRefChain_FollowsEachNodeOnce counts visits rather than timing
// them. A sweep from every start visits at most two nodes per start, however
// long the chain is and however it ends: the first walk records each node it
// followed, and every later start stops at its own record. The chains are
// declared head first, so without that every start would walk to the end.
func TestFollowRefChain_FollowsEachNodeOnce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		members []string
		starts  int
		want    chainVerdict
	}{
		{"chain-of-10000", slices.Concat(chainTo("S", 10000, "End"), []string{end}), 10000, chainTerminates},
		{"chain-of-20000", slices.Concat(chainTo("S", 20000, "End"), []string{end}), 20000, chainTerminates},
		{"loop-of-20000", loopOf(20000), 20000, chainCycles},
		{"tail-of-20000-into-a-loop", slices.Concat(chainTo("S", 20000, "C0"), loopOf(2)), 20002, chainCycles},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := indexOf(t, schemasDoc(tc.members)).Root()
			s := newRefScan()
			s.collect(root)
			require.Len(t, s.out, tc.starts)

			for _, start := range s.out {
				require.Equal(t, tc.want, s.followRefChain(root, start).verdict)
			}

			assert.LessOrEqual(t, s.visits, 2*len(s.out), "a start the first walk followed is visited once")
		})
	}
}

// parameterChainDoc is n component parameters, each a $ref to the next, ending
// in a loop of two that never leaves components.
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

// TestOutsideCycle_FollowsEachNodeOnce is the sweep above through outsideCycle,
// for a loop it leaves to the resolver: one that stays inside components. It
// reports nothing, so it walks from every start, and each later start stops at
// the cycle the first walk recorded (GitHub #924).
func TestOutsideCycle_FollowsEachNodeOnce(t *testing.T) {
	t.Parallel()
	const n = 20000
	root := indexOf(t, parameterChainDoc(n)).Root()
	s := newRefScan()
	s.collect(root)
	require.Len(t, s.outside, n+2)

	_, found := s.outsideCycle(InSource(0), root)

	assert.False(t, found, "a loop inside components is the resolver's to report")
	assert.LessOrEqual(t, s.visits, 2*len(s.outside), "a start the first walk followed is visited once")
}

// TestCycles_LongLoopOutsideComponentsIsRefused is the loop of reference
// objects spelled by document position, which speakeasy's resolver has no guard
// for, at a length past the 10,000 hops a walk once stopped at. Declared either
// way, the first path declared reports it (see GitHub #912 on that site).
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
			assert.Equal(t, 4, diags[0].Provenance.Position.Line, "the first path declared, below three header lines")
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

// TestFollowRefChain_RecordsWhereEachNodesChainEnds pins what a walk records
// for each node it followed, and that a walk stopping at a record answers as a
// whole walk would. A node's chain left components if its own hop did or any
// hop after it did, which on a loop is every hop of the loop. The walks run in
// several orders, so a record is met before, after and inside a loop.
func TestFollowRefChain_RecordsWhereEachNodesChainEnds(t *testing.T) {
	t.Parallel()
	entering := map[jsontext.Pointer]chainOutcome{
		"/paths/~1in":              {chainCycles, true},
		"/components/pathItems/In": {chainCycles, true},
		"/paths/~1out":             {chainCycles, false},
		"/components/pathItems/B":  {chainCycles, false},
		"/components/pathItems/C":  {chainCycles, false},
	}
	leaving := map[jsontext.Pointer]chainOutcome{
		"/components/pathItems/F": {chainCycles, true},
		"/components/pathItems/D": {chainCycles, true},
		"/components/pathItems/E": {chainCycles, true},
		"/paths/~1q":              {chainCycles, true},
	}
	tests := []struct {
		name  string
		walks []jsontext.Pointer
		want  map[jsontext.Pointer]chainOutcome
	}{
		{"entering-in-one-walk", []jsontext.Pointer{"/paths/~1in"}, entering},
		{"entering-loop-first", []jsontext.Pointer{"/components/pathItems/B", "/paths/~1in"}, entering},
		{"entering-backwards", []jsontext.Pointer{"/paths/~1out", "/components/pathItems/In", "/paths/~1in"}, entering},
		{"leaving-in-one-walk", []jsontext.Pointer{"/components/pathItems/F"}, leaving},
		{"leaving-loop-first", []jsontext.Pointer{"/components/pathItems/D", "/components/pathItems/F"}, leaving},
		{"leaving-loop-midway", []jsontext.Pointer{"/paths/~1q", "/components/pathItems/F"}, leaving},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := indexOf(t, []byte(recordDoc)).Root()
			s := newRefScan()
			at := func(p jsontext.Pointer) *yaml.Node {
				path, complete := s.view.PointerPath(root, p)
				require.True(t, complete, "%s names nothing", p)
				return path[len(path)-1]
			}

			for _, p := range tc.walks {
				assert.Equal(t, tc.want[p], s.followRefChain(root, at(p)), "the walk from %s", p)
			}

			for p, want := range tc.want {
				got, recorded := s.outcomes[at(p)]
				assert.True(t, recorded, "%s is on a walk and not recorded", p)
				assert.Equal(t, want, got, "the record for %s", p)
			}
		})
	}
}

// chainThroughRef is a chain of n schemas, ending in a loop of two if loops and
// in a schema with no $ref if not, whose hop from the schema at reads its target
// through a reference: member t of a reference object, so the pointer
// completes and the walk goes on. chain is every schema carrying a $ref, in walk
// order.
func chainThroughRef(n, at int, loops bool) (root *yaml.Node, chain []*yaml.Node) {
	const prefix = "#/schemas/"
	nodes := make([]*yaml.Node, n)
	for i := range nodes {
		next := prefix + strconv.Itoa(i+1)
		if i == n-1 {
			next = prefix + strconv.Itoa(n-2)
		}
		nodes[i] = ynode.Map(ynode.Scalar("$ref"), ynode.Scalar(next))
	}
	chain = nodes
	if !loops {
		nodes[n-1] = ynode.Map(ynode.Scalar("type"), ynode.Scalar("string"))
		chain = nodes[:n-1]
	}
	nodes[at].Content[1] = ynode.Scalar(prefix + "via/t")
	via := ynode.Map(ynode.Scalar("$ref"), ynode.Scalar(prefix+"nowhere"), ynode.Scalar("t"), nodes[at+1])

	schemas := ynode.Map()
	for i, node := range nodes {
		if i != at+1 {
			schemas.Content = append(schemas.Content, ynode.Scalar(strconv.Itoa(i)), node)
		}
	}
	schemas.Content = append(schemas.Content, ynode.Scalar("via"), via)
	return ynode.Map(ynode.Scalar("schemas"), schemas), chain
}

// TestFollowRefChain_ThroughARefRecordsNothing pins that a walk with a hop read
// through a reference records no node, however its chain ends: such a hop
// proves nothing about a start that reaches the same node by another route.
func TestFollowRefChain_ThroughARefRecordsNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		loops bool
		want  chainVerdict
	}{
		{"ends-in-a-loop", true, chainCycles},
		{"ends-in-a-schema", false, chainTerminates},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, chain := chainThroughRef(50, 10, tc.loops)
			s := newRefScan()

			assert.Equal(t, tc.want, s.followRefChain(root, chain[0]).verdict)

			assert.Empty(t, s.outcomes, "a chain read through a reference is recorded for no node")
		})
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

// TestCycles_ReentryRecordsNothing pins that a walk re-entering its chain
// leaves the chain to later starts. S0's walk re-enters, which refCycles does
// not report for a schema, and S1's meets the loop and is reported. Recording
// S0's walk would hand S1 its re-entry, and nothing would be reported.
func TestCycles_ReentryRecordsNothing(t *testing.T) {
	t.Parallel()

	diags := scanBytes(t, reentryDoc(20))

	require.Len(t, diags, 1)
	assert.Equal(t, diag.CyclicRef, diags[0].Code)
	assert.Equal(t, firstMemberLine+1, diags[0].Provenance.Position.Line)
}

// TestFollowRefChain_PanicsPastItsVisitBound pins the bound at its edge. A chain
// of twenty references ending in a schema takes 21 visits: a bound of 21 lets
// the walk finish, and one of 20 stops it with the panic recoverCycleScan turns
// into a warning.
func TestFollowRefChain_PanicsPastItsVisitBound(t *testing.T) {
	t.Parallel()
	root := indexOf(t, schemasDoc(slices.Concat(chainTo("S", 20, "End"), []string{end}))).Root()
	walk := func(bound int) func() {
		s := newRefScan()
		s.maxVisits = bound
		s.collect(root)
		return func() { s.followRefChain(root, s.out[0]) }
	}

	assert.NotPanics(t, walk(21))
	assert.PanicsWithValue(t, "cycle scan: a $ref chain outlasted its 20-node bound", walk(20))
}
