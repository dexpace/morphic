// Package bind resolves the schema $refs of an OpenAPI source into an
// immutable table, over the decoded YAML trees and never through the parser's
// resolver: for each reference, where its own $ref lands and where its chain
// ends, or why it does not. Every answer is a function of the documents alone,
// whatever was asked before, and one work budget bounds the whole table.
//
// A reference is read as docs/compilers/openapi.md#references states:
// a JSON pointer trimmed and percent-decoded, nothing else, the $defs rule, a
// role for each position from the OpenAPI grammar, and neither $anchor nor
// $id as a target (ir-design §4.7).
package bind
