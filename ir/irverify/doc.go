// Package irverify checks a compiled ir.Document against the structural
// invariants every compiler must uphold (stable IDs, no dangling references,
// neutral naming). Its findings are Violation values — our own compiler bugs,
// deliberately a separate channel from ir.Diagnostic, which reports problems in
// the source spec. Verify is pure and imports only ir.
package irverify
