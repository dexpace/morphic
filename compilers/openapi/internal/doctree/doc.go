// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package doctree reads an input's bytes, the source's, an overlay's or a
// document a reference names, into the YAML parser's tree of its first
// document that holds content, held to the node budget as it is read.
//
// A text that is one JSON text is read as JSON, into the tree the parser builds
// for it (docs/parser-library.md#json-as-yaml). Any other text is read as a
// YAML stream lexed as YAML 1.2 (docs/parser-library.md#yaml11-lexing) and its
// plain scalars resolved by YAML 1.2's core schema
// (docs/parser-library.md#scalar-resolution). It owes agreement with the
// parser's nodes, and words no finding: its caller does.
package doctree
