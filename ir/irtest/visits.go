package irtest

import (
	"math"
	"reflect"
	"testing"

	"github.com/dexpace/morphic/ir"
)

// AllocatingVisits walks doc with visitors and returns the path of each visit
// that must allocate, once per visitor allocating there, in walk order, a walk
// per 64 visitors as ir.WalkValues makes. Each visitor is shown the walk it
// would be shown alone, with the walk's own path: the zero ir.WalkPath renders
// empty.
//
// Only a repeat of a visit is measured (fewestAllocs), so what a visitor builds
// on first meeting a value goes uncounted: a memo, but also anything it keeps
// per value, which a test growing the document finds. AllocsPerRun
// measures the process, so a caller is not run in parallel.
func AllocatingVisits(doc *ir.Document, visitors ...ir.Visitor) []string {
	var paths []string
	measured := make([]ir.Visitor, len(visitors))
	for i, visitor := range visitors {
		if visitor != nil { // a nil one stays nil, which the walk skips
			measured[i] = measuredVisitor{visitor: visitor, paths: &paths}
		}
	}
	ir.WalkValues(doc, ir.DocumentPath, measured...)
	return paths
}

// measuredVisitor shows visitor what the walk shows it and appends the path of
// each visit that allocates to paths.
type measuredVisitor struct {
	visitor ir.Visitor
	paths   *[]string
}

// Visit shows v to the visitor, then measures the visit, and asks for v's
// children as the visitor does.
func (m measuredVisitor) Visit(v reflect.Value, at ir.WalkPath) bool {
	children := m.visitor.Visit(v, at)
	if fewestAllocs(func() { m.visitor.Visit(v, at) }) > 0 {
		*m.paths = append(*m.paths, at.String())
	}
	return children
}

// Cycle tells the visitor of a cycle if it asks to be told, so a visitor
// gathering cycles gathers what it would in a walk of its own.
func (m measuredVisitor) Cycle(at, entered ir.WalkPath, ref reflect.Type) {
	if cycles, asks := m.visitor.(ir.CycleVisitor); asks {
		cycles.Cycle(at, entered, ref)
	}
}

// visitMeasurements is how many times AllocatingVisits measures one visit. A
// race build's sync.Pool drops one in four of the items it is handed, so a
// visit handing k of them back allocates in a measurement with a chance of
// about 1-(3/4)^k, and in every one of this many with a negligible chance
// while k is a handful. A visit handing back dozens allocates in nearly every
// one, so code measured here reads its values through nothing pooled per value.
const visitMeasurements = 20

// fewestAllocs returns the fewest allocations one call of visit makes in
// visitMeasurements measurements, stopping at the first that makes none, as no
// later one can make fewer. The fewest, because under the race detector
// sync.Pool drops a share of what it is handed, so a visit reaching a pool, as
// jsontext.Value.IsValid does, allocates a random amount more than it must,
// and never less: an average counts the drops.
func fewestAllocs(visit func()) float64 {
	fewest := math.Inf(1)
	for range visitMeasurements {
		if fewest = min(fewest, testing.AllocsPerRun(1, visit)); fewest == 0 {
			break
		}
	}
	return fewest
}
