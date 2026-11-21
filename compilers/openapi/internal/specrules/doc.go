// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package specrules holds the OpenAPI Specification's rules as data, and the
// engine that checks a source tree against them at the minor it declares.
//
// The table states, for every object of OpenAPI 3.0, 3.1 and 3.2, each field
// with the minors defining and requiring it, the rules binding fields,
// unwritten fields' defaults, and the dialects a Schema Object can name, each
// row citing its sentence.
//
// A rule another package reports names it as owner, and the engine skips it.
// The engine reports an unknown key wherever it is written.
//
// Check is pure: it reads the tree through nodeview and keeps no state.
package specrules
