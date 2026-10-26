package irtest

import (
	"math"
	"reflect"
	"testing"

	"github.com/dexpace/morphic/ir"
)

// AllocatingVisits walks doc with visitors and returns, in walk order, the path
// of each value whose visit allocates however often it is repeated, handed the
// walk's own path, which the zero ir.WalkPath cannot stand in for: it renders
// empty without allocating.
//
// Each value is visited once before it is measured, so what a first meeting
// builds, such as a memo entry, is not counted, and then measured one visit at
// a time, the fewest counting (fewestAllocs). What one visitor prunes is pruned
// for all. AllocsPerRun measures the process, so a caller is not run in
// parallel.
func AllocatingVisits(doc *ir.Document, visitors ...ir.Visitor) []string {
	var paths []string
	ir.WalkValues(doc, ir.DocumentPath, ir.VisitFunc(func(v reflect.Value, at ir.WalkPath) bool {
		visit := func() bool {
			children := true
			for _, visitor := range visitors {
				children = visitor.Visit(v, at) && children
			}
			return children
		}
		children := visit()
		if fewestAllocs(visit) > 0 {
			paths = append(paths, at.String())
		}
		return children
	}))
	return paths
}

// visitMeasurements is how many times AllocatingVisits measures one visit. A
// race build's sync.Pool drops one in four of the items it is handed, so a
// visit handing k of them back allocates in a measurement with a chance of
// about 1-(3/4)^k, and in every one of this many with a negligible chance for
// the handful of pools a visit reaches.
const visitMeasurements = 20

// fewestAllocs returns the fewest allocations one call of visit makes in
// visitMeasurements measurements. The fewest, because under the race detector
// sync.Pool drops a share of what it is handed, so a visit reaching a pool, as
// jsontext.Value.IsValid does, allocates a random amount more than it must,
// and never less: an average counts the drops.
func fewestAllocs(visit func() bool) float64 {
	fewest := math.Inf(1)
	for range visitMeasurements {
		fewest = min(fewest, testing.AllocsPerRun(1, func() { visit() }))
	}
	return fewest
}
