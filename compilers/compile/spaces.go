package compile

import "slices"

// Namespaces is what a compiler declares about the IDs it mints: for each kind
// prefix, the namespaces those IDs live in.
//
// It is built from the same Space constants the compiler derives IDs with, so
// the declaration and the IDs cannot be spelled differently. The compiler's own
// test holds the two to each other in the other direction, namespaces declared
// and never used.
type Namespaces map[string][]Space

// Declaration returns what ir.Document.IDSpaces carries: each kind's namespaces
// sorted and without repeats, copied so the result shares nothing with n. A kind
// with no namespace is left out, and n declaring nothing returns nil.
func (n Namespaces) Declaration() map[string][]string {
	var out map[string][]string
	for kind, spaces := range n {
		list := make([]string, 0, len(spaces))
		for _, space := range spaces {
			list = append(list, string(space))
		}
		slices.Sort(list)
		list = slices.Compact(list)
		if len(list) == 0 {
			continue
		}
		if out == nil {
			out = make(map[string][]string, len(n))
		}
		out[kind] = list
	}
	return out
}
