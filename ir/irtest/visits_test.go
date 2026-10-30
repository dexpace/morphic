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

// TestAllocatingVisits_PrunesForEachVisitorAlone holds the walk to the one each
// visitor would be shown alone: never what it pruned, and still what another
// pruned, so a visitor pruning at the root, as one visiting nothing does, hides
// nothing from the others. A walk pruning for all what one prunes measured no
// value below the root, and reported a document clean. It is not run in
// parallel: AllocsPerRun measures the process.
func TestAllocatingVisits_PrunesForEachVisitorAlone(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{ID: "s/x"}}}
	var pruned, recorded []string
	pruneServices := ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		pruned = append(pruned, at.String())
		return at.String() != "doc.Services"
	})
	record := ir.VisitFunc(func(_ reflect.Value, at ir.WalkPath) bool {
		recorded = append(recorded, at.String())
		return true
	})

	irtest.AllocatingVisits(doc, ir.VisitFunc(nil), pruneServices, nil, record)
	assert.Contains(t, pruned, "doc.Services")
	for _, at := range pruned {
		assert.False(t, strings.HasPrefix(at, "doc.Services["), "%s lies under a value this visitor pruned", at)
	}
	assert.Contains(t, recorded, "doc.Services[0].ID", "what other visitors prune is shown to this one")
}

// TestAllocatingVisits_ListsAValueForEachVisitorAllocating holds the report to
// one path per allocating visit: a value two visitors allocate for is listed
// twice, and one only a visitor that does not prune it allocates for, once.
// It is not run in parallel: AllocsPerRun measures the process.
func TestAllocatingVisits_ListsAValueForEachVisitorAllocating(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{ID: "s/x"}}}
	var kept []string
	rendering := func(prune bool) ir.Visitor {
		return ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
			if v.Type() == reflect.TypeFor[ir.ServiceID]() || v.Type() == reflect.TypeFor[ir.Service]() {
				kept = append(kept[:0], at.String())
			}
			return !prune || v.Type() != reflect.TypeFor[ir.Service]()
		})
	}

	assert.Equal(t, []string{"doc.Services[0]", "doc.Services[0]", "doc.Services[0].ID"},
		irtest.AllocatingVisits(doc, rendering(false), rendering(true)))
}

// TestAllocatingVisits_MeasuresACleanVisitOnce pins what a measurement costs:
// a visit allocating nothing is measured once, since no later measurement can
// find fewer, and one that allocates as often as it takes to find its fewest.
// Each measurement makes the visit twice, a warm-up and the one measured, after
// the walk's own. It is not run in parallel: AllocsPerRun measures the process.
func TestAllocatingVisits_MeasuresACleanVisitOnce(t *testing.T) {
	doc := &ir.Document{Name: "api"}
	var kept []byte
	counting := func(allocate bool) (ir.Visitor, *int) {
		calls := 0
		return ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
			if v.Kind() == reflect.String && v.String() == "api" {
				calls++
				if allocate {
					kept = make([]byte, 64)
				}
			}
			return true
		}), &calls
	}
	clean, cleanCalls := counting(false)
	allocating, allocatingCalls := counting(true)

	assert.Empty(t, irtest.AllocatingVisits(doc, clean))
	assert.Equal(t, []string{"doc.Name"}, irtest.AllocatingVisits(doc, allocating))
	assert.Equal(t, 1+2, *cleanCalls, "the walk's visit, then one measurement")
	assert.Equal(t, 1+2*irtest.VisitMeasurements, *allocatingCalls, "the walk's visit, then every measurement")
	assert.Len(t, kept, 64, "the allocating visit kept what it made")
}

// TestAllocatingVisits_TellsACycleVisitorOfACycle holds the walk to telling a
// visitor gathering cycles of each one, as a walk of its own would: a value
// holding itself is met again where the walk is inside it. It is not run in
// parallel: AllocsPerRun measures the process.
func TestAllocatingVisits_TellsACycleVisitorOfACycle(t *testing.T) {
	list := make([]ir.Value, 1)
	list[0] = ir.Value{Kind: ir.ValueList, List: list}
	doc := &ir.Document{Types: ir.TypeRegistry{"t/x/M": &ir.Model{
		ID: "t/x/M", Examples: []ir.Example{{Value: &list[0]}},
	}}}
	cycles := &cycleCounter{}

	irtest.AllocatingVisits(doc, cycles)
	assert.Positive(t, cycles.met, "the self-holding list closes a cycle")
}

// cycleCounter visits everything and counts the cycles it is told of.
type cycleCounter struct{ met int }

// Visit asks for every value's children.
func (*cycleCounter) Visit(reflect.Value, ir.WalkPath) bool { return true }

// Cycle counts one cycle.
func (c *cycleCounter) Cycle(ir.WalkPath, ir.WalkPath, reflect.Type) { c.met++ }

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
