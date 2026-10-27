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
	// The fingerprint of the first property met on each PropID, and the paths
	// of the properties met later whose fingerprint differs from it. The walk
	// meets properties in the order the declarations list them, so the first
	// met is the PropID's first declaration, and only a property that differs,
	// which is a defect, has its path rendered.
	first := map[string]fingerprint{}
	differs := map[string]bool{}
	return walkCheck{
		visit: func(v reflect.Value, at ir.WalkPath) bool {
			if v.Kind() != reflect.Struct || v.Type() != propertyType {
				return true
			}
			id, got := v.FieldByName("ID").String(), fingerprintOf(v)
			switch met, seen := first[id]; {
			case !seen:
				first[id] = got
			case met != got:
				differs[at.String()] = true
			}
			return true
		},
		report: func(decls declarations) []Violation {
			return duplicateIDs(decls.ids, differs)
		},
	}
}

// duplicateIDs reports each declaration in decls of an identity declared before
// it, unless it is a property that is the first one's copy: of the properties
// declared again, differs holds the paths of those that are not.
func duplicateIDs(decls []ir.IDDeclaration, differs map[string]bool) []Violation {
	first := make(map[identity]string, len(decls))
	var vs []Violation
	for _, d := range decls {
		key := identity{class: d.Class, id: d.ID}
		at, taken := first[key]
		if !taken {
			first[key] = d.Path
			continue
		}
		if d.Class == propIDType && !differs[d.Path] {
			continue // one property materialized at several paths, not two properties
		}
		vs = append(vs, Violation{
			Code:    "ir/duplicate-" + ir.RefNoun(d.Class) + "-id",
			Message: "id " + strconv.Quote(d.ID) + " is declared here and at " + strconv.Quote(at),
			Path:    d.Path,
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
