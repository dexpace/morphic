package census

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Report writes the census as text in the shape the package documents, in a
// fixed order: the three counts of runs, then the members by name, each
// emptiable one followed by its emptiness, then the values by type and by
// value, a value its type declares no constant for marked undeclared.
func (c *Census) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "documents %d\nno-document %d\ntruncated %d\n", c.documents, c.noDocument, c.truncated)
	for _, f := range c.universe.facts {
		t := c.members[f.name]
		fmt.Fprintf(&b, "member %d %d %s\n", t.docs, t.instances, f.name)
		if f.emptiable {
			t := c.empties[f.name]
			fmt.Fprintf(&b, "empty %d %d %s\n", t.docs, t.instances, f.name)
		}
	}
	for _, e := range c.universe.all {
		c.writeValues(&b, e)
	}
	return b.String()
}

// writeValues writes the value lines of the enumeration e: every value it
// declares and every other value a document held.
func (c *Census) writeValues(b *strings.Builder, e *enumeration) {
	values := maps.Clone(e.declared)
	for k, v := range c.undeclared {
		if k.enum == e.name {
			values[k.spelling] = v
		}
	}
	byValue := func(x, y string) int { return compareValues(values[x], values[y]) }
	for _, s := range slices.SortedFunc(maps.Keys(values), byValue) {
		t := c.values[valueKey{enum: e.name, spelling: s}]
		note := ""
		if _, isDeclared := e.declared[s]; !isDeclared {
			note = " undeclared"
		}
		fmt.Fprintf(b, "value %d %d %s %s%s\n", t.docs, t.instances, e.name, s, note)
	}
}
