package irverify

import (
	"maps"
	"testing"
)

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
