package pass

import "github.com/dexpace/morphic/ir"

// DanglingRefs is checkDanglingRefs over what one walk of doc gathers, as
// validate runs it, for the external tests.
func DanglingRefs(doc *ir.Document) []ir.Diagnostic {
	var decls ir.IDDeclarations
	refs := newRefSites(doc)
	truncated := ir.WalkValues(doc, ir.DocumentPath, &decls, refs)
	return checkDanglingRefs(refs, decls, truncated)
}

// KeptRefs returns where the references one walk of doc keeps for
// checkDanglingRefs to settle sit, in walk order, for the external tests.
func KeptRefs(doc *ir.Document) []string {
	refs := newRefSites(doc)
	ir.WalkValues(doc, ir.DocumentPath, refs)
	where := make([]string, len(refs.sites))
	for i, s := range refs.sites {
		where[i] = s.where
	}
	return where
}

// AppendTypeIDs is appendTypeIDs, for the external tests.
var AppendTypeIDs = appendTypeIDs

// ValidateWith is validate, walking the document with walk, for the external
// tests that count the walks.
var ValidateWith = validate
