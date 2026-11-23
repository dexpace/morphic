// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"reflect"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var (
	typeIDType       = reflect.TypeFor[ir.TypeID]()
	typeRegistryType = reflect.TypeFor[ir.TypeRegistry]()
	documentType     = reflect.TypeFor[ir.Document]()
)

// anonEdge is a directed reference from one anonymous type to another,
// indexed into anonGraph.anonIDs.
type anonEdge struct {
	from, to int
}

// noUnreferencedReports is the report function when a document has no anonymous
// types to check.
func noUnreferencedReports(declarations) []Violation { return nil }

// checkUnreferencedAnonymousTypes asserts that every anonymous type in
// doc.Types is reachable through typed ir.TypeID references from a
// non-anonymous type or from outside doc.Types (including UnmodeledEntry
// Targets). A truncated walk reports nothing here, as ir/walk-truncated
// already notes the incomplete check.
func checkUnreferencedAnonymousTypes(doc *ir.Document) walkCheck {
	g := newAnonGraph(doc.Types)
	if g == nil {
		return walkCheck{report: noUnreferencedReports}
	}
	return walkCheck{
		visit:  g.visit,
		report: g.report,
	}
}

// anonGraph tracks reachability of anonymous types during the shared walk with
// flat, pre-allocated slices so clean visits allocate nothing.
type anonGraph struct {
	anonIDs    []ir.TypeID
	anonIndex  map[ir.TypeID]int
	reached    []bool
	stack      []int
	edges      []anonEdge
	inTypes    bool
	ownerIndex int
}

// newAnonGraph indexes the anonymous types of types, or returns nil when none
// exist.
func newAnonGraph(types ir.TypeRegistry) *anonGraph {
	var n int
	for _, td := range types {
		if !ir.IsNilTypeDef(td) && td.Common().Anonymous {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	ids := make([]ir.TypeID, 0, n)
	for id, td := range types {
		if !ir.IsNilTypeDef(td) && td.Common().Anonymous {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	index := make(map[ir.TypeID]int, n)
	for i, id := range ids {
		index[id] = i
	}
	return &anonGraph{
		anonIDs:    ids,
		anonIndex:  index,
		reached:    make([]bool, n),
		stack:      make([]int, 0, n),
		edges:      make([]anonEdge, 0, max(16, 4*n)),
		ownerIndex: -1,
	}
}

// visit tracks whether the shared walk is inside Document.Types and records
// every typed ir.TypeID edge reaching an anonymous type.
func (g *anonGraph) visit(v reflect.Value, at ir.WalkPath) bool {
	t := v.Type()
	if t == typeRegistryType {
		g.inTypes = true
		g.ownerIndex = -1
		return true
	}
	if owner, _, ok := at.Field(); ok && owner == documentType {
		g.inTypes = false
		g.ownerIndex = -1
	}
	if g.inTypes && at.IsMapKey() && t == typeIDType {
		key, _ := reflect.TypeAssert[ir.TypeID](v)
		if index, ok := g.anonIndex[key]; ok {
			g.ownerIndex = index
		} else {
			g.ownerIndex = -1
		}
		return false
	}
	if t != typeIDType {
		return true
	}
	if _, field, ok := at.Field(); ok && field == "ID" {
		return false
	}
	id, _ := reflect.TypeAssert[ir.TypeID](v)
	g.record(id)
	return false
}

// record marks id as a root when reached from outside Document.Types or from a
// non-anonymous type, or appends an edge between anonymous types.
func (g *anonGraph) record(id ir.TypeID) {
	to, ok := g.anonIndex[id]
	if !ok {
		return
	}
	if !g.inTypes || g.ownerIndex < 0 {
		if !g.reached[to] {
			g.reached[to] = true
			g.stack = append(g.stack, to)
		}
		return
	}
	if to != g.ownerIndex {
		g.edges = append(g.edges, anonEdge{from: g.ownerIndex, to: to})
	}
}

// report propagates reachability across anonymous-to-anonymous edges and
// returns a violation for every anonymous type left unreachable.
func (g *anonGraph) report(decls declarations) []Violation {
	n := len(g.anonIDs)
	if decls.truncated || len(g.stack) == n {
		return nil
	}
	if g.propagate() == n {
		return nil
	}
	var vs []Violation
	for i, id := range g.anonIDs {
		if !g.reached[i] {
			vs = append(vs, Violation{
				Code:    "ir/unreferenced-anonymous-type",
				Message: "anonymous type " + strconv.Quote(string(id)) + " is reached by no non-anonymous type and no reference outside types",
				Path:    typePath(id),
			})
		}
	}
	return vs
}

// propagate walks g.edges from g.stack using a flat compressed adjacency list
// and returns the total number of reached anonymous types.
func (g *anonGraph) propagate() int {
	reachedCount := len(g.stack)
	if reachedCount == 0 || len(g.edges) == 0 {
		return reachedCount
	}
	n := len(g.anonIDs)
	offsets := make([]int, n+1)
	for _, e := range g.edges {
		offsets[e.from+1]++
	}
	for i := 1; i <= n; i++ {
		offsets[i] += offsets[i-1]
	}
	adj := make([]int, len(g.edges))
	cursor := slices.Clone(offsets[:n])
	for _, e := range g.edges {
		adj[cursor[e.from]] = e.to
		cursor[e.from]++
	}
	for len(g.stack) > 0 {
		cur := g.stack[len(g.stack)-1]
		g.stack = g.stack[:len(g.stack)-1]
		for _, next := range adj[offsets[cur]:offsets[cur+1]] {
			if !g.reached[next] {
				g.reached[next] = true
				reachedCount++
				g.stack = append(g.stack, next)
			}
		}
	}
	return reachedCount
}
