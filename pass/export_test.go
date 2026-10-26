package pass

import "github.com/dexpace/morphic/ir"

// DanglingRefs is checkDanglingRefs over what one walk of doc reads, as validate
// runs it, for the external tests.
func DanglingRefs(doc *ir.Document) []ir.Diagnostic {
	w := walkDocument(doc, ir.WalkValues)
	return checkDanglingRefs(w.refs, w.decls, w.truncated)
}

// KeptRefs returns where the references one walk of doc keeps for
// checkDanglingRefs to settle sit, in walk order, for the external tests.
func KeptRefs(doc *ir.Document) []string {
	sites := walkDocument(doc, ir.WalkValues).refs.sites
	where := make([]string, len(sites))
	for i, s := range sites {
		where[i] = s.where
	}
	return where
}

// AppendTypeIDs is appendTypeIDs, for the external tests.
var AppendTypeIDs = appendTypeIDs

// ValidateWith is validate, walking the document with walk, for the external
// tests that count the walks.
var ValidateWith = validate

// Families is how many rule families Validate runs, each after an ask of its
// context.
func Families() int { return len(families(nil, walked{}, nil)) }
