// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package doctree

import (
	"cmp"
	"context"
	"math"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
)

// settler is the one walk of a parsed YAML tree: it restores what respell put
// in place, counts the nodes, resolves each plain scalar by the core schema
// and, in a document declaring YAML 1.1, records what YAML 1.1 reads otherwise.
type settler struct {
	respelling respelling
	// keys holds each mapping key met, which legacy needs to tell from a
	// value, or is nil when no Legacy is recorded.
	keys  map[*yaml.Node]bool
	found []Legacy
}

// settle walks doc's tree once (settler), setting doc.Nodes and doc.Legacy. The
// walk is bounded by the tree, which the parser built from bytes in hand, each
// node visited once and an alias as itself; it stops only for ctx.
func settle(ctx context.Context, doc *ParsedYAMLDocument, respellingValue respelling) error {
	settlerValue := settler{respelling: respellingValue}
	if doc.Directive != nil && doc.Directive.Minor == 1 {
		settlerValue.keys = map[*yaml.Node]bool{}
	}
	out := nodeview.Walk{Nodes: math.MaxInt, Visit: settlerValue.visit}.Run(ctx, doc.Root)
	if out.Cut {
		return cmp.Or(ctx.Err(), errCut)
	}
	doc.Nodes, doc.Legacy = out.Visited, settlerValue.found
	return nil
}

// visit settles node: it records what YAML 1.1 reads otherwise in it, restores
// its text, and resolves its children, which it alone knows to be keys or not.
func (s *settler) visit(node *yaml.Node, _ int) nodeview.Step {
	if s.keys != nil {
		s.found = s.legacy(s.found, node)
	}
	s.respelling.restore(node)
	for i, child := range node.Content {
		key := node.Kind == yaml.MappingNode && i%2 == 0
		if key && s.keys != nil {
			s.keys[child] = true
		}
		resolve(child, key)
	}
	return nodeview.Descend
}

// resolve tags the scalar node, when the parser left it plain, as YAML 1.2's
// core schema reads it (docs/parser-library.md#scalar-resolution): a number the
// core schema does not spell is a string, and so are a date and a `<<` that is
// no key (docs/parser-library.md#undecoded-scalar). A key keeps the parser's
// tag but a date's. Its text is never changed.
//
// A core integer the parser tags a float, 089 or one past 64 bits, keeps its
// tag: the model decodes no field from an !!int it cannot hold. Its
// integrality is its text's, and no reader may take it from the tag.
func resolve(node *yaml.Node, key bool) {
	if node == nil || node.Kind != yaml.ScalarNode || node.Style != 0 {
		return
	}
	switch node.ShortTag() {
	case "!!timestamp":
		node.Tag = "!!str"
	case "!!merge":
		if !key {
			node.Tag = "!!str"
		}
	case "!!int":
		if !key && !coreInt(node.Value) {
			node.Tag = "!!str"
		}
	case "!!float":
		if !key && !coreFloat(node.Value) {
			node.Tag = "!!str"
		}
	}
}

// coreInt reports whether text is an integer of the core schema
// (nodeview.ParseCoreInteger).
func coreInt(text string) bool {
	_, ok := nodeview.ParseCoreInteger(text)
	return ok
}

// coreFloat reports whether text is a float of the core schema:
// [-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?, an infinity or NaN.
func coreFloat(text string) bool {
	switch unsigned(text) {
	case ".inf", ".Inf", ".INF":
		return true
	}
	switch text {
	case ".nan", ".NaN", ".NAN":
		return true
	}
	mantissa := unsigned(text)
	if e := strings.IndexAny(mantissa, "eE"); e >= 0 {
		if !digits(unsigned(mantissa[e+1:]), 10) {
			return false
		}
		mantissa = mantissa[:e]
	}
	whole, fraction, dotted := strings.Cut(mantissa, ".")
	switch {
	case !dotted:
		return digits(whole, 10)
	case whole == "":
		return digits(fraction, 10)
	default:
		return digits(whole, 10) && (fraction == "" || digits(fraction, 10))
	}
}

// unsigned is text without the one sign it may open with.
func unsigned(text string) string {
	if text != "" && (text[0] == '+' || text[0] == '-') {
		return text[1:]
	}
	return text
}

// digits reports whether text is one or more digits of base, which is 8, 10 or
// 16.
func digits(text string, base int) bool {
	if text == "" {
		return false
	}
	for _, c := range []byte(text) {
		if digitValue(c) >= base {
			return false
		}
	}
	return true
}

// digitValue is the value of the hexadecimal digit c, or 16 for a byte that is
// none.
func digitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return 16
	}
}
