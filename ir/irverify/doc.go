// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package irverify checks a compiled ir.Document against the structural
// invariants every compiler must uphold: stable IDs, no two nodes claiming one
// identity, no dangling references, neutral naming, routable Unmodeled entries,
// in-range provenance, a readable schema stamp, a service and a well-formed
// source table, and more besides. Verify's body and walkChecks are the full
// enumeration.
//
// Its findings are Violation values, our own compiler bugs, deliberately a
// separate channel from ir.Diagnostic, which reports problems in the source
// spec. Verify is pure and imports only ir.
package irverify
