package openapi

import (
	"fmt"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// maxValueDepth bounds valueFromNode recursion (styleguide bounded-recursion
// rule). Values nested deeper than this are a pathological input, not a spec
// the compiler is expected to lower.
const maxValueDepth = 128

// valueFromNode converts a YAML node into an ir.Value. Numeric literals are kept
// as their exact source text (the no-float64 escape), object member order is
// preserved, and alias nodes are followed. The Go error return is reserved for
// structurally impossible nodes (unknown kinds/tags, over-deep nesting); it
// never reports a spec-level problem.
func valueFromNode(node *yaml.Node) (ir.Value, error) {
	return valueFromNodeAt(node, 0)
}

// valueFromNodeAt is valueFromNode with an explicit recursion depth counter.
func valueFromNodeAt(node *yaml.Node, depth int) (ir.Value, error) {
	if depth > maxValueDepth {
		return ir.Value{}, fmt.Errorf("openapi: value nesting exceeds %d", maxValueDepth)
	}
	if node == nil {
		return ir.Value{Kind: ir.ValueNull}, nil
	}
	switch node.Kind {
	case yaml.AliasNode:
		return valueFromNodeAt(node.Alias, depth+1)
	case yaml.ScalarNode:
		return scalarValue(node)
	case yaml.SequenceNode:
		return sequenceValue(node, depth)
	case yaml.MappingNode:
		return mappingValue(node, depth)
	default:
		return ir.Value{}, fmt.Errorf("openapi: unsupported yaml node kind %d", node.Kind)
	}
}

// scalarValue converts a scalar YAML node into an ir.Value by its resolved tag.
func scalarValue(node *yaml.Node) (ir.Value, error) {
	switch node.Tag {
	case "!!null":
		return ir.Value{Kind: ir.ValueNull}, nil
	case "!!bool":
		var b bool
		if err := node.Decode(&b); err != nil {
			return ir.Value{}, fmt.Errorf("openapi: bool literal %q: %w", node.Value, err)
		}
		return ir.Value{Kind: ir.ValueBool, Bool: b}, nil
	case "!!str":
		// A numeric literal beyond float64 range (e.g. 1.8e308) resolves to a
		// plain, unquoted !!str node; capture it as the number it is, never a
		// string. Quoted strings are never plain, so a genuine numeric string
		// ("123") stays a string.
		if node.Style == 0 {
			if num, err := ir.NewBigVal(node.Value); err == nil {
				return ir.Value{Kind: ir.ValueNumber, Num: num}, nil
			}
		}
		return ir.Value{Kind: ir.ValueString, Str: node.Value}, nil
	case "!!int", "!!float":
		num, err := ir.NewBigVal(node.Value)
		if err != nil {
			return ir.Value{}, fmt.Errorf("openapi: numeric literal %q: %w", node.Value, err)
		}
		return ir.Value{Kind: ir.ValueNumber, Num: num}, nil
	case "!!binary":
		// yaml.v3 base64-decodes a !!binary node into a string (it rejects a
		// []byte target), so decode to string and carry the bytes from there.
		var raw string
		if err := node.Decode(&raw); err != nil {
			return ir.Value{}, fmt.Errorf("openapi: binary literal: %w", err)
		}
		return ir.Value{Kind: ir.ValueBytes, Bytes: []byte(raw)}, nil
	default:
		return ir.Value{}, fmt.Errorf("openapi: unsupported scalar tag %q", node.Tag)
	}
}

// sequenceValue converts a YAML sequence into an ordered ValueList.
func sequenceValue(node *yaml.Node, depth int) (ir.Value, error) {
	list := make([]ir.Value, 0, len(node.Content))
	for _, child := range node.Content {
		v, err := valueFromNodeAt(child, depth+1)
		if err != nil {
			return ir.Value{}, err
		}
		list = append(list, v)
	}
	return ir.Value{Kind: ir.ValueList, List: list}, nil
}

// mappingValue converts a YAML mapping into an ordered ValueObject. Mapping
// content is a flat [k0, v0, k1, v1, ...] slice, so member order is source order.
func mappingValue(node *yaml.Node, depth int) (ir.Value, error) {
	fields := make([]ir.Field, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		val, err := valueFromNodeAt(node.Content[i+1], depth+1)
		if err != nil {
			return ir.Value{}, err
		}
		fields = append(fields, ir.Field{Name: key.Value, Value: val})
	}
	return ir.Value{Kind: ir.ValueObject, Object: fields}, nil
}
