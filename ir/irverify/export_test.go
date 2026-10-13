package irverify

import "testing"

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
