// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Command morphic-census compiles API specs and reports which facts of the IR
// the resulting documents set, as internal/census counts them, with the count
// of documents and instances for each. Every fact is listed, so what no spec
// produces reads as a zero; this command's tests hold the facts no spec under
// the repository's testdata sets to testdata/unwitnessed.golden.txt.
//
// Each argument is a spec file or a directory of specs, chosen as
// morphic-harness chooses them. Exit code is 0 on a report, and 2 on a usage
// error or a failure to read a spec or the IR's declarations.
package main
