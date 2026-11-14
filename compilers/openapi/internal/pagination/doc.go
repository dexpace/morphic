// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package pagination fills an operation's ir.Pagination from the names its
// query parameters and its success responses' members give, under the
// pagination-inference policy (docs/compilers/openapi.md#pagination-policy).
// It runs once the document is whole, since a response's members are read
// through the types the registry holds, and it reads the document the walks
// built, never the source.
package pagination
