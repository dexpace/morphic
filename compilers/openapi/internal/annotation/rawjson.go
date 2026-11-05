// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// RawFromNode converts a YAML node to the canonical JSON an Unmodeled entry
// holds. An absent node yields (nil, nil): no construct was there. One that
// cannot be represented yields an error, so no caller announces a preservation
// that never happened (GitHub #144).
//
// A mapping holds the members the parser reads (nodeview.View.MappingPairs).
// Conversion fails on a mapping that writes a key twice (GitHub #1072) or
// merges past nodeview.MergeDepthLimit, a scalar value.Classify refuses,
// invalid UTF-8, and an alias that cycles or expands past the node budget.
// Numeric literals stay exact (GitHub #32).
func RawFromNode(node *yaml.Node) (ir.RawValue, error) {
	if node == nil {
		return nil, nil
	}
	conv := newRawConv()
	data, err := conv.node(nil, node, 0)
	if err != nil {
		return nil, fmt.Errorf("render yaml node as json: %w", err)
	}
	return data, nil
}

// Bounds on one conversion, a backstop. A YAML node is a graph, not a tree: an
// alias may point at an ancestor, and a chain of aliases each naming the one
// above it expands multiplicatively. scan's cycle detector refuses both shapes
// long before a node reaches here, at far tighter bounds, so the walk is
// bounded on its own terms rather than by what runs before it.
//
// maxRawDepth is yaml.v3's own parse-time nesting cap, so it binds only on
// input the parser could not have produced by nesting alone: an alias cycle.
const (
	maxRawDepth = 10000
	maxRawNodes = 1 << 20
)

// rawConv renders one YAML node as canonical JSON. It carries the node budget
// shared across the whole walk and the view every mapping is read through;
// depth is a parameter because it is a property of the path rather than of the
// conversion.
//
// A scalar renders to JSON text directly, never through a Go value: decoding a
// number into `any` rounds it through float64, the bug this file exists to close
// (GitHub #32). Every node appends to its parent's buffer, so a render is
// linear in the node's size.
type rawConv struct {
	nodes budget.Budget
	view  *nodeview.View
}

// newRawConv returns a conversion with the whole of maxRawNodes to spend.
func newRawConv() rawConv {
	return rawConv{nodes: budget.New(budget.AtMost(maxRawNodes)), view: nodeview.New()}
}

// node appends any YAML node to dst as canonical JSON. On an error what it
// returns is no JSON, and the caller discards it.
func (c *rawConv) node(dst jsontext.Value, n *yaml.Node, depth int) (jsontext.Value, error) {
	if n == nil {
		return nil, errors.New("nil yaml node")
	}
	if depth > maxRawDepth {
		return nil, fmt.Errorf("nesting exceeds %d", maxRawDepth)
	}
	if !c.nodes.Spend(1) {
		return nil, fmt.Errorf("expands past %d nodes", c.nodes.Limit().Cap())
	}

	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) != 1 {
			return nil, fmt.Errorf("document node holds %d children", len(n.Content))
		}
		return c.node(dst, n.Content[0], depth+1)
	case yaml.AliasNode:
		if n.Alias == nil {
			return nil, fmt.Errorf("alias %q resolves to nothing", n.Value)
		}
		return c.node(dst, n.Alias, depth+1)
	case yaml.ScalarNode:
		return appendScalar(dst, n)
	case yaml.SequenceNode:
		return c.sequence(dst, n, depth)
	case yaml.MappingNode:
		return c.mapping(dst, n, depth)
	default:
		return nil, fmt.Errorf("unsupported yaml node kind %d", n.Kind)
	}
}

// appendScalar appends a scalar node to dst as value.Classify reads it, the
// classification an ir.Value is lowered from too, so a scalar kept verbatim and
// one lowered are never two kinds (GitHub #783). What is kept differs only
// where JSON has no type for the kind: a timestamp, a !!binary and a scalar
// under the document's own tag keep the text the source wrote, because the
// resolved form is derivable from the spelling and the spelling is not
// derivable from the resolved form (GitHub #242).
func appendScalar(dst jsontext.Value, n *yaml.Node) (jsontext.Value, error) {
	s, err := value.Classify(n)
	if err != nil {
		return nil, err
	}
	switch s.Kind {
	case value.ScalarNull:
		return append(dst, "null"...), nil
	case value.ScalarBool:
		return strconv.AppendBool(dst, s.Bool), nil
	case value.ScalarNumber:
		return spliceNumber(dst, n.Value, string(s.Num))
	default:
		// A string, and the kinds JSON names only as the text they were written as.
		return appendString(dst, s.Text)
	}
}

