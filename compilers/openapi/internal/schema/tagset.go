// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"slices"

	"github.com/dexpace/morphic/ir"
)

// maxTagAliasHops bounds how many alias scalars a tag's type is read through
// (styleguide bounded-everything rule), as maxBodyAliasHops bounds a body's. A
// longer chain, or a loop, stops on an alias, which bounds no tag.
const maxTagAliasHops = 64

// tagSet is what an instance may hold at a tag property, as the
// discriminator-inference policy reads it
// (docs/compilers/openapi.md#discriminator-inference).
type tagSet struct {
	// strings are the strings admitted, sorted and once each, read only where
	// closed is set.
	strings []string
	// closed reports that strings bounds the strings admitted; false where any
	// string may be, or a value no string reading bounds, such as a number.
	closed bool
	// null reports that null may be held, which is no tag.
	null bool
}

// anyString admits any string, and null only where a reference adds it.
func anyString() tagSet { return tagSet{} }

// anything admits any value, null among them.
func anything() tagSet { return tagSet{null: true} }

// pins reports whether a admits strings, every one a tag, and nothing else: a
// closed set of one or more, without null.
func (a tagSet) pins() bool { return a.closed && len(a.strings) > 0 && !a.null }

// admits reports whether a admits the string tag.
func (a tagSet) admits(tag string) bool {
	if !a.closed {
		return true
	}
	_, found := slices.BinarySearch(a.strings, tag)
	return found
}

// meet returns what both a and b admit, as an instance meeting two
// declarations holds: the strings both bound, and null where both admit it.
func meet(a, b tagSet) tagSet {
	switch {
	case !b.closed:
		return tagSet{strings: a.strings, closed: a.closed, null: a.null && b.null}
	case !a.closed:
		return tagSet{strings: b.strings, closed: true, null: a.null && b.null}
	}
	both := make([]string, 0, min(len(a.strings), len(b.strings)))
	for _, s := range a.strings {
		if _, found := slices.BinarySearch(b.strings, s); found {
			both = append(both, s)
		}
	}
	return tagSet{strings: both, closed: true, null: a.null && b.null}
}

// join returns what a value of one of sets admits: the strings any admits, for
// an exclusive union those exactly one does, as a value two admit matches
// neither alone, and likewise null. One not closed bounds no string.
func join(sets []tagSet, exclusive bool) tagSet {
	once := func(n int) bool { return n == 1 || (!exclusive && n > 0) }
	count := map[string]int{}
	nulls, closed := 0, true
	for _, set := range sets {
		closed = closed && set.closed
		for _, s := range set.strings {
			count[s]++
		}
		if set.null {
			nulls++
		}
	}
	got := tagSet{closed: closed, null: once(nulls)}
	if !closed {
		return got
	}
	got.strings = []string{}
	for s, n := range count {
		if once(n) {
			got.strings = append(got.strings, s)
		}
	}
	slices.Sort(got.strings)
	return got
}

// leaf returns what a value of td, no union, admits: a string Literal's
// string, or the members of a closed Enum marking no flags every one of which
// is a string, sorted and once each. Any other node admits any string.
func leaf(td ir.TypeDef) tagSet {
	switch n := td.(type) {
	case *ir.Literal:
		if n == nil || n.Value.Kind != ir.ValueString {
			return anyString()
		}
		return tagSet{strings: []string{n.Value.Str}, closed: true}
	case *ir.Enum:
		if n == nil || !n.Closed || n.Flags {
			return anyString()
		}
		tags := make([]string, 0, len(n.Members))
		for _, m := range n.Members {
			if m.Value.Kind != ir.ValueString {
				return anyString()
			}
			tags = append(tags, m.Value.Str)
		}
		slices.Sort(tags)
		return tagSet{strings: slices.Compact(tags), closed: true}
	default:
		return anyString()
	}
}

// throughAliases returns the type the alias scalars from id stand over, at
// most maxTagAliasHops of them read, and whether one of them admits null.
func throughAliases(reg ir.TypeRegistry, id ir.TypeID) (ir.TypeID, bool) {
	null := false
	for range maxTagAliasHops {
		s, alias := reg[id].(*ir.Scalar)
		if !alias || s == nil || s.Base == nil {
			break
		}
		id, null = s.Base.Target, null || s.Base.Nullable
	}
	return id, null
}

// unionNode returns the union the type id is, nil where it is none.
func unionNode(reg ir.TypeRegistry, id ir.TypeID) *ir.Union {
	u, _ := reg[id].(*ir.Union)
	return u
}

// flatMember returns the member of flat whose wire name is property.
func flatMember(flat ir.Flattening, property string) (ir.FlatProperty, bool) {
	i := slices.IndexFunc(flat.Properties, func(p ir.FlatProperty) bool {
		return p.Property.EffectiveWireName() == property
	})
	if i < 0 {
		return ir.FlatProperty{}, false
	}
	return flat.Properties[i], true
}
