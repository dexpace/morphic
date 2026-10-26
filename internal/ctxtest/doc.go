// Package ctxtest holds the context decorators the tests of cancellation
// share, so a test anywhere in the module puts a cancel at a chosen check of a
// context the same way. It is test infrastructure outside the compile pipeline,
// and imports only the standard library, so the tests of any package may
// import it.
package ctxtest
