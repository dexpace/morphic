package irtest

import (
	"reflect"
	"testing"

	"github.com/dexpace/morphic/ir"
)

// AllocatingVisits walks doc with visitors and returns, in walk order, the path
// of each value whose visit allocates when repeated, handed the walk's own path,
// which the zero ir.WalkPath cannot stand in for: it renders empty without
// allocating.
//
// Each value is visited once before it is measured, so what a first meeting
// builds, such as a memo entry, is not counted, and many times after, so a
// visitor keeps what it keeps as often. What one visitor prunes is pruned for
// all. AllocsPerRun measures the process, so a caller is not run in parallel.
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
		if testing.AllocsPerRun(10, func() { visit() }) > 0 {
			paths = append(paths, at.String())
		}
		return children
	}))
	return paths
}
