package irverify

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// idSpacesPath is where a document declares the namespaces its IDs live in.
const idSpacesPath = ir.DocumentPath + ".IDSpaces"

// checkIDSpaces asserts that every ID with a kind prefix lives in a namespace the
// document declares for that kind, and that the declaration is usable.
//
// It is the one check that sees a path glued onto its namespace for every class,
// needing neither a recorded pointer nor any knowledge of the format: an ID that
// lost its separator is well-formed in a namespace nobody declared (GitHub
// #141). A type in the primitive namespace, which is ir's own, needs no
// declaration.
//
// A document declaring nothing while carrying IDs is reported once, as a
// producer that forgot, like a missing irVersion.
func checkIDSpaces(doc *ir.Document, decls declarations) ([]Violation, bool) {
	vs := declarationViolations(doc.IDSpaces)
	declared := declaredSpaces(doc.IDSpaces)
	var carried int
	for _, d := range decls.ids {
		prefix, held := kindPrefixes[d.Class]
		if !held {
			continue
		}
		space, wellFormed := ir.IDSpace(prefix, d.ID)
		if !wellFormed || (prefix == ir.IDKindType && space == ir.IDSpacePrim) {
			continue // malformed is reported where the shape is checked; prim is ir's
		}
		carried++
		if len(doc.IDSpaces) > 0 && !declared[prefix][space] {
			vs = append(vs, Violation{
				Code: "ir/id-space-undeclared",
				Message: "id " + strconv.Quote(d.ID) + " lives in namespace " + strconv.Quote(space) +
					", which the document does not declare for kind " + strconv.Quote(prefix),
				Path: d.Path,
			})
		}
	}
	if carried > 0 && len(doc.IDSpaces) == 0 {
		vs = append(vs, Violation{
			Code:    "ir/id-spaces-absent",
			Message: "document declares no id namespaces but carries ids that need them",
			Path:    idSpacesPath,
		})
	}
	return vs, decls.truncated
}

// declaredSpaces returns the declaration as sets, for membership.
func declaredSpaces(declaration map[string][]string) map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(declaration))
	for kind, spaces := range declaration {
		set := make(map[string]bool, len(spaces))
		for _, space := range spaces {
			set[space] = true
		}
		out[kind] = set
	}
	return out
}

// declarationViolations reports what makes a declaration unusable on its own: a
// key that is not a kind prefix, a namespace that is empty or carries the ID
// separator, and a list that is not sorted without repeats. The last is the
// canonical order, the byte order of the UTF-8 spelling, which keeps two
// producers' lists of one vocabulary byte-identical (invariant 7).
func declarationViolations(declaration map[string][]string) []Violation {
	var vs []Violation
	kinds := ir.IDKinds()
	for _, kind := range slices.Sorted(maps.Keys(declaration)) {
		if !slices.Contains(kinds, kind) {
			vs = append(vs, Violation{
				Code:    "ir/id-spaces-unknown-kind",
				Message: "id namespaces are declared for kind " + strconv.Quote(kind) + ", which no id opens with",
				Path:    idSpacesPath + "[" + strconv.Quote(kind) + "]",
			})
			continue
		}
		vs = appendSpaceViolations(vs, kind, declaration[kind])
	}
	return vs
}

// appendSpaceViolations reports the unusable entries of one kind's list.
func appendSpaceViolations(vs []Violation, kind string, spaces []string) []Violation {
	for i, space := range spaces {
		path := idSpacesPath + "[" + kind + "][" + strconv.Itoa(i) + "]"
		if space == "" || strings.Contains(space, ir.IDSeparator) {
			vs = append(vs, Violation{
				Code:    "ir/id-space-invalid",
				Message: "namespace " + strconv.Quote(space) + " is empty or carries the id separator, so no id can be in it",
				Path:    path,
			})
		}
		if i > 0 && space <= spaces[i-1] {
			vs = append(vs, Violation{
				Code:    "ir/id-spaces-not-canonical",
				Message: "namespace " + strconv.Quote(space) + " does not follow " + strconv.Quote(spaces[i-1]) + " in sorted order without repeats",
				Path:    path,
			})
		}
	}
	return vs
}
