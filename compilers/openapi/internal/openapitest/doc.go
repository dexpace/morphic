// Package openapitest holds the test scaffolding every test package under
// compilers/openapi would otherwise copy, since test files cannot share
// unexported helpers.
//
// A helper belongs here only if all of those packages can import it, so imports
// stop at ir, compilers, diag and third-party libraries: reaching further would
// make this package unimportable from the internal tests of what it reached.
// That keeps parseFull and the lowerer fixture with their drivers.
//
// componentID and typeByName stay in test files: the idderivations rule
// refuses a spelled-out ID in production code, and deriving one through compile
// would make the lookup a tautology.
package openapitest
