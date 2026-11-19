// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Path is the location of one node of a result, from the root of the value a
// query read. It is a link to its parent's path, so a nodelist one walk finds
// holds a link a node however deep they lie; walks from several starts each
// build their own (GitHub #1960). The zero Path is the root's.
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
	Name               string
	Index              int
	IsObjectMemberStep bool
}

// child returns the path of the member name of the node at p.
func (p Path) child(name string) Path {
	return Path{last: &link{parent: p.last, name: name, member: true}}
}

// element returns the path of the element at index i of the array at p.
func (p Path) element(i int) Path {
	return Path{last: &link{parent: p.last, index: i}}
}

// Parent returns the path of the node holding the one at p and the step from
// it to p, and false for the root's path, which nothing holds. Path values
// compare by the link they hold, and two paths naming one position are one
// value only where they were built as one, so a caller telling positions
// apart reads them step by step.
func (p Path) Parent() (Path, Step, bool) {
	if p.last == nil {
		return Path{}, Step{}, false
	}
	link := p.last
	return Path{last: link.parent}, Step{Name: link.name, Index: link.index, IsObjectMemberStep: link.member}, true
}

// String returns p as RFC 9535 section 2.7 normalizes it: `$`, then each
// member name in single quotes and each array index in brackets, such as
// $['paths']['/a'][0].
func (p Path) String() string {
	var steps []Step
	for at, step, ok := p.Parent(); ok; at, step, ok = at.Parent() {
		steps = append(steps, step)
	}
	var b strings.Builder
	b.WriteByte('$')
	for _, step := range slices.Backward(steps) {
		if !step.IsObjectMemberStep {
			b.WriteString("[" + strconv.Itoa(step.Index) + "]")
			continue
		}
		b.WriteString("['")
		writeNormalName(&b, step.Name)
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
