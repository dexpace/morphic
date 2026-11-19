// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"fmt"
	"slices"
)

// secondSchema is the component the secondRef second construct names.
const secondSchema = "S"

// second is a keyword a case writes beside its construct, in the same unit.
// One named as a construct row writes that row's first value, said of the
// row's targets and written at its versions and spellings, so the case
// writing it beside no construct is the row's own case and writes nothing.
// One named as no row writes what own writes.
type second struct {
	name string
	// own returns what a second of no row writes on t at v, and false where it
	// says nothing of t.
	own func(targetValue target, versionValue version) (written, bool)
	// restates is true of a second saying what t's body says already, so
	// inline, writing that body, writes nothing more.
	restates bool
	// excludes are the constructs the second is not written beside, and why.
	excludes map[string]string
}

// seconds returns the second constructs, in the order the second axis lists
// them: none, then keywords a schema commonly writes beside another.
func seconds() []second {
	return []second{
		{name: none},
		{name: "targetType", own: targetType, restates: true},
		{name: "description"},
		{name: "default"},
		{name: "readOnly", excludes: map[string]string{
			"writeOnly": "a schema read-only and write-only at once, which 3.0 forbids",
		}},
		{name: "enum", excludes: map[string]string{
			"type": "a type its value already says, as the union's type, or contradicts, as its other",
		}},
		{name: "properties"},
		{name: "additionalProperties"},
		{name: "oneOf"},
		{name: "secondRef", own: secondRef, excludes: map[string]string{
			"$id": "the base the second $ref resolves against, so it names nothing (docs/compilers/openapi.md#references)",
		}},
		{name: "nullable"},
		{name: "typeNull", own: typeNull},
	}
}

// typeOf returns the type targetValue's body writes, and false for a target writing
// none.
func typeOf(targetValue target) (any, bool) {
	i := slices.IndexFunc(targetValue.body, func(memberValue member) bool { return memberValue.name == "type" })
	if i < 0 {
		return nil, false
	}
	return targetValue.body[i].value, true
}

// targetType restates targetValue's type beside the construct.
func targetType(targetValue target, _ version) (written, bool) {
	typ, ok := typeOf(targetValue)
	return written{members: object{{"type", typ}}}, ok
}

// typeNull is from 3.1 the spelling of a nullable targetValue, its type and "null";
// before it a type is one name, and the nullable second says it.
func typeNull(targetValue target, versionValue version) (written, bool) {
	typ, ok := typeOf(targetValue)
	if !ok || !versionValue.dialect {
		return written{}, false
	}
	return written{members: object{{"type", []any{typ, "null"}}}}, true
}

// secondRef is an allOf entry naming, by a second $ref, a schema saying what
// targetValue says, so the use holds every instance targetValue does. It follows the target's
// reference in every spelling (written.entries).
func secondRef(targetValue target, _ version) (written, bool) {
	ref := object{{"$ref", "#/components/schemas/" + secondSchema}}
	return written{entries: []any{ref}, components: object{{secondSchema, targetValue.body}}}, true
}

// written is what a second writes beside a construct, or a case in one unit:
// its members, the components they name, the index of the construct row whose
// first value it is, -1 for a second of no row, and where that row is written.
// entries are allOf entries naming schemas by $ref, which every spelling
// writes right after the target's reference: moved with the members, an entry
// written first would also move them before that reference, reordering the
// composition's mixins, which the order written decides
// (docs/compilers/openapi.md#allof-classification), so the two spellings would
// differ by more than the spelling.
type written struct {
	members, components object
	entries             []any
	row                 int
	stays               stays
}

// names returns the members' names, and allOf where w writes entries.
func (w written) names() []string {
	out := make([]string, 0, len(w.members)+1)
	for _, member := range w.members {
		out = append(out, member.name)
	}
	if len(w.entries) > 0 {
		out = append(out, "allOf")
	}
	return out
}

// secondAt returns what the second at index j of the second axis writes on targetValue
// at versionValue, and false where it writes nothing there: at a version or a target like
// another, whose findings beside a second repeat that one's, as they do with
// none; where its row is not said of targetValue or versionValue lacks it; where own says nothing
// of targetValue.
func (c cases) secondAt(j int, versionValue version, targetValue target) (written, bool) {
	sec := c.seconds[j]
	if sec.name != none && (versionValue.like != "" || narrowed(targetValue.name) != "") {
		return written{}, false
	}
	out := written{row: c.rowOf(j)}
	switch {
	case out.row >= 0:
		construct := c.constructs[out.row]
		if !construct.span.holds(versionValue) || !construct.saidOf(targetValue) {
			return written{}, false
		}
		out.members, out.components, out.stays = construct.values(targetValue, versionValue)[0], construct.components, construct.stays
	case sec.own != nil:
		var ok bool
		if out, ok = sec.own(targetValue, versionValue); !ok {
			return written{}, false
		}
		out.row = -1
	default:
	}
	return out, true
}

// fits reports whether the case at can write sec, its second, beside first,
// its construct's members, on targetValue: not beside a construct the second excludes,
// nor writing a keyword first writes; and at inline neither restating targetValue's
// body nor, as a row's value, replacing a member of it, so that the merged
// document would no longer say which keyword is the second.
func (c cases) fits(at []int, sec written, first object, targetValue target) bool {
	if at[2] > 0 {
		if _, excluded := c.seconds[at[3]].excludes[c.constructs[(at[2]-1)/2].keyword]; excluded {
			return false
		}
	}
	if slices.ContainsFunc(sec.names(), func(name string) bool { return slices.ContainsFunc(first, named(name)) }) {
		return false
	}
	if c.spellings[at[5]] != inline {
		return true
	}
	replaces := slices.ContainsFunc(sec.members, func(memberValue member) bool { return slices.ContainsFunc(targetValue.body, sameName(memberValue)) })
	return !c.seconds[at[3]].restates && (sec.row < 0 || !replaces)
}

// rowOf returns the index in c.constructs of the row the second at index j
// is named as, -1 for none or a second writing its own members. A second
// naming no row and writing nothing of its own panics, which CheckCase reports
// as the case's outcome.
func (c cases) rowOf(j int) int {
	sec := c.seconds[j]
	if sec.name == none || sec.own != nil {
		return -1
	}
	i := slices.IndexFunc(c.constructs, func(constructValue construct) bool { return constructValue.keyword == sec.name })
	if i < 0 {
		panic(fmt.Sprintf("openapicases: second construct %q names no construct row and writes nothing", sec.name))
	}
	return i
}

// sameName returns the predicate of a member named as memberValue is.
func sameName(memberValue member) func(member) bool { return named(memberValue.name) }

// named returns the predicate of a member named name.
func named(name string) func(member) bool {
	return func(memberValue member) bool { return memberValue.name == name }
}
