// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irfacts

// pairs collects values each under a key from 0 to n, in the order added, for
// grouped to hand back as one list per key.
type pairs[T any] struct {
	keys   []int
	values []T
}

// pairsOf returns pairs room for at most most values, so that collecting them
// allocates once, in proportion to the graph, and never grows.
func pairsOf[T any](most int) pairs[T] {
	return pairs[T]{keys: make([]int, 0, most), values: make([]T, 0, most)}
}

// add records value under key.
func (p *pairs[T]) add(key int, value T) {
	p.keys = append(p.keys, key)
	p.values = append(p.values, value)
}

// grouped returns, for each key from 0 to n, the values added under it in the
// order added, each list a slice of one backing array sized by a counting pass:
// a build allocates per index rather than per list. A key added nothing has an
// empty list.
func (p *pairs[T]) grouped(n int) [][]T {
	start := make([]int, n+1)
	for _, k := range p.keys {
		start[k+1]++
	}
	for k := range n {
		start[k+1] += start[k]
	}
	backing := make([]T, len(p.values))
	filled := make([]int, n)
	for i, k := range p.keys {
		backing[start[k]+filled[k]] = p.values[i]
		filled[k]++
	}
	out := make([][]T, n)
	for k := range n {
		out[k] = backing[start[k]:start[k+1]:start[k+1]]
	}
	return out
}
