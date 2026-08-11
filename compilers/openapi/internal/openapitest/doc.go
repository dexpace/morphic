// Package openapitest holds the test scaffolding every test package under
// compilers/openapi would otherwise carry as its own copy.
//
// Go test files cannot share unexported helpers across packages, so the split
// into one package per lowering stage duplicated the scaffolding instead of
// retiring it: the same sourceOf, requireNoErrorDiags and componentSpec ended
// up defined once per package, byte for byte. A helper that exists once cannot
// drift; five copies held in step only by review can.
//
// It sits here rather than in the repo-root internal/testspec, which holds the
// fixture spec strings the engine and CLI tests share: that package cannot
// import compilers/openapi/internal/..., and this one needs diag for the
// diagnostic codes it matches on.
//
// # What may live here
//
// A helper belongs here only if it is reachable from every test package under
// compilers/openapi, internal and external alike. That bounds the imports to
// ir, compilers, diag and third-party libraries — packages no test package
// under compilers/openapi sits inside. Reaching any further would make this
// package unimportable from the internal tests of whatever it reached, because
// an internal test file may not import a package that imports its own.
//
// Two families of scaffolding are therefore absent by necessity, not oversight:
//
//   - parseFull, which drives the whole compiler, needs compilers/openapi, and
//     that package's own internal tests could then not import this one.
//   - The lowerer fixture needs lowering.Ctx and schema.AnchorIndex, which would
//     shut out the internal tests of load, resolve, annotation, schema and
//     everything else beneath them.
//
// Both stay with the packages that drive them, each built on a single
// field-initialising constructor so the fixture cannot drift within a package
// even where it must be repeated across them.
//
// A third family is absent for a different reason. componentID and typeByName
// spell a type ID out as a string, and internal/archtest's ID-grammar sweep
// permits that in a test file while refusing it in a production file — which is
// what these are. Deriving the ID through compile instead would satisfy the
// sweep by making the lookup agree with the compiler by construction, and a
// lookup that cannot disagree is no longer an oracle, so those two stay in the
// test files that spell them.
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
