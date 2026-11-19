// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

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
	for _, memberFact := range c.universe.facts {
		tally := c.members[memberFact.name]
		fmt.Fprintf(&b, "member %d %d %s\n", tally.docs, tally.instances, memberFact.name)
		if memberFact.emptiable {
			tallyValue := c.empties[memberFact.name]
			fmt.Fprintf(&b, "empty %d %d %s\n", tallyValue.docs, tallyValue.instances, memberFact.name)
		}
	}
	for _, enumeration := range c.universe.all {
		c.writeValues(&b, enumeration)
	}
	return b.String()
}

// writeValues writes the value lines of the enumeration enumerationValue: every value it
// declares and every other value a document held.
func (c *Census) writeValues(b *strings.Builder, enumerationValue *enumeration) {
	values := maps.Clone(enumerationValue.declared)
	for valueKey, v := range c.undeclared {
		if valueKey.enum == enumerationValue.name {
			values[valueKey.spelling] = v
		}
	}
	byValue := func(x, y string) int { return compareValues(values[x], values[y]) }
	for _, s := range slices.SortedFunc(maps.Keys(values), byValue) {
		tally := c.values[valueKey{enum: enumerationValue.name, spelling: s}]
		note := ""
		if _, isDeclared := enumerationValue.declared[s]; !isDeclared {
			note = " undeclared"
		}
		fmt.Fprintf(b, "value %d %d %s %s%s\n", tally.docs, tally.instances, enumerationValue.name, s, note)
	}
}
