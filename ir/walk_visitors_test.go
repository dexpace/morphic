package ir_test

import (
	"hash/fnv"
	"math/rand/v2"
	"reflect"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// recorder is an ir.CycleVisitor that records each path it is shown and each
// cycle it is told of, pruning wherever prune says, and logs each visit to log
// when one is shared with other recorders.
type recorder struct {
	name   string
	prune  func(path string) bool
	log    *[]string
	paths  []string
	cycles []valueCycle
}

func (r *recorder) Visit(_ reflect.Value, at ir.WalkPath) bool {
	path := at.String()
	r.paths = append(r.paths, path)
	if r.log != nil {
		*r.log = append(*r.log, r.name+" "+path)
	}
	return r.prune == nil || !r.prune(path)
}

func (r *recorder) Cycle(at, entered ir.WalkPath, ref reflect.Type) {
	r.cycles = append(r.cycles, valueCycle{Path: at.String(), Entered: entered.String(), Type: ref.String()})
}

// pruneOneIn returns a pruning rule that prunes about one path in n, the ones
// salt picks, so that recorders built with different salts prune different
// parts of one document.
func pruneOneIn(n uint32, salt string) func(string) bool {
	return func(path string) bool {
		h := fnv.New32a()
		_, _ = h.Write([]byte(salt + path))
		return h.Sum32()%n == 0
	}
}

// sharedAndAlone walks root once with a recorder per rule, and again with each
// rule's recorder alone, and returns both sets with the shared walk's truncation
// and whether any lone walk truncated.
func sharedAndAlone(root any, rules []func(string) bool) (shared, alone []*recorder, sharedCut, aloneCut bool) {
	visitors := make([]ir.Visitor, len(rules))
	for i, rule := range rules {
		r := &recorder{prune: rule}
		shared = append(shared, r)
		visitors[i] = r
	}
	sharedCut = ir.WalkValues(root, "doc", visitors...)
	for _, rule := range rules {
		r := &recorder{prune: rule}
		alone = append(alone, r)
		aloneCut = ir.WalkValues(root, "doc", r) || aloneCut
	}
	return shared, alone, sharedCut, aloneCut
}

// assertSameWalks holds each shared recorder to having been shown and told
// exactly what the lone one beside it was.
func assertSameWalks(t *testing.T, shared, alone []*recorder, context any) {
	t.Helper()
	for i := range shared {
		require.Empty(t, cmp.Diff(alone[i].paths, shared[i].paths), "visitor %d's paths, in %+v", i, context)
		require.Empty(t, cmp.Diff(alone[i].cycles, shared[i].cycles), "visitor %d's cycles, in %+v", i, context)
	}
}

// TestWalkValues_VisitorsShareOneTraversal holds the visitors to one traversal,
// called in the order given: each value is shown to every visitor before the
// walk moves on, which separate walks, one per visitor, would not do.
func TestWalkValues_VisitorsShareOneTraversal(t *testing.T) {
	t.Parallel()
	var log []string
	first, second := &recorder{name: "first", log: &log}, &recorder{name: "second", log: &log}
	doc := &ir.Document{Name: "api", Types: ir.TypeRegistry{"t/x/M": &ir.Model{ID: "t/x/M"}}}

	ir.WalkValues(doc, "doc", first, second)
	require.Greater(t, len(log), 4, "the walk must reach more than the root")
	for i := 0; i < len(log); i += 2 {
		assert.Equal(t, "first "+first.paths[i/2], log[i])
		assert.Equal(t, "second "+first.paths[i/2], log[i+1])
	}
}

// TestWalkValues_APrunedVisitorStillMeetsASharedPointerElsewhere holds the walk
// to entering a pointer once per visitor rather than once per walk. Here one
// pointer is reached at doc.A and at doc.B; the first visitor prunes doc.A, so
// alone it enters the pointer at doc.B. A walk recording only that some visitor
// had entered it would skip it there, and the first visitor would never meet it.
func TestWalkValues_APrunedVisitorStillMeetsASharedPointerElsewhere(t *testing.T) {
	t.Parallel()
	shared := &ptrNode{}
	root := struct{ A, B *ptrNode }{A: shared, B: shared}
	rules := []func(string) bool{func(path string) bool { return path == "doc.A" }, nil}

	got, want, _, _ := sharedAndAlone(root, rules)
	assert.Contains(t, got[0].paths, "doc.B.Next", "the first visitor meets the pointer where it alone enters it")
	assert.NotContains(t, got[1].paths, "doc.B.Next", "the second entered it at doc.A, and meets it once")
	assertSameWalks(t, got, want, root)
}

// TestWalkValues_EachVisitorSeesTheWalkItWouldSeeAlone draws documents of
// shared and cyclic values at random, and visitors each pruning a different
// share of them, and holds every visitor of the one walk to the paths, cycles
// and truncation it meets walking alone. The seed is fixed, so a failure can be
// drawn again.
func TestWalkValues_EachVisitorSeesTheWalkItWouldSeeAlone(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(813, 962))
	rules := []func(string) bool{nil, pruneOneIn(3, "a"), pruneOneIn(4, "b"), pruneOneIn(5, "c")}
	var differ int
	for range 400 {
		nodes := randomGraph(r)
		values := buildGraph(nodes)
		model := &ir.Model{ID: "t/x/M"}
		for i := range values {
			model.Examples = append(model.Examples, ir.Example{Value: &values[i]})
		}
		doc := &ir.Document{Types: ir.TypeRegistry{model.ID: model}}

		shared, alone, sharedCut, aloneCut := sharedAndAlone(doc, rules)
		assertSameWalks(t, shared, alone, nodes)
		require.Equal(t, aloneCut, sharedCut, "%+v", nodes)
		if !cmp.Equal(alone[0].paths, alone[1].paths) {
			differ++
		}
	}
	assert.Greater(t, differ, 100, "the rules must prune often enough to give visitors different walks")
}

