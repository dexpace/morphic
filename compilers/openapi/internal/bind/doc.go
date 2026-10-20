// Package bind resolves every schema $ref of an OpenAPI document into a table,
// over the decoded YAML tree and without the parser's resolver: each site, the
// position its chain ends at, the role of what is there, and how it ended. It
// reads a reference as the compiler does: a JSON pointer from the root, the
// $defs rule of internal/defs, the grammar's schema positions, and neither
// $anchor nor $id as a target (ir-design §4.7).
//
// Nothing calls it yet. It is the measured prototype of GitHub #913; putting
// it behind an option, with the parser's resolver as its oracle, is #914.
package bind
