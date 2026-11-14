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
func settle(ctx context.Context, doc *Document, r respelling) error {
	s := settler{respelling: r}
	if doc.Directive != nil && doc.Directive.Minor == 1 {
		s.keys = map[*yaml.Node]bool{}
	}
	out := nodeview.Walk{Nodes: math.MaxInt, Visit: s.visit}.Run(ctx, doc.Root)
	if out.Cut {
		return cmp.Or(ctx.Err(), errCut)
	}
	doc.Nodes, doc.Legacy = out.Visited, s.found
	return nil
}

// visit settles n: it records what YAML 1.1 reads otherwise in it, restores
// its text, and resolves its children, which it alone knows to be keys or not.
func (s *settler) visit(n *yaml.Node, _ int) nodeview.Step {
	if s.keys != nil {
		s.found = s.legacy(s.found, n)
	}
	s.respelling.restore(n)
	for i, c := range n.Content {
		key := n.Kind == yaml.MappingNode && i%2 == 0
		if key && s.keys != nil {
			s.keys[c] = true
		}
		resolve(c, key)
	}
	return nodeview.Descend
}

// resolve tags the scalar n, when the parser left it plain, as YAML 1.2's core
// schema reads it (docs/parser-library.md#scalar-resolution): a number the
// core schema does not spell is a string, and so are a date and a `<<` that is
// no key (docs/parser-library.md#undecoded-scalar). A key keeps the parser's
// tag but a date's. Its text is never changed.
//
// A core integer the parser tags a float, 089 or one past 64 bits, keeps its
// tag: the model decodes no field from an !!int it cannot hold, and every
// reader of a number reads both tags in base ten.
func resolve(n *yaml.Node, key bool) {
	if n == nil || n.Kind != yaml.ScalarNode || n.Style != 0 {
		return
	}
	switch n.ShortTag() {
	case "!!timestamp":
		n.Tag = "!!str"
	case "!!merge":
		if !key {
			n.Tag = "!!str"
		}
	case "!!int":
		if !key && !coreInt(n.Value) {
			n.Tag = "!!str"
		}
	case "!!float":
		if !key && !coreFloat(n.Value) {
			n.Tag = "!!str"
		}
	}
}

// coreInt reports whether text is an integer of the core schema:
// [-+]?[0-9]+, 0o[0-7]+ or 0x[0-9a-fA-F]+.
func coreInt(text string) bool {
	if octal, ok := strings.CutPrefix(text, "0o"); ok {
		return digits(octal, 8)
	}
	if hex, ok := strings.CutPrefix(text, "0x"); ok {
		return digits(hex, 16)
	}
	return digits(unsigned(text), 10)
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
