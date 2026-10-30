// Package overlay applies an OpenAPI Overlay document to the parsed node tree,
// and records which positions in the result the overlay is answerable for.
//
// It sits on the entry side beside the loader: it mutates the tree in place and
// knows nothing about lowering. Spec problems leave as ir.Diagnostic values,
// not a Go error, since an overlay can only be wrong as an input document.
//
// Mutating the tree rather than re-serialised bytes keeps each untouched node's
// line and column as parsed, so diagnostics name positions in a file that
// exists. Origin names the positions the overlay introduced.
package overlay
