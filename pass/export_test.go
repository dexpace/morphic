package pass

import "github.com/dexpace/morphic/ir"

// DanglingRefs is checkDanglingRefs over what one walk of doc reads, as validate
// runs it, for the external tests.
func DanglingRefs(doc *ir.Document) []ir.Diagnostic {
	w := walkDocument(doc, ir.WalkValues)
	return checkDanglingRefs(w.refs, w.truncated)
}

// KeptPropIDs returns where the PropID references one walk of doc keeps for
// checkPropIDRefs to settle sit, in walk order, for the external tests.
func KeptPropIDs(doc *ir.Document) []string {
	sites := walkDocument(doc, ir.WalkValues).props.sites
	paths := make([]string, 0, len(sites))
	for _, s := range sites {
		paths = append(paths, s.Path)
	}
	return paths
}

// WalkVisitors returns the visitors Validate's one walk of doc carries, in
// order, for the external tests to walk doc with themselves.
func WalkVisitors(doc *ir.Document) []ir.Visitor {
	var visitors []ir.Visitor
	walkDocument(doc, func(_ any, _ string, vs ...ir.Visitor) bool {
		visitors = vs
		return false
	})
	return visitors
}

// AppendTypeIDs is appendTypeIDs, for the external tests.
var AppendTypeIDs = appendTypeIDs

// ValidateWith is validate, walking the document with walk, for the external
// tests that count the walks.
var ValidateWith = validate
