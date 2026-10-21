// Package bind resolves the schema $refs of an OpenAPI source into an
// immutable table, over the decoded YAML trees and never through the parser's
// resolver: for each reference, where its own $ref lands and where its chain
// ends, or why it does not. Every answer is a function of the documents alone,
// and one work budget bounds the whole table.
//
// A reference is read as docs/compilers/openapi.md#references states: against
// its schema resource's base, by pointer, plain name or identifier, with the
// $defs rule and a role for each position from the OpenAPI grammar.
package bind
