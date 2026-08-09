package irverify

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// idFieldName is the field a node declares its own identity through. It is
// spelled here as well as in ir because this check reads the one declaration
// ir.DeclaredIDs drops — the empty one — and so cannot ask ir for the answer.
const idFieldName = "ID"

// checkDeclaredIDs asserts every node that declares an identity of its own
// carries a non-empty one.
//
// An ID is derived from the source pointer of its defining occurrence, so an
// empty one means a compiler minted nothing where it was supposed to mint an
// identity — our bug, on the same reasoning as ir/duplicate-<noun>-id. What it
// costs downstream is worse than the missing ID: every reference to the node
// resolves against a registry that does not contain it, so the defect reads as a
// dangling reference at the *referring* site rather than as the missing
// declaration it is.
//
// Nothing else reaches it. checkRegistryKeys reports an empty ID from the
// registry *key*, and only Types, Channels, Messages and Auth are maps; an
// Operation is declared inside the Service→OperationGroup tree, a Service sits
// in a slice, and a Property is a position inside its model, so none of the
// three has a key for that rule to read. checkDuplicateIDs never sees one
// either, because ir.DeclaredIDs drops an empty ID before the declaration list
// is built — correctly, since nothing can reference one and treating several
// nodes carrying one as duplicates of each other would name the wrong defect
// (GitHub #289). The claim that a node meant to have an identity carries one is
// separate, and this is where it is made.
//
// The classes checkRegistryKeys already covers are skipped, so one defect stays
// one report rather than becoming two under the same code. Which those are is
// read off Document's own shape through ir.DocumentRegistries rather than listed
// here, so a registry added to Document moves its class across on its own.
//
// The code is the one the map-keyed classes already use — ir/empty-<noun>-id —
// so one defect reads under one code whichever rule reports it.
func checkDeclaredIDs(doc *ir.Document, _ declarations) ([]Violation, bool) {
	keyed := ir.DocumentRegistries(doc)
	var vs []Violation
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.Struct {
			return true
		}
		class, id, declares := declaredID(v)
		if !declares || id != "" {
			return true
		}
		if _, hasRegistry := keyed[class]; hasRegistry {
			return true // checkRegistryKeys reads this class from its registry key
		}
		noun := ir.RefNoun(class)
		vs = append(vs, Violation{
			Code:    "ir/empty-" + noun + "-id",
			Message: noun + " declares no identity of its own, so nothing can reference it",
			Path:    path,
		})
		return true
	})
	return vs, truncated
}

// declaredID returns the identity v declares for itself: the class of ID, its
// value, and whether v declares one at all.
//
// It repeats the predicate ir.declaredID applies — a field named ID that v's own
// type declares, of a named string type — because that function answers only for
// the non-empty ones. Repeating it exactly is the point: the two have to agree on
// what declares an identity, or this rule and ir.DeclaredIDs disagree about which
// nodes exist.
//
// A promoted field is not its own declaration. Every type node embeds TypeCommon
// and so promotes its TypeID, which ir.DeclaredIDs would record a second time —
// here such a node is skipped as registry-keyed before that matters, so the
// clause is carried for fidelity with ir rather than for an effect of its own.
//
// Both narrowing clauses are pinned by TestDeclaredID_ClassifiesEachShape rather
// than by the walk comparison beside it: no Document separates them, since every
// promoted ID is registry-keyed and the IR declares no plain-string ID, so
// dropping either leaves every fixture-driven test here green.
// TestCheckDeclaredIDs_ReachesEveryIDDeclaringNode holds the walk in step with
// ir.DeclaredIDs; this holds the predicate.
func declaredID(v reflect.Value) (class reflect.Type, id string, declares bool) {
	f, isDeclared := v.Type().FieldByName(idFieldName)
	if !isDeclared || len(f.Index) != 1 || !namedString(f.Type) {
		return nil, "", false
	}
	return f.Type, v.Field(f.Index[0]).String(), true
}

// namedString reports whether t is a named string type, the shape every ID class
// takes. A plain string is not one: it names something rather than identifying
// it.
func namedString(t reflect.Type) bool {
	return t.Kind() == reflect.String && t.PkgPath() != ""
}
