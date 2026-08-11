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
// ir.PropID is held to the same claim by way of a fingerprint, because a
// repeated PropID is often not a second declaration. A response declared once in
// components and referenced by three operations materializes into all three —
// responses are embedded by value, not interned — so the header property it
// declares appears at three paths under the one ID its defining occurrence
// derives (testdata/conformance/openapi/component-reuse.yaml). That the ID stays
// the declaration's rather than the use site's is what #107 fixed, so the repeat
// is invariant 3 holding, not breaking: the copies are one property and a lookup
// for that ID is unambiguous. Two *genuinely different* properties on one PropID
// are the defect, and skipping the class outright hid them with the copies
// (GitHub #280).
//
// The fingerprint is the property's wire identity and its type: source name,
// wire name, and the ID its TypeRef targets. Copies of one declaration agree on
// all three because they are copies; two properties agreeing on all three are
// indistinguishable to a consumer that looks one up by ID, which is the only
// thing a duplicate ID costs. Nothing wider is read, because a fingerprint that
// separates two copies reports every document that reuses a component.
//
// The first declaration in walk order stands and every later one is reported, so
// n nodes on one ID yield n-1 violations rather than n. Walk order is
// deterministic (invariant 7), so which one stands does not vary between runs.
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
