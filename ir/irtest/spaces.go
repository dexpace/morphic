// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irtest

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// SpacesUsed returns the namespaces the IDs of doc live in, by kind prefix, in
// the form of [ir.Document.IDSpaces]: sorted, without repeats, a kind with none
// left out. It returns nil when no ID needs one, a nil doc included.
//
// An ID of no known kind or too malformed to have a namespace adds none, and
// neither does a type in the primitive namespace, which ir owns. A walk cut short
// yields what it found; irverify reports the truncation itself.
func SpacesUsed(doc *ir.Document) map[string][]string {
	kinds := ir.IDKinds()
	var out map[string][]string
	var decls ir.IDDeclarations
	ir.WalkValues(doc, ir.DocumentPath, &decls)
	for _, d := range decls.List() {
		kind, _, found := strings.Cut(d.ID, ir.IDSeparator)
		if !found || !slices.Contains(kinds, kind) {
			continue
		}
		space, wellFormed := ir.IDSpace(kind, d.ID)
		if !wellFormed || (kind == ir.IDKindType && space == ir.IDSpacePrim) {
			continue
		}
		if out == nil {
			out = map[string][]string{}
		}
		out[kind] = append(out[kind], space)
	}
	for kind, spaces := range out {
		slices.Sort(spaces)
		out[kind] = slices.Compact(spaces)
	}
	return out
}
