// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package value

import (
	"fmt"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// maxValueDepth bounds FromNode recursion (styleguide bounded-recursion
// rule). Values nested deeper than this are a pathological input, not a spec
// the compiler is expected to lower.
const maxValueDepth = 128

// FromNode converts a YAML node into an ir.Value. Numeric literals are kept as
// their exact source text (the no-float64 escape), object members are in the
// order the parser's model holds them, a key written twice at the copy it reads
// (docs/parser-library.md#duplicate-key), and aliases are followed.
//
// The error reports a spec-level problem: a value nested past the depth cap, a
// scalar Classify refuses or no ir.Value holds (`!acme`), or a mapping merging
// past nodeview.MergeDepthLimit. The caller decides what one means, a bad
// bound an error, an example that will not convert a warning, and holds the
// provenance (GitHub #169).
func FromNode(node *yaml.Node) (ir.Value, error) {
	converterValue := converter{view: nodeview.New()}
	return converterValue.value(node, 0)
}

// converter is one FromNode conversion: the view every mapping in the value is
// read through, shared so a merge chain is expanded once.
type converter struct {
	view *nodeview.View
}

// value converts node, depth levels below the value FromNode was handed.
func (c converter) value(node *yaml.Node, depth int) (ir.Value, error) {
	if depth > maxValueDepth {
		return ir.Value{}, fmt.Errorf("openapi: value nesting exceeds %d", maxValueDepth)
	}
	if node == nil {
		return ir.Value{Kind: ir.ValueNull}, nil
	}
	// A value the load phase read as left out is converted as the source wrote
	// it (nodeview.Unread).
	node = nodeview.Written(node)
	switch node.Kind {
	case yaml.AliasNode:
		return c.value(node.Alias, depth+1)
	case yaml.ScalarNode:
		return scalarValue(node)
	case yaml.SequenceNode:
		return c.sequence(node, depth)
	case yaml.MappingNode:
		return c.mapping(node, depth)
	default:
		return ir.Value{}, fmt.Errorf("openapi: unsupported yaml node kind %d", node.Kind)
	}
}

// scalarValue converts a scalar YAML node into an ir.Value by its
// classification. A timestamp keeps its text: OpenAPI's data model is JSON,
// where a date is only expressible as a string, so `2021-1-1` stays
// `2021-1-1`. A scalar under a tag YAML gives no type has no ir.Value to hold
// it.
func scalarValue(node *yaml.Node) (ir.Value, error) {
	scalar, err := Classify(node)
	if err != nil {
		return ir.Value{}, err
	}
	switch scalar.Kind {
	case ScalarNull:
		return ir.Value{Kind: ir.ValueNull}, nil
	case ScalarBool:
		return ir.Value{Kind: ir.ValueBool, Bool: scalar.Bool}, nil
	case ScalarNumber:
		return ir.Value{Kind: ir.ValueNumber, Number: scalar.Num}, nil
	case ScalarString, ScalarTimestamp:
		return ir.Value{Kind: ir.ValueString, String: scalar.Text}, nil
	case ScalarBinary:
		return ir.Value{Kind: ir.ValueBytes, Bytes: scalar.Bytes}, nil
	default:
		// ScalarTagged, and so any kind added later: refused, never guessed at.
		return ir.Value{}, fmt.Errorf("openapi: unsupported scalar tag %q", node.Tag)
	}
}

// sequence converts a YAML sequence into an ordered ValueList.
func (c converter) sequence(node *yaml.Node, depth int) (ir.Value, error) {
	list := make([]ir.Value, 0, len(node.Content))
	for _, child := range node.Content {
		value, err := c.value(child, depth+1)
		if err != nil {
			return ir.Value{}, err
		}
		list = append(list, value)
	}
	return ir.Value{Kind: ir.ValueList, List: list}, nil
}

// errMergedPastBound refuses a mapping whose `<<` merges run past what the
// view expands, so a member the source does write would be missing.
var errMergedPastBound = fmt.Errorf("openapi: mapping merges through `<<` deeper than %d levels", nodeview.MergeDepthLimit)

// mapping converts a YAML mapping into a ValueObject holding the members the
// parser reads (nodeview.View.MappingPairs), so a key written twice is read at
// the copy the model reads, as a payload kept verbatim is, and load's rule
// table reports the repeat (GitHub #1663).
func (c converter) mapping(node *yaml.Node, depth int) (ir.Value, error) {
	pairs := c.view.MappingPairs(node)
	if c.view.Exhausted() {
		return ir.Value{}, errMergedPastBound
	}
	fields := make([]ir.Field, 0, len(pairs))
	for _, pair := range pairs {
		val, err := c.value(pair.Val, depth+1)
		if err != nil {
			return ir.Value{}, err
		}
		fields = append(fields, ir.Field{Name: pair.Key, Value: val})
	}
	return ir.Value{Kind: ir.ValueObject, Object: fields}, nil
}
