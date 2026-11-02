// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package ircases holds the tables of names and decimals that the tests
// of package ir assert on and that ir's fuzz targets seed from. It is test
// infrastructure outside the compile pipeline, and imports only the standard
// library, because those targets run in a test binary kept small on purpose: a
// fuzz run pays on every execution for all the code the binary links. Each
// table is an unexported variable that a function hands out a copy of, so no
// test can change what another reads.
package ircases
