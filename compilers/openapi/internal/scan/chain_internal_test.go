package scan

import (
	"fmt"
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

// firstMemberLine is the line of the first schema in a chainDoc: the five lines
// of header above it.
const firstMemberLine = 6

// chainDoc is a document whose schemas form one $ref chain: tail schemas S0…,
// then either a loop of loopLen schemas C0… that refer round to each other, or,
// for loopLen 0, a schema that refers to nothing. reverse declares them last
// first, so each schema refers to one declared before it.
func chainDoc(tail, loopLen int, reverse bool) []byte {
	ref := func(name, target string) string {
		return fmt.Sprintf("    %s: {$ref: '#/components/schemas/%s'}\n", name, target)
	}
	var members []string
	for i := range tail {
		next := "S" + strconv.Itoa(i+1)
		switch {
		case i < tail-1:
		case loopLen > 0:
			next = "C0"
		default:
			next = "End"
		}
		members = append(members, ref("S"+strconv.Itoa(i), next))
	}
	for i := range loopLen {
		members = append(members, ref("C"+strconv.Itoa(i), "C"+strconv.Itoa((i+1)%loopLen)))
	}
	if loopLen == 0 {
		members = append(members, "    End: {type: string}\n")
	}
	if reverse {
		for i, j := 0, len(members)-1; i < j; i, j = i+1, j-1 {
			members[i], members[j] = members[j], members[i]
		}
	}
	return []byte("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n" +
		strings.Join(members, ""))
}

// TestCycles_ChainLongerThanTheCap pins what a chain past maxCycleDepth reports,
// in both declaration orders. The reported line is the first start whose own
// walk reaches the loop within the cap — not the head of the chain — so a change
// to how much of a capped walk is remembered moves it.
func TestCycles_ChainLongerThanTheCap(t *testing.T) {
	t.Parallel()
	const hops = 15000
	tests := []struct {
		name     string
		loopLen  int
		reverse  bool
		wantLine int // 0: no diagnostic
	}{
		{"terminal-forward", 0, false, 0},
		{"terminal-reverse", 0, true, 0},
		{"loop-forward", 2, false, firstMemberLine + hops + 2 - maxCycleDepth},
		{"loop-reverse", 2, true, firstMemberLine},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			diags := scanBytes(t, chainDoc(hops, tc.loopLen, tc.reverse))
			if tc.wantLine == 0 {
				assert.Empty(t, diags, "a chain that ends is not a cycle however long it is")
				return
			}
			require.Len(t, diags, 1)
			assert.Equal(t, diag.CyclicRef, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity)
			assert.Equal(t, tc.wantLine, diags[0].Provenance.Position.Line)
		})
	}
}

// TestCycles_VerdictsAroundTheCap pins which starts a loop is still found from,
// either side of maxCycleDepth. A start reveals a loop when its tail and the
// loop fit in the cap together, so the first to report is max(0, tail+loop-cap)
// schemas from the head, and none does once the loop alone is longer.
func TestCycles_VerdictsAroundTheCap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tail, loopLen int
		wantLine      int // 0: no diagnostic
	}{
		{tail: 5, loopLen: 2, wantLine: firstMemberLine},
		{tail: 9000, loopLen: 1000, wantLine: firstMemberLine},
		{tail: 9000, loopLen: 1001, wantLine: firstMemberLine + 1},
		{tail: 9999, loopLen: 2, wantLine: firstMemberLine + 1},
		{tail: 10000, loopLen: 2, wantLine: firstMemberLine + 2},
		{tail: 12000, loopLen: 5, wantLine: firstMemberLine + 2005},
		{tail: 12000, loopLen: 9990, wantLine: firstMemberLine + 11990},
		{tail: 3, loopLen: 10000, wantLine: firstMemberLine + 3},
		{tail: 3, loopLen: 10001},
		{tail: 20, loopLen: 10001},
		{tail: 1, loopLen: 20000},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("tail%d-loop%d", tc.tail, tc.loopLen), func(t *testing.T) {
			t.Parallel()
			diags := scanBytes(t, chainDoc(tc.tail, tc.loopLen, false))
			if tc.wantLine == 0 {
				assert.Empty(t, diags,
					"a loop longer than the cap is past what a start can reveal; #922 asks whether it should be")
				return
			}
			require.Len(t, diags, 1)
			assert.Equal(t, diag.CyclicRef, diags[0].Code)
			assert.Equal(t, tc.wantLine, diags[0].Provenance.Position.Line)
		})
	}
}

// chainHops is how many hops a sweep over every start of a chain follows, and
// how many starts there are. Every start must end without a verdict: a sweep
// that finds a loop stops at the first, which would measure nothing.
func chainHops(t *testing.T, tail, loopLen int) (hops, starts int) {
	t.Helper()
	root := indexOf(t, chainDoc(tail, loopLen, false)).Root()
	s := newRefScan()
	s.collect(root)
	for _, start := range s.out {
		verdict, _ := s.followRefChain(root, start)
		require.Equal(t, chainTerminates, verdict)
	}
	return s.hops, len(s.out)
}

