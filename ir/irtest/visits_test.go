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
