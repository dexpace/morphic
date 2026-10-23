// Package e2e_test holds the OpenAPI compiler's end-to-end tests: the
// conformance corpus, the goldens, fuzzing, benchmarks, and scenarios several
// internal packages decide together, each compiling a spec through openapi.New
// and reading what comes out.
//
// It is a package of its own because coverage counts only a package's own
// tests. Nothing here is covered by them, so the code they reach still needs
// the unit tests beside it, and a case here never stands in for one.
package e2e_test
