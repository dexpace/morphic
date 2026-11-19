// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package statement reads what a schema position writes into one value: the
// schema object there, every key it writes in the slot its keyword speaks of
// (specrules.Slot), each slot's values typed, and its inline allOf entries
// flattened as the lowering flattens them
// (docs/compilers/openapi.md#allof-classification). A Table reads each schema
// object once and remembers what it read.
//
// model.go is the one file that reads the parser library's schema type: the
// rest of the package, and every reader of a statement, sees what the parser
// read only through the values it holds.
package statement
