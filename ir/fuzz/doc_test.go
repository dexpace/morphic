// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package fuzz_test holds the fuzz targets over package ir's public API, apart
// from ir's other tests. A fuzz run instruments everything its test binary
// links and pays for all of it on every execution, and those tests link
// golang.org/x/tools through internal/decls. Nothing here counts towards ir's
// coverage, so the seeds never stand in for ir's own tests.
package fuzz_test
