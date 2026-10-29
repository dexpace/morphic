package irverify

import (
	"maps"
	"reflect"
	"testing"

	"github.com/dexpace/morphic/ir"
)

// EntryReader is entryReader, for this package's external tests.
type EntryReader = entryReader

// HeldEntry is heldEntry, for this package's external tests.
type HeldEntry = heldEntry

// HeldFor is heldFor, for this package's external tests.
func (r *entryReader) HeldFor(m reflect.Value) HeldEntry { return r.heldFor(m) }

// Read is read, for this package's external tests.
func (h heldEntry) Read(iter *reflect.MapIter) (key, value reflect.Value) { return h.read(iter) }

// ValueCarriers exposes valueCarriers to this package's external tests.
//
// The derivation reads the type-checked ir package through helpers this
// package's internal tests declare, while the table held to it lives in package
// irverify_test beside the document builders that table needs. Exporting it
// from a _test.go file keeps it out of the production API.
func ValueCarriers(t *testing.T) []string {
	t.Helper()
	return valueCarriers(t)
}

// SelfReachableReferences exposes selfReachableReferences to this package's
// external tests, for the reason ValueCarriers is exported.
func SelfReachableReferences(t *testing.T) []string {
	t.Helper()
	return selfReachableReferences(t)
}

// HeldLists exposes, for each field stringLists holds to a rule, whether that
// rule is listSet rather than listPath, so the external tests' fixtures can be
// held to reaching every one with the right expectation.
func HeldLists() map[string]bool {
	held := map[string]bool{}
	for key, rule := range maps.All(stringLists) {
		if rule == listPath || rule == listSet {
			held[key] = rule == listSet
		}
	}
	return held
}

// WalkVisitors builds every walking check for doc and returns the visitors
// Verify's one walk carries, the declarations' collector among them, for this
// package's external tests.
func WalkVisitors(doc *ir.Document) []ir.Visitor {
	_, _, visitors := walkVisitors(doc, walkChecks())
	return visitors
}
