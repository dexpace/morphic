// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package fuzz_test holds the fuzz targets that compile whole documents through
// the OpenAPI compiler's public API, apart from the end-to-end tests. A fuzz run
// instruments everything its test binary links and pays for all of it on every
// execution, so these import the compiler and the standard library and not the
// test tooling the end-to-end suite needs, testify included.
package fuzz_test
