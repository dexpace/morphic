// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath

import (
	"context"
	"errors"
	"fmt"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// MaxResults bounds the nodes the nodelists of one evaluation hold at once:
// the list a segment reads and the one it builds, and those of every query a
// filter it is in evaluates. Each node costs a step, so the steps bound them
// too, but a step is cheap and a node held is not: this bounds the nodes a
// query holds however deep its filters nest. It does not count the links of
// their paths, which nested descendant segments build in proportion to their
// steps, so such a query can hold gigabytes within its budget (GitHub #1960).
const MaxResults = 1 << 22

var (
	// ErrSteps reports an evaluation that spent its budget of steps.
	ErrSteps = errors.New("jsonpath: the query spends more steps than its budget holds")
	// ErrResults reports nodelists holding more than MaxResults nodes at once.
	ErrResults = fmt.Errorf("jsonpath: the query holds more than %d nodes in its nodelists at once", MaxResults)
	// ErrMergeBound reports a mapping whose `<<` merges run deeper than the view
	// expands (nodeview.MergeDepthLimit), so its members are not all known.
	ErrMergeBound = fmt.Errorf("jsonpath: a mapping merges through `<<` deeper than %d levels", nodeview.MergeDepthLimit)
)

// Query is one RFC 9535 query, parsed. It holds no state of an evaluation, so
// one Query may be evaluated any number of times, at once.
type Query struct {
	segments []segment
	notes    []PatternNote
}

// PatternNote is a pattern a query hands match or search as a literal and what
// a writer should be told of it: that it matches no string, Dead, so the call
// is false wherever it is evaluated, and why; or else that it writes `^` or `$`
// as an anchor where RFC 9485's grammar reads the character.
type PatternNote struct {
	Pattern, Reason string
	Dead            bool
}

// PatternNotes returns what q's literal patterns call for, one note a
// pattern at most, in the order written.
func (q Query) PatternNotes() []PatternNote { return slices.Clone(q.notes) }

// segment is one child or descendant segment and its selectors. singular
// reports a segment a singular query may hold: one name or index selector,
// written with no blank space.
type segment struct {
	selectors  []selector
	descendant bool
	singular   bool
}

// selectorKind is the kind of a selector.
type selectorKind uint8

const (
	selName selectorKind = iota
	selWildcard
	selIndex
	selSlice
	selFilter
)

// selector is one selector of a segment: the field its kind names is set.
type selector struct {
	filter logical
	name   string
	slice  slice
	index  int
	kind   selectorKind
}

// slice is a slice selector's bounds; start and end are set where written,
// and step is 1 where not.
type slice struct {
	start, end, step int
	hasStart, hasEnd bool
}

// Located is one node of a result: its normalized path and the node there,
// an alias followed.
type Located struct {
	Path Path
	Node *yaml.Node
}

// Select evaluates q over the JSON value root stands for: an alias reads as
// what it names and a merged member as a member of the mapping merging it
// (view). Every node visited, compared or returned spends a step of steps;
// ErrSteps reports them spent, ErrMergeBound a mapping whose merges pass the
// levels view expands. A tree whose aliases close a cycle has no end, which
// only a bounded budget stops.
func (q Query) Select(ctx context.Context, view *nodeview.View, root *yaml.Node, steps *budget.Budget) ([]Located, error) {
	if view == nil || steps == nil {
		return nil, errors.New("jsonpath: an evaluation needs a view and a budget")
	}
	root = nodeview.Deref(nodeview.DocumentRoot(root))
	if root == nil {
		return nil, errors.New("jsonpath: the tree holds no value to query")
	}
	evaluatorValue := &evaluator{view: view, steps: steps, results: MaxResults, root: root}
	return evaluatorValue.run(ctx, q.segments, Located{Node: root})
}
