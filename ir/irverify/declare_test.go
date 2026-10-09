package irverify_test

import (
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// verifyDeclared runs Verify on a copy of doc that declares the namespaces its
// IDs live in, unless doc declares some itself.
//
// A document is stamped with an irVersion by the fixture that builds it; this
// does the same for Document.IDSpaces, so a test about some other rule is not
// also a test of the declaration. Tests of the declaration build their own and
// call irverify.Verify directly.
func verifyDeclared(doc *ir.Document) []irverify.Violation {
	if doc == nil || doc.IDSpaces != nil {
		return irverify.Verify(doc)
	}
	declared := *doc
	declared.IDSpaces = spacesUsedBy(doc)
	return irverify.Verify(&declared)
}

// spacesUsedBy returns the declaration naming exactly the namespaces doc's IDs
// use: sorted, without repeats, and without the primitive namespace, which ir
// owns. An ID of no known kind, or too malformed to have a namespace, adds none.
func spacesUsedBy(doc *ir.Document) map[string][]string {
	used := map[string]map[string]bool{}
	decls, _ := ir.DeclaredIDs(doc)
	for _, d := range decls {
		kind, _, found := strings.Cut(d.ID, ir.IDSeparator)
		if !found {
			continue
		}
		space, ok := ir.IDSpace(kind, d.ID)
		if !ok || (kind == ir.IDKindType && space == ir.IDSpacePrim) {
			continue
		}
		if used[kind] == nil {
			used[kind] = map[string]bool{}
		}
		used[kind][space] = true
	}
	out := map[string][]string{}
	for kind, spaces := range used {
		if !containsKind(kind) {
			continue
		}
		list := make([]string, 0, len(spaces))
		for space := range spaces {
			list = append(list, space)
		}
		slices.Sort(list)
		out[kind] = list
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func containsKind(kind string) bool { return slices.Contains(ir.IDKinds(), kind) }
