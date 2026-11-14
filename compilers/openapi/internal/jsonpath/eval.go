// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath

import (
	"context"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// reader reads the value a tree stands for within a budget: the view it
// reads mappings through, the steps it spends and the most nodes a nodelist
// holds. It compares values too, which is reading two at once.
type reader struct {
	view  *nodeview.View
	steps *budget.Budget
	// asked counts the steps since the context was last asked.
	asked int
	// results bounds a nodelist: MaxResults.
	results int
	// scalars holds each scalar node of the document read so far, as read.
	scalars map[*yaml.Node]scalar
}

// evaluator is one evaluation of a query: what it reads with, the root an
// absolute query starts at, and the patterns read from the document it has
// compiled.
type evaluator struct {
	reader
	root     *yaml.Node
	patterns map[patternKey]pattern
}

// spend charges n steps, and asks the context once every nodeview.AskEvery.
func (r *reader) spend(ctx context.Context, n int) error {
	if !r.steps.Spend(n) {
		return ErrSteps
	}
	if r.asked += n; r.asked >= nodeview.AskEvery {
		r.asked = 0
		return ctx.Err()
	}
	return nil
}

// emit appends l to the nodelist out, a step for the node returned.
func (r *reader) emit(ctx context.Context, out []Located, l Located) ([]Located, error) {
	if err := r.spend(ctx, 1); err != nil {
		return nil, err
	}
	if len(out) >= r.results {
		return nil, ErrResults
	}
	return append(out, l), nil
}

// members returns the members of the mapping n as the view reads them, a
// step for each, spent before any is walked: a mapping's `<<` merges make its
// members cost more than its text, and each read of them is charged. A view
// a read has taken past its merge depth refuses, so the expansion runs once.
func (r *reader) members(ctx context.Context, n *yaml.Node) ([]nodeview.Pair, error) {
	pairs := r.view.MappingPairs(n)
	if r.view.Exhausted() {
		return nil, ErrMergeBound
	}
	if err := r.spend(ctx, len(pairs)); err != nil {
		return nil, err
	}
	return pairs, nil
}

// run applies segments in order to the nodelist holding from.
func (e *evaluator) run(ctx context.Context, segments []segment, from Located) ([]Located, error) {
	list := []Located{from}
	for _, s := range segments {
		var next []Located
		for _, in := range list {
			var err error
			if s.descendant {
				next, err = e.descend(ctx, s.selectors, in, next)
			} else {
				next, err = e.selectAll(ctx, s.selectors, in, next)
			}
			if err != nil {
				return nil, err
			}
		}
		list = next
	}
	return list, nil
}

// descend applies selectors to in and to each of its descendants, a node
// before its descendants and children in order, appending to out. The walk
// keeps a stack of its own, so a document of any depth costs no goroutine
// stack; each node visited is a step.
func (e *evaluator) descend(ctx context.Context, selectors []selector, in Located, out []Located) ([]Located, error) {
	stack := []Located{in}
	for len(stack) > 0 {
		at := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err := e.spend(ctx, 1); err != nil {
			return nil, err
		}
		var err error
		if out, err = e.selectAll(ctx, selectors, at, out); err != nil {
			return nil, err
		}
		if stack, err = e.pushChildren(ctx, at, stack); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// pushChildren pushes the children of at onto stack, the last first, so they
// are popped in order.
func (r *reader) pushChildren(ctx context.Context, at Located, stack []Located) ([]Located, error) {
	switch at.Node.Kind {
	case yaml.MappingNode:
		pairs, err := r.members(ctx, at.Node)
		if err != nil {
			return nil, err
		}
		for _, p := range slices.Backward(pairs) {
			if p.Val != nil {
				stack = append(stack, Located{Path: at.Path.child(p.Key), Node: p.Val})
			}
		}
	case yaml.SequenceNode:
		for i, c := range slices.Backward(at.Node.Content) {
			if v := nodeview.Deref(c); v != nil {
				stack = append(stack, Located{Path: at.Path.element(i), Node: v})
			}
		}
	default:
	}
	return stack, nil
}

// children returns the children of in: an object's member values, or an
// array's elements, each with its path.
func (r *reader) children(ctx context.Context, in Located) ([]Located, error) {
	stack, err := r.pushChildren(ctx, in, nil)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(stack)-1; i < j; i, j = i+1, j-1 {
		stack[i], stack[j] = stack[j], stack[i]
	}
	return stack, nil
}

// selectAll applies each selector to in, in order, appending to out.
func (e *evaluator) selectAll(ctx context.Context, selectors []selector, in Located, out []Located) ([]Located, error) {
	for i := range selectors {
		var err error
		if out, err = e.selectOne(ctx, &selectors[i], in, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// selectOne applies s to in, appending what it selects to out.
func (e *evaluator) selectOne(ctx context.Context, s *selector, in Located, out []Located) ([]Located, error) {
	switch s.kind {
	case selName:
		return e.byName(ctx, s.name, in, out)
	case selIndex:
		return e.byIndex(ctx, s.index, in, out)
	case selSlice:
		return e.bySlice(ctx, s.slice, in, out)
	case selWildcard:
		return e.every(ctx, in, out, nil)
	default:
		return e.every(ctx, in, out, s.filter)
	}
}

// byName selects the member name of an object.
func (e *evaluator) byName(ctx context.Context, name string, in Located, out []Located) ([]Located, error) {
	if in.Node.Kind != yaml.MappingNode {
		return out, nil
	}
	if _, err := e.members(ctx, in.Node); err != nil {
		return nil, err
	}
	v := e.view.ChildByToken(in.Node, name)
	if v == nil {
		return out, nil
	}
	return e.emit(ctx, out, Located{Path: in.Path.child(name), Node: v})
}

// byIndex selects the element at i of an array, counting from its end where i
// is negative.
func (e *evaluator) byIndex(ctx context.Context, i int, in Located, out []Located) ([]Located, error) {
	if in.Node.Kind != yaml.SequenceNode {
		return out, nil
	}
	n := len(in.Node.Content)
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		return out, nil
	}
	v := nodeview.Deref(in.Node.Content[i])
	if v == nil {
		return out, nil
	}
	return e.emit(ctx, out, Located{Path: in.Path.element(i), Node: v})
}

// bySlice selects the elements of an array s selects, as RFC 9535 section
// 2.3.4.2.2 bounds them: each index from the lower bound to the upper by
// step, or from the upper to the lower for a negative step. Each iteration
// selects one element, so the loop is bounded by the array.
func (e *evaluator) bySlice(ctx context.Context, s slice, in Located, out []Located) ([]Located, error) {
	if in.Node.Kind != yaml.SequenceNode || s.step == 0 {
		return out, nil
	}
	lower, upper := s.bounds(len(in.Node.Content))
	var err error
	if s.step > 0 {
		for i := lower; i < upper && err == nil; i += s.step {
			out, err = e.element(ctx, in, i, out)
		}
		return out, err
	}
	for i := upper; lower < i && err == nil; i += s.step {
		out, err = e.element(ctx, in, i, out)
	}
	return out, err
}

// element selects the element at i of the array in, which holds it.
func (e *evaluator) element(ctx context.Context, in Located, i int, out []Located) ([]Located, error) {
	v := nodeview.Deref(in.Node.Content[i])
	if v == nil {
		return out, nil
	}
	return e.emit(ctx, out, Located{Path: in.Path.element(i), Node: v})
}

// bounds returns the lower and upper bounds s gives an array of n elements.
func (s slice) bounds(n int) (lower, upper int) {
	normal := func(i int) int {
		if i >= 0 {
			return i
		}
		return n + i
	}
	if s.step > 0 {
		start, end := 0, n
		if s.hasStart {
			start = normal(s.start)
		}
		if s.hasEnd {
			end = normal(s.end)
		}
		return min(max(start, 0), n), min(max(end, 0), n)
	}
	start, end := n-1, -n-1
	if s.hasStart {
		start = normal(s.start)
	}
	if s.hasEnd {
		end = normal(s.end)
	}
	return min(max(end, -1), n-1), min(max(start, -1), n-1)
}

// every selects each child of in, or, given a filter, each child it holds
// for. Each child a filter tests is a step.
func (e *evaluator) every(ctx context.Context, in Located, out []Located, filter logical) ([]Located, error) {
	children, err := e.children(ctx, in)
	if err != nil {
		return nil, err
	}
	for _, c := range children {
		if filter != nil {
			if err := e.spend(ctx, 1); err != nil {
				return nil, err
			}
			held, err := filter.holds(ctx, e, c)
			if err != nil {
				return nil, err
			}
			if !held {
				continue
			}
		}
		if out, err = e.emit(ctx, out, c); err != nil {
			return nil, err
		}
	}
	return out, nil
}
