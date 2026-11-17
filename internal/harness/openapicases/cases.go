// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"fmt"
	"slices"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/internal/harness"
)

// The axes' names, in their order, and the values every axis but version,
// target, construct and position takes.
const (
	versionAxis   = "version"
	targetAxis    = "target"
	constructAxis = "construct"
	secondAxis    = "second"
	positionAxis  = "position"
	spellingAxis  = "spelling"

	// none is the construct, and the second construct, a case leaves out.
	none = "none"
	// otherMark marks a construct's other value: "minLength~" writes the second.
	otherMark = "~"
	// inline writes a target's body, the construct merged in, at the position.
	inline = "inline"
)

// specPath is the path every case's one source is held at.
const specPath = "openapi.json"

// version is an OpenAPI version a case is written at: its name, the core
// version a finding at it is recorded under, and whether its schemas read JSON
// Schema 2020-12.
type version struct {
	name, like string
	dialect    bool
}

// versions returns the versions, in the order the version axis lists them.
func versions() []version {
	return []version{{name: "3.0.3"}, {name: "3.1.0", dialect: true}, {name: "3.2.0", like: "3.1.0", dialect: true}}
}

// spellings returns the spellings, in the order the spelling axis lists them.
func spellings() []string { return []string{inline, beside, wrap, entry, entryFirst, nested} }

// cases is the OpenAPI product: the tables each axis lists the values of.
type cases struct {
	versions   []version
	targets    []target
	constructs []construct
	seconds    []second
	positions  []position
	spellings  []string
}

// New returns the OpenAPI compiler's schema cases.
func New() harness.Cases {
	return cases{
		versions: versions(), targets: targets(), constructs: constructs(), seconds: seconds(),
		positions: positions(), spellings: spellings(),
	}
}

// Axes names the axes: version, target, construct (none, then each keyword
// and its other value), second construct, position and spelling.
func (c cases) Axes() []harness.Axis {
	axes := []harness.Axis{{Name: versionAxis}, {Name: targetAxis}, {Name: constructAxis}, {Name: secondAxis},
		{Name: positionAxis}, {Name: spellingAxis}}
	for _, v := range c.versions {
		axes[0].Values = append(axes[0].Values, harness.Value{Name: v.name, Like: v.like})
	}
	for _, t := range c.targets {
		axes[1].Values = append(axes[1].Values, harness.Value{Name: t.name, Like: narrowed(t.name)})
	}
	axes[2].Values = append(axes[2].Values, harness.Value{Name: none})
	for _, k := range c.constructs {
		axes[2].Values = append(axes[2].Values, harness.Value{Name: k.keyword}, harness.Value{Name: k.keyword + otherMark})
	}
	for _, sec := range c.seconds {
		axes[3].Values = append(axes[3].Values, harness.Value{Name: sec.name, Like: secondLike(sec.name)})
	}
	for _, p := range c.positions {
		axes[4].Values = append(axes[4].Values, harness.Value{Name: p.name})
	}
	axes[5].Values = spellingValues(c.spellings)
	return axes
}

// secondLike returns the core value a second construct is recorded under:
// none, as the gate runs a construct alone and a nightly run writes a second
// beside it.
func secondLike(name string) string {
	if name == none {
		return ""
	}
	return none
}

// coreValues returns a core value of each name.
func coreValues(of []string) []harness.Value {
	values := make([]harness.Value, len(of))
	for i, n := range of {
		values[i] = harness.Value{Name: n}
	}
	return values
}

// spellingValues returns a value of each spelling, the gate running all but
// an entry written first, which lowers as the entry written last does
// (docs/compilers/openapi.md#allof-classification) and so repeats its
// findings: a nightly run records them under that entry.
func spellingValues(of []string) []harness.Value {
	values := coreValues(of)
	for i, n := range of {
		if n == entryFirst {
			values[i].Like = entry
		}
	}
	return values
}

