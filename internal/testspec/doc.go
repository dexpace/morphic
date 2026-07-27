// Package testspec holds the OpenAPI fixture spec strings that engine,
// cmd/morphic, cmd/morphic-harness, and internal/harness tests would otherwise
// each carry as their own byte-identical copy.
//
// This deliberately couples those packages' tests to three shared constants
// instead of four independent fixtures drifting apart unnoticed; that
// coupling is the accepted tradeoff. The package holds only constants — no
// functions, no vars with initializers — so it stays a zero-statement package
// that scripts/check-coverage.sh skips rather than fails for lacking tests.
package testspec
