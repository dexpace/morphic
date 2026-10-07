// Package openapitest holds the test scaffolding every test package under
// compilers/openapi would otherwise copy, since test files cannot share
// unexported helpers.
//
// A helper belongs here only if all of those packages can import it, so imports
// stop at ir, compilers, diag and third-party libraries: reaching further would
// make this package unimportable from the internal tests of what it reached.
// That keeps parseFull and the lowerer fixture with their drivers.
//
// componentID and typeByName stay in test files: the ID-grammar sweep refuses
// a spelled-out ID in production code, and deriving one through compile would
// make the lookup a tautology.
package openapitest

// TB is the subset of *testing.T these helpers need.
//
// It is an interface rather than *testing.T so this package's own tests can
// drive the failure branches with a recording stub, the way ir/irtest does:
// a helper that aborts a real test cannot have its abort path covered.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
	FailNow()
	Fatalf(format string, args ...any)
}