// TestFollowRefChain_WorkDoesNotGrowQuadraticallyPastTheCap counts hops rather
// than timing them. The chain is declared head first, the order in which a walk
// stopping at the cap would be repeated from every start; the loops are longer
// than the cap, so no start reveals one and the sweep reaches every start.
func TestFollowRefChain_WorkDoesNotGrowQuadraticallyPastTheCap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		tail, loopLen int
	}{
		{"terminal-at-cap", maxCycleDepth, 0},
		{"terminal-past-cap", 2 * maxCycleDepth, 0},
		{"loop-just-past-cap", 5, maxCycleDepth + 1},
		{"loop-twice-cap", 5, 2 * maxCycleDepth},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hops, starts := chainHops(t, tc.tail, tc.loopLen)
			assert.LessOrEqual(t, hops, 3*starts,
				"each start should cost a hop or two once the first walk has covered the chain")
		})
	}
}

func TestFollowRefChain_ChainTwiceTheCapCostsAboutTwiceTheCap(t *testing.T) {
	t.Parallel()
	atCap, _ := chainHops(t, maxCycleDepth, 0)
	pastCap, _ := chainHops(t, 2*maxCycleDepth, 0)

	assert.Positive(t, atCap)
	assert.LessOrEqual(t, pastCap, 3*atCap, "the cost grew by more than a small multiple")
}

// chainThroughRef is a chain of n schemas, ending in a two-schema loop if loops
// and in a schema with no $ref if not, whose hop from schema at reads its target
// through a reference — the shape markSafe declines to remember. The target is
// the member t of a reference object, so the pointer completes and the walk goes
// on. chain is every schema carrying a $ref, in walk order.
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

func TestFollowRefChain_ThroughARefIsNotRememberedPastTheCap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		loops bool
	}{
		{"ends-in-a-loop", true},
		{"ends-in-a-schema", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, chain := chainThroughRef(maxCycleDepth+500, 100, tc.loops)

			s := newRefScan()
			verdict, _ := s.followRefChain(root, chain[0])

			assert.Equal(t, chainTerminates, verdict, "the chain ends past what this start can reveal")
			var remembered []int
			for i, n := range chain {
				if s.safe[n] {
					remembered = append(remembered, i)
				}
			}
			assert.Empty(t, remembered,
				"a chain read through a reference proves nothing about another reaching it")
		})
	}
}

// reentryDoc is a chain S0…S(n-1) whose last hop reads its target through S0,
// at a member t referring back to S(n-1). From S0 that hop re-enters the chain;
// from any later start it passes S0 as an ordinary reference and lands on a
// loop of t and S(n-1).
func reentryDoc(n int) []byte {
	var b strings.Builder
	b.WriteString("openapi: 3.1.0\ninfo: {title: t, version: '1'}\npaths: {}\ncomponents:\n  schemas:\n")
	last := "S" + strconv.Itoa(n-1)
	fmt.Fprintf(&b, "    S0: {$ref: '#/components/schemas/S1', t: {$ref: '#/components/schemas/%s'}}\n", last)
	for i := 1; i < n-1; i++ {
		fmt.Fprintf(&b, "    S%d: {$ref: '#/components/schemas/S%d'}\n", i, i+1)
	}
	fmt.Fprintf(&b, "    %s: {$ref: '#/components/schemas/S0/t'}\n", last)
	return []byte(b.String())
}

// TestCycles_ReentryPastTheCapIsNotRemembered pins that a walk re-entering its
// chain past maxCycleDepth leaves the chain to later starts. The re-entry is
// S0's alone: S(i) for i > 0 meets the loop's revisit after n+1-i hops, and the
// first to do so within the cap reports it.
func TestCycles_ReentryPastTheCapIsNotRemembered(t *testing.T) {
	t.Parallel()
	const n = maxCycleDepth + 10

	diags := scanBytes(t, reentryDoc(n))

	require.Len(t, diags, 1)
	assert.Equal(t, diag.CyclicRef, diags[0].Code)
	assert.Equal(t, firstMemberLine+n+1-maxCycleDepth, diags[0].Provenance.Position.Line)
}

func TestFollowRefChain_HopLimitEndsTheWalkWithoutAVerdict(t *testing.T) {
	t.Parallel()
	root := indexOf(t, chainDoc(20, 0, false)).Root()
	s := newRefScan()
	s.hopLimit = 5
	s.collect(root)

	verdict, _ := s.followRefChain(root, s.out[0])

	assert.Equal(t, chainTerminates, verdict)
	assert.Empty(t, s.safe, "a walk that stopped at the limit proved nothing about its nodes")
	assert.Equal(t, s.hopLimit+1, s.hops)
}
