package irverify

import (
	"reflect"
	"strings"

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

// declaredAt is where an identity was first declared, and the fingerprint of the
// node that declared it, so a later declaration of the same identity can be
// compared against it rather than only counted.
type declaredAt struct {
	path        string
	fingerprint string
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
func checkDuplicateIDs(doc *ir.Document, decls declarations) ([]Violation, bool) {
	fingerprints, truncated := propertyFingerprints(doc)
	first := make(map[identity]declaredAt, len(decls.ids))
	var vs []Violation
	for _, d := range decls.ids {
		key := identity{class: d.Class, id: d.ID}
		at, taken := first[key]
		if !taken {
			first[key] = declaredAt{path: d.Path, fingerprint: fingerprints[d.Path]}
			continue
		}
		if d.Class == propIDType && at.fingerprint == fingerprints[d.Path] {
			continue // one property materialized at several paths, not two properties
		}
		vs = append(vs, Violation{
			Code:    "ir/duplicate-" + ir.RefNoun(d.Class) + "-id",
			Message: "id " + d.ID + " is declared here and at " + at.path,
			Path:    d.Path,
		})
	}
	return vs, decls.truncated || truncated
}

// propertyFingerprints returns every property the document holds, fingerprinted,
// keyed by the path that declares it — the same path ir.DeclaredIDs reports for
// the same node, since both walks spell a node's location the one way.
func propertyFingerprints(doc *ir.Document) (map[string]string, bool) {
	fingerprints := map[string]string{}
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct || v.Type() != propertyType {
			return true
		}
		fingerprints[path] = fingerprintOf(v)
		return true
	})
	return fingerprints, truncated
}

// fingerprintOf renders what separates one property from another (see
// checkDuplicateIDs). It reads fields off the walked value rather than
// converting it back to an ir.Property, because a value the walk reached through
// an unexported field cannot be converted (see ir.WalkValues); checkNaming's
// namingChannels reads its channels the same way.
//
// The parts are joined on NUL, which no source name, wire name or ID contains,
// so no two properties can agree on the rendering while disagreeing on the
// parts.
func fingerprintOf(prop reflect.Value) string {
	return strings.Join([]string{
		prop.FieldByName("Name").FieldByName("Source").String(),
		prop.FieldByName("WireName").String(),
		prop.FieldByName("Type").FieldByName("Target").String(),
	}, "\x00")
}
