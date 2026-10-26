package ir

import (
	"slices"
	"strings"
)

// inferredSeparator joins the names in an inferred-marker list. A heuristic's
// name holds none.
const inferredSeparator = ","

// JoinInferred builds the list [Provenance.Inferred] and [TypeRef.Inferred]
// hold: each heuristic's name once, comma-separated, in the order first written.
//
// Each argument is itself such a list, so one heuristic's name is a list of one
// and two stored lists merge in one call. Names compare exactly and none is
// empty. A node reached by two references is annotated twice, and a name that
// repeated per reference would make the field depend on the document's reference
// count rather than on which heuristics ran.
func JoinInferred(lists ...string) string {
	names := make([]string, 0, len(lists))
	for _, list := range lists {
		for name := range strings.SplitSeq(list, inferredSeparator) {
			if name != "" && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	return strings.Join(names, inferredSeparator)
}
