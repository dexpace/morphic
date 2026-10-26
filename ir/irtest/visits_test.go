package irtest_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// TestAllocatingVisits_FindsEachVisitRenderingItsPath holds the measurement to
// the walk's own path: a visitor rendering the path of each operation ID
// allocates there and nowhere else, which the zero ir.WalkPath, rendering
// empty, would not show. It is not run in parallel: AllocsPerRun measures the
// process.
func TestAllocatingVisits_FindsEachVisitRenderingItsPath(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{Groups: []ir.OperationGroup{{
		Operations: []ir.Operation{{ID: "op/x/A"}, {ID: "op/x/B"}},
	}}}}}
	var last string
	renderOpIDs := ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		if v.Type() == reflect.TypeFor[ir.OpID]() {
			last = at.String()
		}
		return true
	})

	assert.Equal(t, []string{
		"doc.Services[0].Groups[0].Operations[0].ID",
		"doc.Services[0].Groups[0].Operations[1].ID",
	}, irtest.AllocatingVisits(doc, renderOpIDs))
	assert.Equal(t, "doc.Services[0].Groups[0].Operations[1].ID", last)
	assert.Empty(t, irtest.AllocatingVisits(doc, ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool {
		return true
	})), "a visitor doing nothing allocates nothing")
}

// TestAllocatingVisits_PrunesForAllWhatOnePrunes holds the walk to never
// showing a visitor a value it pruned: what one visitor prunes, the others are
// not shown either. It is not run in parallel: AllocsPerRun measures the
// process.
func TestAllocatingVisits_PrunesForAllWhatOnePrunes(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{ID: "s/x"}}}
	pruneServices := ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		return at.String() != "doc.Services"
	})
	var seen []string
	record := ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		seen = append(seen, at.String())
		return true
	})

	irtest.AllocatingVisits(doc, pruneServices, record)
	assert.Contains(t, seen, "doc.Services")
	for _, at := range seen {
		assert.False(t, strings.HasPrefix(at, "doc.Services["), "%s lies under a pruned value", at)
	}
}

// TestAllocatingVisits_CountsTheFewestAVisitAllocates holds the measurement to
// what a visit must allocate, not what it happens to: a visit allocating on
// every third call, as one reaching a pool a race build drops from does at
// random, is not reported, though an average of its calls allocates. Every
// third, so the calls that allocate fall alike on a measurement's warm-up and
// on what it measures. It is not run in parallel: AllocsPerRun measures the
// process.
func TestAllocatingVisits_CountsTheFewestAVisitAllocates(t *testing.T) {
	doc := &ir.Document{Name: "api"}
	calls := 0
	var kept [][]byte
	sometimes := ir.VisitFunc(func(reflect.Value, ir.WalkPath) bool {
		if calls++; calls%3 == 0 {
			kept = append(kept[:0], make([]byte, 64), make([]byte, 64), make([]byte, 64), make([]byte, 64))
		}
		return true
	})

	assert.Empty(t, irtest.AllocatingVisits(doc, sometimes))
	assert.Positive(t, testing.AllocsPerRun(10, func() { sometimes(reflect.Value{}, ir.WalkPath{}) }),
		"averaged, the visit allocates, or the case tests nothing")
	assert.NotEmpty(t, kept)
}
