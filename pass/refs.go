package pass

import (
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// typeIDType is the reflect.Type of ir.TypeID, the one reference class the
// reachability analysis in validate.go needs without a document to resolve
// against.
var typeIDType = reflect.TypeFor[ir.TypeID]()

// propIDType and propertyType are the reflect.Types of ir.PropID and the struct
// that declares one. A PropID references no document-level registry — a property
// is a position inside its model — so both halves of resolving one come from the
// walk itself: the sites that carry a PropID, and the ir.Property values that
// declare them.
var (
	propIDType   = reflect.TypeFor[ir.PropID]()
	propertyType = reflect.TypeFor[ir.Property]()
)

// refSite is one discovered ID reference: the class that made it a reference,
// its value, and a human-readable location used for diagnostic provenance.
type refSite struct {
	idType reflect.Type
	id     string
	where  string
}

// collectRefs returns every reference reachable from root whose Go type isRef
// accepts, sorted by location, and reports whether the depth cap truncated the
// walk.
//
// The traversal is ir.WalkValues, which this pass and ir/irverify share so that
// one document has one walk: the same bound, cycle guard, order and path
// spelling, and a reference-bearing field added to the IR covered by both the
// moment it exists. The integer-index checks in validate.go stay rooted at a
// stable ID rather than at doc — those diagnostics are read by a spec author,
// for whom an ID outlives a position.
//
// The sort alone does not make the result deterministic (invariant 7). Sorting
// reorders a site set; it cannot repair one whose *membership* varies, which is
// what a randomized traversal of an aliased value graph produces — hence the
// ordered map walk ir.WalkValues performs.
func collectRefs(root any, path string, isRef func(reflect.Type) bool) ([]refSite, bool) {
	return collectWalk(root, path, isRef, nil)
}

// collectWalk is collectRefs with an optional per-struct hook, so a caller can
// collect declarations alongside references in one traversal rather than
// standing up a second bounded walk beside this one.
func collectWalk(root any, path string, isRef func(reflect.Type) bool, onStruct func(reflect.Value)) ([]refSite, bool) {
	var sites []refSite
	truncated := ir.WalkValues(root, path, func(v reflect.Value, at string) bool {
		if v.Kind() == reflect.String && v.String() != "" && isRef(v.Type()) {
			sites = append(sites, refSite{idType: v.Type(), id: v.String(), where: at})
		}
		if onStruct != nil && v.Kind() == reflect.Struct {
			onStruct(v)
		}
		return true
	})
	// Distinct sites have distinct paths in every shape the IR declares, but two
	// embedded structs contributing a same-named promoted field would collide;
	// a stable sort keeps the deterministic walk order in that case rather than
	// leaving the pair to sort.Interface's unspecified swap order.
	slices.SortStableFunc(sites, func(a, b refSite) int {
		return strings.Compare(a.where, b.where)
	})
	return sites, truncated
}

// collectTypeIDs returns every ir.TypeID reachable from root. Type reachability
// follows one class, so it names that class rather than resolving against a
// document it is not handed.
func collectTypeIDs(root any, path string) ([]refSite, bool) {
	return collectRefs(root, path, func(t reflect.Type) bool { return t == typeIDType })
}

// collectPropIDs returns every ir.PropID reference reachable from root together
// with the set of PropIDs the same traversal saw declared, on an ir.Property.
// Both halves come from the value graph, so a PropID-carrying field or a new
// Property-bearing list is covered the moment it exists.
//
// A PropID reached as a map key is left out. The one PropID-keyed map the IR
// declares is Content.Encoding, and checkEncodingKeys resolves those keys against
// the properties of the model the content names — a tighter claim about the same
// defect. Reporting both would give one defect two locations and two codes, which
// the package doc rules out.
func collectPropIDs(root any, path string) ([]refSite, map[ir.PropID]bool) {
	declared := map[ir.PropID]bool{}
	sites, _ := collectWalk(root, path,
		func(t reflect.Type) bool { return t == propIDType },
		func(v reflect.Value) {
			if v.Type() != propertyType {
				return
			}
			if id := v.FieldByName("ID").String(); id != "" {
				declared[ir.PropID(id)] = true
			}
		})
	// Truncation is dropped rather than returned: this walks the same document
	// to the same cap as checkDanglingRefs, which reports ir/walk-truncated for
	// both.
	return slices.DeleteFunc(sites, func(s refSite) bool {
		return strings.HasSuffix(s.where, ir.MapKeySuffix)
	}), declared
}
