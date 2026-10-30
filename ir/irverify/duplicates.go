package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

var (
	// propIDType is the reflect.Type of the one ID class checkDuplicateIDs holds
	// to a weaker claim than "declared once"; see there for why.
	propIDType = reflect.TypeFor[ir.PropID]()
	// propertyType is the node that class identifies, which is what has to be
	// fingerprinted to make that weaker claim.
	propertyType = reflect.TypeFor[ir.Property]()
)

// identity is one declared ID together with the class it belongs to, which is
// the pair that has to be unique: an OpID and a TypeID spelling the same string
// name two different nodes.
type identity struct {
	class reflect.Type
	id    string
}

// fingerprint is what separates one property from another (see
// checkDuplicateIDs): its source name, wire name and TypeRef target. It holds
// the property's own strings, so taking one copies no bytes, and two compare
// equal exactly when each part does.
type fingerprint struct {
	source, wire, target string
}

// checkDuplicateIDs asserts no two nodes declare the same identity (invariant
// #3), reporting each later declaration against the first. The registry maps
// cannot enforce it for classes they lack, such as operations, services and
// groups, so a shared ID resolves to whichever the reader reaches first.
//
// ir.PropID is held to a fingerprint instead: a response declared once in
// components is embedded by value at every use, each copy keeping the
// declaration's PropID (GitHub #107). Different properties on one PropID are
// the defect (#280). The fingerprint is source name, wire name and TypeRef
// target; a wider one flags every reused component.
func checkDuplicateIDs(*ir.Document) walkCheck {
	// The first property met on each PropID, and the later ones whose
	// fingerprint differs from it, each named by its PropID and how many met on
	// that PropID before it. The walk meets properties in the order the
	// declarations list them, so the first met is the PropID's first
	// declaration, and the n-th met its n-th.
	first := map[string]firstProperty{}
	differs := map[occurrence]bool{}
	return walkCheck{
		visit: func(v reflect.Value, _ ir.WalkPath) bool {
			if v.Kind() != reflect.Struct || v.Type() != propertyType {
				return true
			}
			id, got := v.FieldByName("ID").String(), fingerprintOf(v)
			met, seen := first[id]
			if !seen {
				first[id] = firstProperty{fingerprint: got, met: 1}
				return true
			}
			if met.fingerprint != got {
				differs[occurrence{id: id, n: met.met}] = true
			}
			met.met++
			first[id] = met
			return true
		},
		report: func(decls declarations) []Violation {
			return duplicateIDs(decls, differs)
		},
	}
}

// firstProperty is the first property the walk met on a PropID, by its
// fingerprint, and how many it has met on that PropID so far.
type firstProperty struct {
	fingerprint fingerprint
	met         int
}

// occurrence names one declaration of an identity: the ID, and how many
// declarations of it came before this one.
type occurrence struct {
	id string
	n  int
}

// redeclaration is a declaration of an identity declared before it, by its
// position among the declarations and the first one's.
type redeclaration struct {
	at, first int
}

// duplicateIDs reports each declaration in decls of an identity declared before
// it, unless it is a property that is the first one's copy: differs holds the
// occurrences of the properties that are not. Only what it reports has its
// path spelled.
func duplicateIDs(decls declarations, differs map[occurrence]bool) []Violation {
	list := decls.list()
	type declared struct{ first, met int }
	seen := make(map[identity]declared, len(list))
	var again []redeclaration
	for i, d := range list {
		key := identity{class: d.Class, id: d.ID}
		before, taken := seen[key]
		if !taken {
			seen[key] = declared{first: i, met: 1}
			continue
		}
		seen[key] = declared{first: before.first, met: before.met + 1}
		if d.Class == propIDType && !differs[occurrence{id: d.ID, n: before.met}] {
			continue // one property materialized at several paths, not two properties
		}
		again = append(again, redeclaration{at: i, first: before.first})
	}
	return redeclarationViolations(list, again, decls)
}

// redeclarationViolations reports each of again, spelling the paths of the
// declarations it names and no other.
func redeclarationViolations(list []ir.IDDeclaration, again []redeclaration, decls declarations) []Violation {
	if len(again) == 0 {
		return nil
	}
	at := make(map[int]bool, 2*len(again))
	for _, r := range again {
		at[r.at], at[r.first] = true, true
	}
	paths := decls.pathsAt(at)
	vs := make([]Violation, 0, len(again))
	for _, r := range again {
		d := list[r.at]
		vs = append(vs, Violation{
			Code:    "ir/duplicate-" + ir.RefNoun(d.Class) + "-id",
			Message: "id " + strconv.Quote(d.ID) + " is declared here and at " + strconv.Quote(paths[r.first]),
			Path:    paths[r.at],
		})
	}
	return vs
}

// fingerprintOf reads what separates one property from another (see
// checkDuplicateIDs). It reads fields off the walked value rather than
// converting it back to an ir.Property, because a value the walk reached
// through an unexported field cannot be converted (see ir.WalkValues);
// checkNaming's namingChannels reads its channels the same way.
func fingerprintOf(prop reflect.Value) fingerprint {
	return fingerprint{
		source: prop.FieldByName("Name").FieldByName("Source").String(),
		wire:   prop.FieldByName("WireName").String(),
		target: prop.FieldByName("Type").FieldByName("Target").String(),
	}
}
