// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package oas holds the OpenAPI Specification's rules as data, and the engine
// that checks a source tree against them at the minor it declares.
//
// The table states, for every object of OpenAPI 3.0, 3.1 and 3.2, each field
// with the minors defining and requiring it, the rules binding fields,
// unwritten fields' defaults, and the dialects a Schema Object can name, each
// row citing its sentence.
//
// A rule another package already reports names it as owner: the table states
// it and the engine skips it. Unknown keys are not reported here.
//
// Check is pure: it reads the tree through nodeview and keeps no state.
package oas
