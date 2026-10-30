// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package testspec holds the OpenAPI fixture spec strings that engine,
// cmd/morphic, cmd/morphic-harness, and internal/harness tests would
// otherwise each carry as byte-identical copies.
//
// It deliberately couples those packages' tests to three shared constants
// instead of four fixtures drifting apart unnoticed. The package holds only
// constants — no functions, no vars with initializers — so it stays a
// zero-statement package that the coverage gate skips rather than fails
// for lacking tests.
package testspec
