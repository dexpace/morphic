// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath

import (
	"fmt"
	"strconv"
	"strings"
)

// Path is the location of one node of a result, from the root of the value a
// query read. It is a link to its parent's path, so a nodelist of n nodes holds
// n links however deep they lie. The zero Path is the root's.
type Path struct{ last *link }

// link is one step of a Path and the path to its holder.
type link struct {
	parent *link
	name   string
	index  int
	member bool
}

// Step is one step of a Path: the member Name of an object, or the element at
// Index of an array.
type Step struct {
	Name   string
	Index  int
	Member bool
}

// child returns the path of the member name of the node at p.
func (p Path) child(name string) Path {
	return Path{last: &link{parent: p.last, name: name, member: true}}
}

// element returns the path of the element at index i of the array at p.
func (p Path) element(i int) Path {
	return Path{last: &link{parent: p.last, index: i}}
}

// Steps returns p's steps from the root, in order.
func (p Path) Steps() []Step {
	depth := 0
	for l := p.last; l != nil; l = l.parent {
		depth++
	}
	steps := make([]Step, depth)
	for l := p.last; l != nil; l = l.parent {
		depth--
		steps[depth] = Step{Name: l.name, Index: l.index, Member: l.member}
	}
	return steps
}

// String returns p as RFC 9535 section 2.7 normalizes it: `$`, then each
// member name in single quotes and each array index in brackets, such as
// $['paths']['/a'][0].
func (p Path) String() string {
	var b strings.Builder
	b.WriteByte('$')
	for _, s := range p.Steps() {
		if !s.Member {
			b.WriteString("[" + strconv.Itoa(s.Index) + "]")
			continue
		}
		b.WriteString("['")
		writeNormalName(&b, s.Name)
		b.WriteString("']")
	}
	return b.String()
}

// writeNormalName writes name as a normalized path quotes it: a quote and a
// backslash escaped, the five control characters JSON names by a letter so
// named, and every other one below U+0020 as a \u escape in lower case.
func writeNormalName(b *strings.Builder, name string) {
	for _, r := range name {
		switch {
		case r == '\'' || r == '\\':
			b.WriteString(`\` + string(r))
		case r >= 0x20:
			b.WriteRune(r)
		default:
			if i := strings.IndexRune("\b\f\n\r\t", r); i >= 0 {
				b.WriteString(`\` + "bfnrt"[i:i+1])
				continue
			}
			fmt.Fprintf(b, `\u%04x`, r)
		}
	}
}
