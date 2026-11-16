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
	// other marks a construct's other value: "minLength~" writes the second.
	other = "~"
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

// seconds returns the second constructs, in the order the second axis lists
// them.
func seconds() []string { return []string{none} }

// spellings returns the spellings, in the order the spelling axis lists them.
func spellings() []string { return []string{inline} }

// cases is the OpenAPI product: the tables each axis lists the values of.
type cases struct {
	versions   []version
	targets    []target
	constructs []construct
	seconds    []string
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
		axes[1].Values = append(axes[1].Values, harness.Value{Name: t.name})
	}
	axes[2].Values = append(axes[2].Values, harness.Value{Name: none})
	for _, k := range c.constructs {
		axes[2].Values = append(axes[2].Values, harness.Value{Name: k.keyword}, harness.Value{Name: k.keyword + other})
	}
	axes[3].Values = names(c.seconds)
	for _, p := range c.positions {
		axes[4].Values = append(axes[4].Values, harness.Value{Name: p.name})
	}
	axes[5].Values = names(c.spellings)
	return axes
}

// names returns a core value of each name.
func names(of []string) []harness.Value {
	values := make([]harness.Value, len(of))
	for i, n := range of {
		values[i] = harness.Value{Name: n}
	}
	return values
}

// Write returns the document at one value index per axis, and false where the
// combination writes nothing: a construct its version lacks or its target is
// not said of, or indices outside the product. A table the writer cannot
// encode panics, which CheckCase reports as the case's outcome.
func (c cases) Write(at []int) (harness.Document, bool) {
	if !c.holds(at) {
		return harness.Document{}, false
	}
	v, t, p := c.versions[at[0]], c.targets[at[1]], c.positions[at[4]]
	members, components, ok := c.construct(at[2], v, t)
	if !ok || c.seconds[at[3]] != none || c.spellings[at[5]] != inline {
		return harness.Document{}, false
	}

	paths, schemas := p.place(t.body.with(members))
	data, err := encode(object{
		{"openapi", v.name},
		{"info", object{{"title", "cases"}, {"version", "1"}}},
		{"paths", paths},
		{"components", object{{"schemas", append(schemas, components...)}}},
	})
	if err != nil {
		panic(fmt.Sprintf("openapicases: case %v: %v", at, err))
	}
	doc := harness.Document{Sources: []compilers.Source{{Path: specPath, Data: data}}}
	c.neighbours(&doc, at)
	return doc, true
}

// neighbours names, in doc, the cases the presence oracle compares the case at
// with: the case leaving the construct out, and, for a keyword something
// carries, the case writing its other value. The none construct and a
// keyword's other value name none: the latter is asked through its keyword.
func (c cases) neighbours(doc *harness.Document, at []int) {
	if at[2] == 0 || (at[2]-1)%2 != 0 {
		return
	}
	k := c.constructs[(at[2]-1)/2]
	doc.Absent = slices.Concat(at[:2], []int{0}, at[3:])
	doc.Carried, doc.Reason = k.reason == "", k.reason
	if doc.Carried {
		doc.Other = slices.Concat(at[:2], []int{at[2] + 1}, at[3:])
	}
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
