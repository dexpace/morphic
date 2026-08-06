package irverify

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// propIDType is the reflect.Type of the one ID class checkDuplicateIDs does not
// hold to being declared once; see there for why.
var propIDType = reflect.TypeFor[ir.PropID]()

// identity is one declared ID together with the class it belongs to, which is
// the pair that has to be unique: an OpID and a TypeID spelling the same string
// name two different nodes.
type identity struct {
	class reflect.Type
	id    string
}

// checkDuplicateIDs asserts no two nodes declare the same identity (invariant
// #3). It reads the declarations rather than walking for them, and passes on
// whether the walk that produced them was cut short; Verify folds that into the
// document's one ir/walk-truncated violation.
//
// Uniqueness was enforced only by the registry maps, and they cannot express it
// for a class they do not hold: an operation nests inside the
// Service→OperationGroup tree and a service sits in a slice, so two of either
// sharing an ID made every reference to it resolve to whichever the reader
// reaches first, with nothing in the document saying which that is.
//
// A duplicate is a Violation rather than an ir.Diagnostic because an ID is
// derived from the source pointer of the defining occurrence, so two nodes
// sharing one means a compiler minted the same pointer twice — our bug, not
// something a spec author wrote or can fix.
//
// ir.PropID is outside the claim, because a repeated PropID is usually not a
// second declaration. A response declared once in components and referenced by
// three operations materializes into all three — responses are embedded by value,
// not interned — so the header property it declares appears at three paths under
// the one ID its defining occurrence derives
// (testdata/conformance/openapi/component-reuse.yaml). That the ID stays the
// declaration's rather than the use site's is what #107 fixed, so the repeat is
// invariant 3 holding, not breaking: the copies are one property and a lookup for
// that ID is unambiguous.
//
// The skip is wider than that reason, and deliberately provisional rather than
// settled: two genuinely *different* properties minted at one PropID go
// unreported with them, which is the same defect this check exists for. Telling
// the two apart needs a fingerprint of the node rather than its ID alone —
// GitHub #280 carries it, and this comment is the debt until it lands.
//
// The first declaration in walk order stands and every later one is reported, so
// n nodes on one ID yield n-1 violations rather than n. Walk order is
// deterministic (invariant 7), so which one stands does not vary between runs.
func checkDuplicateIDs(_ *ir.Document, decls declarations) ([]Violation, bool) {
	first := make(map[identity]string, len(decls.ids))
	var vs []Violation
	for _, d := range decls.ids {
		if d.Class == propIDType {
			continue
		}
		key := identity{class: d.Class, id: d.ID}
		at, taken := first[key]
		if !taken {
			first[key] = d.Path
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/duplicate-" + ir.RefNoun(d.Class) + "-id",
			Message: "id " + d.ID + " is declared here and at " + at,
			Path:    d.Path,
		})
	}
	return vs, decls.truncated
}