// spliceNumber appends num, an already-validated number value.Classify read, to
// dst as raw JSON, refusing it if it is not a JSON number. appendScalar is the
// one caller that splices a numeric literal straight into a document rather
// than an ir.Value field, so BigVal's contract (its text always renders as a
// JSON-valid number) is checked here.
//
// NewBigVal enforces that contract itself, so the check is unreachable through
// appendScalar and tests call this directly. It stays because dropping it would
// turn a future regression in BigVal into a document silently carrying a
// construct JSON cannot name (GitHub #45).
func spliceNumber(dst jsontext.Value, source, num string) (jsontext.Value, error) {
	if !jsontext.Value(num).IsValid() {
		return nil, fmt.Errorf("numeric literal %q renders as %q, which is not JSON", source, num)
	}
	return append(dst, num...), nil
}

// sequence appends a YAML sequence to dst as a JSON array, in source order.
func (c *rawConv) sequence(dst jsontext.Value, n *yaml.Node, depth int) (jsontext.Value, error) {
	dst = append(dst, '[')
	for i, child := range n.Content {
		if i > 0 {
			dst = append(dst, ',')
		}
		var err error
		if dst, err = c.node(dst, child, depth+1); err != nil {
			return nil, err
		}
	}
	return append(dst, ']'), nil
}

// mapping appends a YAML mapping to dst as a JSON object of the members the
// parser reads, each key once (MappingPairs), in the byte order of their keys,
// as the IR's determinism invariant asks of every map it serializes. Of several
// members that fail to convert, the first by key is reported.
//
// A key written twice, by the mapping or a mapping it merges, is refused, as
// yaml.v3 refuses it by default: the member would hold one copy and drop the
// other. A key written through an alias is the key it names, so it repeats
// one written plainly (GitHub #1072).
func (c *rawConv) mapping(dst jsontext.Value, n *yaml.Node, depth int) (jsontext.Value, error) {
	if key, in, repeated := c.view.Repeat(n); repeated {
		return nil, repeatedKeyError(in, key)
	}
	pairs := c.view.MappingPairs(n)
	if c.view.Exhausted() {
		return nil, fmt.Errorf("mapping merges through `<<` deeper than %d levels", nodeview.MergeDepthLimit)
	}
	sorted := slices.SortedFunc(slices.Values(pairs), func(a, b nodeview.Pair) int {
		return strings.Compare(a.Key, b.Key)
	})

	dst = append(dst, '{')
	for i, p := range sorted {
		if i > 0 {
			dst = append(dst, ',')
		}
		var err error
		if dst, err = appendString(dst, p.Key); err != nil {
			return nil, fmt.Errorf("mapping key: %w", err)
		}
		dst = append(dst, ':')
		if dst, err = c.node(dst, p.Val, depth+1); err != nil {
			return nil, err
		}
	}
	return append(dst, '}'), nil
}

// repeatedKeyError names key, which the mapping n writes more than once, and
// the lines of its first two copies.
func repeatedKeyError(n *yaml.Node, key string) error {
	var lines []int
	for i := 0; i+1 < len(n.Content) && len(lines) < 2; i += 2 {
		if k, own := nodeview.KeyOf(n.Content[i]); own && k == key {
			lines = append(lines, n.Content[i].Line)
		}
	}
	return fmt.Errorf("mapping key %q already defined at line %d, and written again at line %d", key, lines[0], lines[1])
}

// appendString appends s to dst as a JSON string with RFC 8785's minimal
// escaping, matching the IR's canonical form. Text that is not valid UTF-8
// fails the conversion rather than reaching the IR as U+FFFD, which a source
// could also have written itself.
func appendString(dst jsontext.Value, s string) (jsontext.Value, error) {
	b, err := jsontext.AppendQuote(dst, s)
	if err != nil {
		return nil, fmt.Errorf("string %q: %w", s, err)
	}
	return b, nil
}
