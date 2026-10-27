package ir_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// TestJoinInferred_ListsEachNameOnceInFirstWrittenOrder pins the one form an
// inferred-marker list takes: every name once, in the order first written,
// never sorted, with nothing empty and nothing trimmed. Each argument is itself
// a list, so one heuristic's name and a stored list go through the same call.
func TestJoinInferred_ListsEachNameOnceInFirstWrittenOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		lists []ir.Inferred
		want  ir.Inferred
	}{
		{"no argument", nil, ""},
		{"one empty list", []ir.Inferred{""}, ""},
		{"one name", []ir.Inferred{"a"}, "a"},
		{"an empty list first", []ir.Inferred{"", "a"}, "a"},
		{"an empty list last", []ir.Inferred{"a", ""}, "a"},
		{"a name twice", []ir.Inferred{"a", "a"}, "a"},
		{"the first written leads, nothing is sorted", []ir.Inferred{"b", "a"}, "b,a"},
		{"two lists sharing a name", []ir.Inferred{"a,b", "b,c"}, "a,b,c"},
		{"an empty name inside a list", []ir.Inferred{"a,,b"}, "a,b"},
		{"a list with a leading and a trailing separator", []ir.Inferred{",a,"}, "a"},
		{"a name repeated inside one list", []ir.Inferred{"a,b,a"}, "a,b"},
		{"names compare exactly and nothing is trimmed", []ir.Inferred{" a", "a"}, " a,a"},
		{"a repeat past the names a small set holds", []ir.Inferred{distinctNames(ir.FewInferred + 1), "n0"},
			distinctNames(ir.FewInferred + 1)},
		{"the heuristics the compiler writes", []ir.Inferred{
			"group-path-prefix", "streaming-media-type", "extension-promotion",
		}, "group-path-prefix,streaming-media-type,extension-promotion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ir.JoinInferred(tc.lists...))
		})
	}
}

// distinctNames returns a list of n distinct names, n0 to n(n-1).
func distinctNames(n int) ir.Inferred {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("n%d", i)
	}
	return ir.Inferred(strings.Join(out, ","))
}

// TestInferred_IsCanonicalIsWhatJoinInferredKeeps reads the lists a producer
// that skips the join can store, GitHub #1114's among them: a list is canonical
// exactly when JoinInferred hands it back unchanged.
func TestInferred_IsCanonicalIsWhatJoinInferredKeeps(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		list ir.Inferred
		want bool
	}{
		{"", true},
		{"a", true},
		{"a,b", true},
		{"b,a", true},
		{" a,a", true},
		{"a,A", true},
		{"a,,b", false},
		{",a", false},
		{"a,", false},
		{",", false},
		{"a,a", false},
		{"b,a,b", false},
		{distinctNames(ir.FewInferred + 1), true},
		{distinctNames(ir.FewInferred+1) + ",n0", false},
	} {
		t.Run(string(tc.list), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.list.IsCanonical())
			assert.Equal(t, tc.want, ir.JoinInferred(tc.list) == tc.list, "JoinInferred(%q)", tc.list)
		})
	}
}

// TestInferred_IsCanonicalAgreesWithJoinInferredOnEveryShortList holds the
// reading to the writing on every list of up to seven bytes over a separator,
// two names' letters and a byte that does not decode. The separator is the
// grammar's one special byte, so these are all its shapes up to that length,
// and a join always writes a canonical list.
func TestInferred_IsCanonicalAgreesWithJoinInferredOnEveryShortList(t *testing.T) {
	t.Parallel()
	const alphabet, longest = "ab,\xe9", 7
	level := []string{""}
	for length := 0; ; length++ {
		for _, list := range level {
			l := ir.Inferred(list)
			joined := ir.JoinInferred(l)
			if l.IsCanonical() != (joined == l) {
				t.Fatalf("IsCanonical(%q) = %v, but JoinInferred writes %q", list, l.IsCanonical(), joined)
			}
			if !joined.IsCanonical() {
				t.Fatalf("JoinInferred(%q) = %q, which IsCanonical refuses", list, joined)
			}
		}
		if length == longest {
			return
		}
		level = extended(level, alphabet)
	}
}

// extended returns every list of level followed by one byte of alphabet.
func extended(level []string, alphabet string) []string {
	out := make([]string, 0, len(level)*len(alphabet))
	for _, list := range level {
		for i := range len(alphabet) {
			out = append(out, list+alphabet[i:i+1])
		}
	}
	return out
}

// TestInferred_IsCanonicalFindsEveryRepeatAndEmptyName plants a repeat of each
// name at each later place, and an empty name at each place, in lists the set
// holds on the stack and lists it outgrows, where a repeat is found only if the
// names read before the set grew are still in it.
func TestInferred_IsCanonicalFindsEveryRepeatAndEmptyName(t *testing.T) {
	t.Parallel()
	for _, n := range []int{2, ir.FewInferred, ir.FewInferred + 1, 4 * ir.FewInferred} {
		distinct := strings.Split(string(distinctNames(n)), ",")
		assert.True(t, distinctNames(n).IsCanonical(), "%d distinct names", n)
		for i := range n {
			for j := i + 1; j < n; j++ {
				l := withName(distinct, j, distinct[i])
				assert.False(t, l.IsCanonical(), "%d names, the one at %d repeated at %d", n, i, j)
			}
			assert.False(t, withName(distinct, i, "").IsCanonical(), "%d names, empty at %d", n, i)
		}
	}
}

// withName returns list with the name at i replaced by name.
func withName(list []string, i int, name string) ir.Inferred {
	out := append([]string(nil), list...)
	out[i] = name
	return ir.Inferred(strings.Join(out, ","))
}

// TestInferred_IsCanonicalAllocatesNothingForAFewNames holds IsCanonical to the
// cost its doc promises for the lists a node carries, canonical or not, since
// irverify asks it of every list a document stores. It is not run in parallel:
// AllocsPerRun measures the process.
func TestInferred_IsCanonicalAllocatesNothingForAFewNames(t *testing.T) {
	for n := 1; n <= ir.FewInferred; n++ {
		l := distinctNames(n)
		assert.Zero(t, testing.AllocsPerRun(10, func() { _ = l.IsCanonical() }), "%d distinct names", n)
	}
	repeat := distinctNames(ir.FewInferred-1) + ",n0"
	assert.Zero(t, testing.AllocsPerRun(10, func() { _ = repeat.IsCanonical() }), "%q", repeat)
}
