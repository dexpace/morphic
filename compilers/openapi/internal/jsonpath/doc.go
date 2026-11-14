// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package jsonpath evaluates RFC 9535 queries over the JSON value a YAML tree
// stands for: an alias reads as the node it names, a `<<` merge key's members
// as members of the mapping merging them, and a scalar as the compiler reads
// it. It is how the compiler reads an overlay's selectors.
//
// Over a hostile document a query can cost far more than its text, so every
// step an evaluation takes is charged to the caller's budget, and every
// nesting the parser follows is held to a named cap.
package jsonpath
