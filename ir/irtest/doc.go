// Package irtest provides test helpers for IR documents: golden snapshots,
// fixtures built in one call, filled or nested to a chosen depth, a document's
// encoding and ID namespaces, diagnostics' codes, and the repository's root.
//
// A golden stores a document without its irVersion. The stamp names the shape
// of the JSON, which a test in package ir pins against that shape, so storing
// it in every snapshot would only make each bump rewrite them all. The helpers
// assert it instead: a golden is written and compared only for a document
// stamped with ir.IRVersion, and ReadGolden puts the stamp back.
package irtest
