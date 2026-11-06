// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package testspec holds the OpenAPI fixture specs that the tests of engine,
// cmd/morphic, cmd/morphic-harness and internal/harness, and the OpenAPI
// compiler's end-to-end tests and fuzz targets, would otherwise each carry as
// copies.
//
// It imports only the standard library, because those fuzz targets run in a
// test binary kept small on purpose: a fuzz run pays on every execution for all
// the code the binary links.
package testspec
