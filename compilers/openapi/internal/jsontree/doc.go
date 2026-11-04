// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package jsontree reads a source that is one JSON text into the node tree the
// YAML parser builds for the same text. It exists for the JSON that parser
// refuses or misreads and RFC 8259 allows
// (docs/parser-library.md#json-as-yaml).
//
// Read gives a text the parser also reads the tree the parser gives it, line
// ends aside, and leaves a text that is not one JSON text to its caller, to read
// as YAML. It owes agreement with the parser's nodes, their tags and columns,
// so it stays in the compiler that reads them.
package jsontree
