// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"maps"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/typeformat"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// scalarForm is one `format` a reading holds: as written, and the primitive the
// type and format beside each other select where the table has a row for them.
type scalarForm struct {
	spelled string
	prim    ir.PrimitiveKind
	listed  bool
}

// narrows reports whether f is the form scalarFormValue is, or a narrower one.
func (f scalarForm) narrows(scalarFormValue scalarForm) bool {
	if f.spelled == scalarFormValue.spelled {
		return true
	}
	return f.listed && scalarFormValue.listed && typeformat.Relate(f.prim, scalarFormValue.prim) == typeformat.ANarrower
}

// admitted is what a schema's keywords leave an instance able to be: its kinds,
// the values a keyword fixes where one does, and the formats and element schema
// it is held to. It compares only what these name, so two schemas differing in
// anything else, a bound or an annotation, compare equal.
type admitted struct {
	kinds kindSet
	// fixed marks a value set, values then holding each member's key (memberOf)
	// with its kind, none where the set is empty, and joined the sets a union
	// adds to it (joinSets), each still the branch's own where it can be: a
	// union shares its branches' sets rather than copying them, no member is
	// held twice, and none is written to.
	fixed  bool
	values map[string]kindSet
	joined []map[string]kindSet
	forms  []scalarForm
	items  *statement.Statement
}

// hasMember reports whether a's value set holds the member keyed key.
func (a admitted) hasMember(key string) bool {
	if _, ok := a.values[key]; ok {
		return true
	}
	for _, set := range a.joined {
		if _, ok := set[key]; ok {
			return true
		}
	}
	return false
}

// members yields each member of a's value set with its kind, once.
func (a admitted) members(yield func(string, kindSet) bool) {
	for v, kind := range a.values {
		if !yield(v, kind) {
			return
		}
	}
	for _, set := range a.joined {
		for v, kind := range set {
			if !yield(v, kind) {
				return
			}
		}
	}
}

// size is how many members a's value set holds.
func (a admitted) size() int {
	n := len(a.values)
	for _, set := range a.joined {
		n += len(set)
	}
	return n
}

// and is what a and b admit together. The values of a set a kind beside it
// excludes stay in the set, and reachable shows them gone.
func (a admitted) and(b admitted) admitted {
	out := a
	out.kinds &= b.kinds
	out.forms = slices.Concat(a.forms, b.forms)
	if out.items == nil {
		out.items = b.items
	}
	switch {
	case a.fixed && b.fixed:
		out.values, out.joined = sharedValues(a, b), nil
	case b.fixed:
		out.fixed, out.values, out.joined = true, b.values, b.joined
	}
	return out
}

// sharedValues returns the members a's and b's value sets hold, walking the
// smaller: a value set is shared by every reader of it, so neither may be
// written to.
func sharedValues(a, b admitted) map[string]kindSet {
	if b.size() < a.size() {
		a, b = b, a
	}
	out := make(map[string]kindSet, a.size())
	for v, kind := range a.members {
		if b.hasMember(v) {
			out[v] = kind
		}
	}
	return out
}

// or is what either of a and b admits: a union, whose branches hold no one
// format or element schema an instance is held to, and whose value set joins
// theirs.
func (a admitted) or(b admitted) admitted {
	out := admitted{kinds: a.kinds | b.kinds, fixed: a.fixed && b.fixed}
	if out.fixed {
		out.values, out.joined = joinSets(a, b)
	}
	return out
}

// maxJoinedSets bounds the sets a value set joins beside its first, so asking
// whether it holds a member reads at most that many more maps.
const maxJoinedSets = 8

// joinSets returns the sets the union of a's and b's value sets holds: the
// larger's as they are, and the smaller's where the larger holds none of their
// members, else one set of the members it lacks, so no member is held twice.
// Past maxJoinedSets sets beside the first, they are copied into one.
func joinSets(a, b admitted) (map[string]kindSet, []map[string]kindSet) {
	if b.size() > a.size() {
		a, b = b, a
	}
	added := slices.Concat([]map[string]kindSet{b.values}, b.joined)
	for v := range b.members {
		if a.hasMember(v) {
			rest := make(map[string]kindSet, b.size())
			for member, kind := range b.members {
				if !a.hasMember(member) {
					rest[member] = kind
				}
			}
			added = []map[string]kindSet{rest}
			break
		}
	}
	joined := slices.Concat(a.joined, slices.DeleteFunc(added, func(set map[string]kindSet) bool { return len(set) == 0 }))
	if len(joined) <= maxJoinedSets {
		return a.values, joined
	}
	one := make(map[string]kindSet, a.size()+b.size())
	for _, set := range slices.Concat([]map[string]kindSet{a.values}, joined) {
		maps.Copy(one, set)
	}
	return one, nil
}

// reachable reports whether any value is left: a kind, and, under a value set, a
// member of a kind left.
func (a admitted) reachable() bool {
	if !a.fixed {
		return a.kinds != 0
	}
	for _, kind := range a.members {
		if kind&a.kinds != 0 {
			return true
		}
	}
	return false
}

// admitsEverything reports whether a holds an instance to nothing a reading
// compares: every kind, null among them, no value set, no format and no
// element schema.
func (a admitted) admitsEverything() bool {
	return a.kinds == kindAny && !a.fixed && len(a.forms) == 0 && a.items == nil
}

// kindsWithin reports whether a's kinds are b's, which says nothing of null:
// a reference's Nullable holds that, read on its own (nulls).
func (a admitted) kindsWithin(b admitted) bool { return a.kinds&^kindNull&^b.kinds == 0 }

// valuesWithin reports whether b fixes no value a does not fix.
func (a admitted) valuesWithin(b admitted) bool {
	within, _ := a.walkWithin(b)
	return within
}

// walkWithin is valuesWithin with how many of a's members it looked up. Those
// are distinct, so a set larger than b's holds one b lacks, answered unwalked.
func (a admitted) walkWithin(b admitted) (within bool, walked int) {
	if !b.fixed {
		return true, 0
	}
	if !a.fixed || a.size() > b.size() {
		return false, 0
	}
	for v := range a.members {
		walked++
		if !b.hasMember(v) {
			return false, walked
		}
	}
	return true, walked
}

// narrowedTo reports whether joint, what a and the conjuncts beside it admit
// together, leaves out a kind or a value a admits.
func (a admitted) narrowedTo(joint admitted) bool {
	return a.kinds != joint.kinds || !a.valuesWithin(joint)
}

// formsWithin reports whether a is held to some form narrowing each of b's.
func (a admitted) formsWithin(b admitted) bool {
	for _, want := range b.forms {
		if !slices.ContainsFunc(a.forms, func(scalarFormValue scalarForm) bool { return scalarFormValue.narrows(want) }) {
			return false
		}
	}
	return true
}

// takes reports whether a admits the value v writes, for a default read across a
// narrowing (Use.Default). One it cannot read is no value it takes.
func (a admitted) takes(v *yaml.Node) bool {
	key, kind, ok := memberOf(v)
	if !ok {
		return false
	}
	if a.fixed {
		return a.hasMember(key)
	}
	return kind&a.kinds != 0
}

// memberOf returns the key v compares by, as an enum compares its own entries
// (valueKey), so 1 and 1.0 are one value, with its kind, and false for a value
// it cannot read.
func memberOf(v *yaml.Node) (string, kindSet, bool) {
	val, err := value.FromNode(v)
	if err != nil {
		return "", 0, false
	}
	key, keyed := valueKey(val, 0)
	return key, kindOfValue(val), keyed
}