// TestWalkValues_TruncationIsAnyVisitorsWalk holds the flag to what the
// visitors' own walks report: cut short when any of them reaches past the cap,
// and not when the only one that would have pruned above it.
func TestWalkValues_TruncationIsAnyVisitorsWalk(t *testing.T) {
	t.Parallel()
	deep := irtest.NestedListValue(ir.MaxWalkDepth)
	stops := ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return false })
	walks := ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return true })

	assert.False(t, ir.WalkValues(deep, "doc", stops), "a visitor that prunes at the root reaches no cap")
	assert.True(t, ir.WalkValues(deep, "doc", stops, walks), "one visitor reaching the cap truncates the walk")
}

// TestWalkValues_MoreVisitorsThanOneTraversalCarries holds the walk to its claim
// however many visitors share it: past the number one traversal carries, each
// is still shown the walk it would see alone, cycles included.
func TestWalkValues_MoreVisitorsThanOneTraversalCarries(t *testing.T) {
	t.Parallel()
	loop := &ptrNode{}
	loop.Next = loop
	root := struct {
		Loop *ptrNode
		Kids []kidTree
		Doc  *ir.Document
	}{Loop: loop, Kids: []kidTree{{}, {Kids: []kidTree{{}}}}, Doc: listOf(3)}
	rules := make([]func(string) bool, 130)
	for i := range rules {
		rules[i] = pruneOneIn(uint32(2+i%7), strconv.Itoa(i))
	}

	shared, alone, _, _ := sharedAndAlone(root, rules)
	assertSameWalks(t, shared, alone, "130 visitors")
}

// TestWalkValues_TruncationReachesPastOneTraversal holds the flag to the same
// rule when the visitors outnumber one traversal: a walk of 130 is cut short
// when any one of them reaches the cap, the first or the last, each in a
// traversal the others do not share, and not when every one prunes at the root.
func TestWalkValues_TruncationReachesPastOneTraversal(t *testing.T) {
	t.Parallel()
	deep := irtest.NestedListValue(ir.MaxWalkDepth)
	stops := ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return false })
	walks := ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool { return true })
	visitorsWith := func(walking int) []ir.Visitor {
		visitors := make([]ir.Visitor, 130)
		for i := range visitors {
			visitors[i] = stops
		}
		if walking >= 0 {
			visitors[walking] = walks
		}
		return visitors
	}
	assert.False(t, ir.WalkValues(deep, "doc", visitorsWith(-1)...))
	assert.True(t, ir.WalkValues(deep, "doc", visitorsWith(0)...), "the first visitor's traversal reaches the cap")
	assert.True(t, ir.WalkValues(deep, "doc", visitorsWith(129)...), "the last visitor's traversal reaches the cap")
}

// TestWalkValues_NilVisitorsAreSkipped pins the two ways to pass a visitor that
// is not there: a nil Visitor is skipped, and a nil VisitFunc prunes at the root.
// Neither panics, and a walk with no visitor reaches nothing and cuts nothing.
func TestWalkValues_NilVisitorsAreSkipped(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{Name: "api"}
	r := &recorder{}
	assert.False(t, ir.WalkValues(doc, "doc", nil, r, ir.VisitFunc(nil)))
	assert.Contains(t, r.paths, "doc.Name", "a nil visitor beside it does not stop the others")

	deep := irtest.NestedListValue(ir.MaxWalkDepth)
	assert.False(t, ir.WalkValues(deep, "doc"), "no visitor, no walk")
	assert.False(t, ir.WalkValues(deep, "doc", nil), "a nil visitor is none")
	assert.False(t, ir.WalkValues(deep, "doc", ir.VisitFunc(nil)), "a nil VisitFunc prunes at the root")
}
