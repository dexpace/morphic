// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath

import (
	"context"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// SelectHolding is Select with a nodelist bounded by results rather than
// MaxResults, which a test reaches with a small tree.
func (q Query) SelectHolding(ctx context.Context, root *yaml.Node, results int) ([]Located, error) {
	b := budget.New(budget.Unbounded())
	e := &evaluator{view: nodeview.New(), steps: &b, results: results, root: root}
	return e.run(ctx, q.segments, Located{Node: root})
}

// IsNumber reports whether text reads as a number.
func IsNumber(text string) bool {
	_, ok := newNumber(text)
	return ok
}

// Translate is translate, reporting whether the pattern reads.
func Translate(pattern string) bool {
	_, ok := translate(pattern)
	return ok
}

// ReadBytes and CompareBytes are the bytes of a scalar reading it, and
// comparing it, costs a step.
const (
	ReadBytes    = readBytes
	CompareBytes = compareBytes
)
