// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package e2e_test holds the OpenAPI compiler's end-to-end tests: the
// conformance corpus and the lowering specification held to it, the goldens,
// fuzzing, benchmarks, and scenarios several internal packages decide
// together, which is to say what the compiler does as a whole rather than
// what one of its packages decides.
//
// It is a package of its own because coverage counts only a package's own
// tests. Nothing here is covered by them, so the code they reach still needs
// the unit tests beside it, and a case here never stands in for one.
package e2e_test
