// Package irverify checks a compiled ir.Document against the structural
// invariants every compiler must uphold: stable IDs, no two nodes claiming one
// identity, no dangling references, neutral naming, routable Unmodeled entries,
// in-range provenance, a readable schema stamp, and more besides.
//
// Verify's body and walkChecks are the enumeration; the parenthetical that used
// to close that sentence was not, and had already fallen behind the checks
// beside it. A list here is one more thing that has to be kept true, and nothing
// fails when it stops being.
//
// Its findings are Violation values — our own compiler bugs, deliberately a
// separate channel from ir.Diagnostic, which reports problems in the source
// spec. Verify is pure and imports only ir.
package irverify
