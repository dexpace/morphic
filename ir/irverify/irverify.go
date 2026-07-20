package irverify

import (
	"sort"

	"github.com/dexpace/morphic/ir"
)

// Violation is one structural defect found in an ir.Document: an internal
// compiler bug, never a spec-author problem (that is ir.Diagnostic). Code is a
// stable, slash-namespaced string; Path locates the offending node.
type Violation struct {
	// Code is a stable, slash-namespaced identifier for the defect class.
	Code string
	// Message is a human-readable description of the specific defect.
	Message string
	// Path locates the offending node within the document.
	Path string
}

// Verify runs every structural invariant check over doc and returns the
// violations, sorted by (Code, Path) for deterministic output. A structurally
// sound document yields nil.
func Verify(doc *ir.Document) []Violation {
	if doc == nil {
		return []Violation{{Code: "ir/nil-document", Message: "document is nil"}}
	}

	vs := checkRegistryKeys(doc)
	vs = append(vs, checkReferentialIntegrity(doc)...)
	vs = append(vs, checkNaming(doc)...)

	sort.Slice(vs, func(i, j int) bool {
		if vs[i].Code != vs[j].Code {
			return vs[i].Code < vs[j].Code
		}
		return vs[i].Path < vs[j].Path
	})
	return vs
}

// checkRegistryKeys asserts every Types entry is keyed by its own Common().ID
// and that the ID is non-empty.
func checkRegistryKeys(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		if id == "" {
			vs = append(vs, Violation{
				Code:    "ir/empty-type-id",
				Message: "type registry has an empty key",
				Path:    `types[""]`,
			})
			continue
		}
		if got := td.Common().ID; got != id {
			vs = append(vs, Violation{
				Code:    "ir/type-id-mismatch",
				Message: "registry key " + string(id) + " disagrees with node ID " + string(got),
				Path:    "types[" + string(id) + "]",
			})
		}
	}
	return vs
}
