// Package harness compiles OpenAPI specs and applies the bug-catching oracles
// (no panic/error, irverify invariants, JSON round-trip, determinism), returning
// a structured Result per spec. It is test/tooling infrastructure, not part of
// the compile pipeline.
package harness
