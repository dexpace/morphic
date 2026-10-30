// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts

// intervals indexes what each vertex of a directed graph reaches, a vertex
// lying below every one it reaches. The strongly connected components are
// numbered in post-order, searching down from each component that reaches no
// other, and each carries the interval from the least number below it to its
// own. A component outside another's interval is never below it, and one
// numbered by the other's own search always is. Only a component an earlier
// search reached first, through another parent, can lie inside an interval
// without being below it.
type intervals struct {
	comp   []int  // each vertex's component
	cyclic []bool // each component's members reach one another
	lo, hi []int  // each component's interval: the least number below it, and its own
	first  []int  // the first number each component's own search gave out
}

// newIntervals indexes the graph whose vertex v has an edge to each vertex in
// edges[v].
func newIntervals(edges [][]int) *intervals {
	r := &intervals{}
	r.number(r.condense(edges))
	return r
}

// condense records each vertex's component and whether each component holds a
// cycle, and returns each component's child components, the ones with an edge
// to it, a child once for each such edge.
func (r *intervals) condense(edges [][]int) [][]int {
	comps := newTarjan(edges).run()
	r.comp = make([]int, len(edges))
	for c, members := range comps {
		for _, v := range members {
			r.comp[v] = c
		}
	}
	r.cyclic = make([]bool, len(comps))
	kids := make([][]int, len(comps))
	for c, members := range comps {
		for _, v := range members {
			for _, w := range edges[v] {
				p := r.comp[w]
				if p == c {
					r.cyclic[c] = true // a member reaching itself, or another: a component of two has such an edge.
					continue
				}
				kids[p] = append(kids[p], c)
			}
		}
	}
	return kids
}

// number searches down kids from each component no other names as a child, and
// gives each component its post-order number, as hi, the least number of any
// component below it, as lo, and the first number its own search gives out, as
// first. The search is iterative, as a graph may be as deep as it is large.
func (r *intervals) number(kids [][]int) {
	r.lo, r.hi, r.first = make([]int, len(kids)), make([]int, len(kids)), make([]int, len(kids))
	isKid := make([]bool, len(kids))
	for _, ks := range kids {
		for _, k := range ks {
			isKid[k] = true
		}
	}
	visited := make([]bool, len(kids))
	next := 0
	var path []frame
	for root := range kids {
		if isKid[root] {
			continue
		}
		visited[root] = true
		r.first[root] = next
		path = append(path, frame{vertex: root})
		for len(path) > 0 {
			top := &path[len(path)-1]
			if top.next < len(kids[top.vertex]) {
				k := kids[top.vertex][top.next]
				top.next++
				if !visited[k] {
					visited[k] = true
					r.first[k] = next
					path = append(path, frame{vertex: k})
				}
				continue
			}
			c := top.vertex
			path = path[:len(path)-1]
			r.close(c, kids[c], next)
			next++
		}
	}
}

// close numbers component c, whose kids are all numbered already: the
// condensation has no cycle, so a kid the search met before has been closed.
func (r *intervals) close(c int, kids []int, number int) {
	r.hi[c], r.lo[c] = number, number
	for _, k := range kids {
		r.lo[c] = min(r.lo[c], r.lo[k])
	}
}

// below reports whether vertex t reaches vertex b, with settled false when the
// intervals cannot say: t's interval lies inside b's, but a search before b's
// own numbered it, so it may lie on no path down from b. That needs a vertex
// below b to have an edge to one that is neither b nor below it, so in a forest
// every pair is settled.
func (r *intervals) below(t, b int) (sub, settled bool) {
	ct, cb := r.comp[t], r.comp[b]
	if ct == cb {
		return r.cyclic[ct], true
	}
	if r.lo[ct] < r.lo[cb] || r.hi[ct] >= r.hi[cb] {
		return false, true
	}
	return true, r.hi[ct] >= r.first[cb]
}