// Write returns the document at one value index per axis, and false where the
// combination writes nothing: a construct or a second its version lacks or its
// target is not said of (unit), a spelling that moves no such keyword or writes
// another's document (spelt), or indices outside the product. A table the
// writer cannot encode panics, which CheckCase reports as the case's outcome.
func (c cases) Write(at []int) (harness.Document, bool) {
	if !c.holds(at) {
		return harness.Document{}, false
	}
	v, t, p, s := c.versions[at[0]], c.targets[at[1]], c.positions[at[4]], c.spellings[at[5]]
	u, ok := c.unit(at, v, t)
	if !ok {
		return harness.Document{}, false
	}
	use, ok := c.spelt(u, s, t)
	if !ok {
		return harness.Document{}, false
	}

	paths, schemas := p.place(use)
	if s != inline {
		schemas = append(schemas, member{targetSchema, t.body})
	}
	data, err := encode(object{
		{"openapi", v.name},
		{"info", object{{"title", "cases"}, {"version", "1"}}},
		{"paths", paths},
		{"components", object{{"schemas", append(schemas, u.components...)}}},
	})
	if err != nil {
		panic(fmt.Sprintf("openapicases: case %v: %v", at, err))
	}
	doc := harness.Document{
		Sources: []compilers.Source{{Path: specPath, Data: data}},
		Equal:   c.equal(at, u),
	}
	if doc.Equal != nil && at[2] > 0 && at[3] > 0 {
		doc.Equal.Parts = c.parts(at)
	}
	c.neighbours(&doc, v, t, at)
	return doc, true
}

// parts returns the cases writing alone each keyword the case at writes in one
// unit, the rest alike: its construct with no second, and its second with no
// construct, which for a second named as a construct row is that row's case.
func (c cases) parts(at []int) [][]int {
	first := slices.Concat(at[:3], []int{0}, at[4:])
	second := slices.Concat(at[:2], []int{0}, at[3:])
	if row := c.rowOf(at[3]); row >= 0 {
		second[2], second[3] = 1+2*row, 0
	}
	return [][]int{first, second}
}

// unit returns what the case at writes in one unit on t at v: its construct's
// members then its second's, the components they name, the second's entries
// and where the unit is written, the stricter of the two, and false where
// either writes nothing. A row's second beside no construct is that row's
// case, which writes it.
func (c cases) unit(at []int, v version, t target) (written, bool) {
	members, components, ok := c.construct(at[2], v, t)
	if !ok {
		return written{}, false
	}
	sec, ok := c.secondAt(at[3], v, t)
	if !ok || (sec.row >= 0 && at[2] == 0) || !c.fits(at, sec, members, t) {
		return written{}, false
	}
	return written{
		members:    slices.Concat(members, sec.members),
		components: slices.Concat(components, sec.components),
		entries:    sec.entries,
		row:        -1,
		stays:      max(c.staysAt(at[2]), sec.stays),
	}, true
}

// neighbours names, in doc, the cases the presence oracle compares the case at
// with: the case leaving the construct out, and, for a keyword something
// carries, the case writing its other value. The none construct and a
// keyword's other value name none: the latter is asked through its keyword.
// Left out of a unit whose second writes no member, the construct leaves an
// entry spelling wrap's document; left beside a row's second, it leaves that
// row's case. A keyword the target's body writes already is replaced by the
// construct, so no case leaves it out (Document.Replaces).
func (c cases) neighbours(doc *harness.Document, v version, t target, at []int) {
	if at[2] == 0 || (at[2]-1)%2 != 0 {
		return
	}
	k := c.constructs[(at[2]-1)/2]
	doc.Absent = slices.Concat(at[:2], []int{0}, at[3:])
	if row := c.rowOf(at[3]); row >= 0 {
		doc.Absent[2], doc.Absent[3] = 1+2*row, 0 // the row's second alone is the row's case
	} else if s := c.spellings[at[5]]; s == entry || s == entryFirst {
		if sec, _ := c.secondAt(at[3], v, t); len(sec.members) == 0 {
			doc.Absent[5] = slices.Index(c.spellings, wrap) // an entry saying nothing writes wrap's document
		}
	}
	doc.Carried, doc.Reason = k.reason == "", k.reason
	if doc.Carried {
		doc.Other = slices.Concat(at[:2], []int{at[2] + 1}, at[3:])
	}
	doc.Replaces = slices.ContainsFunc(t.body, func(m member) bool { return m.name == k.keyword })
}

// holds reports whether at names a case of c: one index per axis, each within
// its axis.
func (c cases) holds(at []int) bool {
	lengths := []int{len(c.versions), len(c.targets), 1 + 2*len(c.constructs), len(c.seconds), len(c.positions),
		len(c.spellings)}
	if len(at) != len(lengths) {
		return false
	}
	for k, n := range lengths {
		if at[k] < 0 || at[k] >= n {
			return false
		}
	}
	return true
}

// construct returns the members the construct at index i of the construct axis
// merges into t's body at v, and the components it names, or false where v
// lacks it or it is not said of t. Index 0 is none, which writes nothing.
func (c cases) construct(i int, v version, t target) (object, object, bool) {
	if i == 0 {
		return nil, nil, true
	}
	k := c.constructs[(i-1)/2]
	if !k.span.holds(v) || !k.saidOf(t) {
		return nil, nil, false
	}
	return k.values(t, v)[(i-1)%2], k.components, true
}
