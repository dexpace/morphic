package census

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Report writes the census as text in the shape the package documents, in a
// fixed order. Members come first, sorted by name; values follow, by type and
// then value, a string quoted and an integer bare. A value its type declares
// no constant for ends in "undeclared". The first line counts the documents
// whose walk stopped at the depth bound.
func (c *Census) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "documents %d truncated %d\n", c.documents, c.truncated)
	fmt.Fprintf(&b, "no-document %d\n", c.noDocument)
	for _, name := range c.universe.members {
		t := c.members[name]
		fmt.Fprintf(&b, "member %d %d %s\n", t.docs, t.instances, name)
	}
	for _, e := range c.universe.all {
		c.writeValues(&b, e)
	}
	return b.String()
}

// writeValues writes the value lines of the enumeration e: every value it
// declares and every other value a document held.
func (c *Census) writeValues(b *strings.Builder, e *enumeration) {
	spellings := maps.Clone(e.declared)
	for k := range c.values {
		if k.enum == e.name {
			spellings[k.value] = e.declared[k.value]
		}
	}
	for _, s := range slices.SortedFunc(maps.Keys(spellings), e.compareSpellings) {
		t := c.values[valueKey{enum: e.name, value: s}]
		note := ""
		if !e.declared[s] {
			note = " undeclared"
		}
		fmt.Fprintf(b, "value %d %d %s %s%s\n", t.docs, t.instances, e.name, e.render(s), note)
	}
}
