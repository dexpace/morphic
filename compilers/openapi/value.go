package openapi

import (
	"fmt"
	"strconv"
	"strings"

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
		// plain, unquoted !!str node; capture it as a number, not a string.
		// Quoted strings are never plain, so a genuine numeric string ("123")
		// stays a string. This classification is by wire spelling alone,
		// independent of any surrounding schema type (the Type-vs-Value split).
		if node.Style == 0 {
			if num, err := ir.NewBigVal(node.Value); err == nil {
				return ir.Value{Kind: ir.ValueNumber, Num: num}, nil
			}
		}
		return ir.Value{Kind: ir.ValueString, Str: node.Value}, nil
	case "!!timestamp":
		// YAML 1.1 resolves a plain date/time scalar (e.g. 2021-01-01) to
		// !!timestamp, but OpenAPI's data model is JSON, where a date is only
		// expressible as a string; the tag is a YAML parsing artifact, not a
		// JSON Schema type. Keep the verbatim source spelling (2021-1-1 stays
		// 2021-1-1), classified by wire spelling alone — same rule as !!str
		// above, independent of any surrounding schema type.
		return ir.Value{Kind: ir.ValueString, Str: node.Value}, nil
	case "!!int", "!!float":
		num, err := numericLiteral(node)
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

// numericLiteral converts a numeric-tagged YAML scalar into the BigVal of the
// value YAML resolved it to, which is not what its source text means in base
// 10. YAML takes an !!int's base from its prefix and drops "_" separators, so
// 0644 is 420 and 0x1f is 31; passing that spelling to NewBigVal — a base-10
// decimal literal — reads 0644 back as 644 instead.
//
// Only !!int is decoded. Every !!int yaml.v3 resolves fits int64 or uint64 (a
// wider integer resolves to !!float instead), so the decode is exact. A !!float
// keeps its text, because decoding one would round it through float64 and lose
// the arbitrary precision BigVal exists to preserve; only its separators are
// dropped.
func numericLiteral(node *yaml.Node) (ir.BigVal, error) {
	if node == nil {
		return "", fmt.Errorf("openapi: nil numeric node")
	}
	switch node.Tag {
	case "!!int":
		return decodeIntLiteral(node)
	case "!!float":
		return ir.NewBigVal(strings.ReplaceAll(node.Value, "_", ""))
	default:
		// A quoted or explicitly tagged spelling carries no YAML-assigned base,
		// so its text is already the decimal literal it looks like.
		return ir.NewBigVal(node.Value)
	}
}

// decodeIntLiteral renders an !!int node as its exact decimal literal. The
// signed range is tried first so a negative value is never read back as a large
// positive one; the unsigned range covers the int64..uint64 band above it.
func decodeIntLiteral(node *yaml.Node) (ir.BigVal, error) {
	var signed int64
	if err := node.Decode(&signed); err == nil {
		return ir.NewBigVal(strconv.FormatInt(signed, 10))
	}
	var unsigned uint64
	if err := node.Decode(&unsigned); err != nil {
		return "", fmt.Errorf("openapi: integer literal %q: %w", node.Value, err)
	}
	return ir.NewBigVal(strconv.FormatUint(unsigned, 10))
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
