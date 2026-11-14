// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package pagination fills an operation's ir.Pagination: Promote from the
// extensions a policy maps to the pagination targets
// (docs/compilers/openapi.md#extension-pagination), and InferWalks, under the
// pagination-inference policy, from the names its query parameters and its
// success responses' members give (docs/compilers/openapi.md#pagination-policy).
// Both run once the document is whole, since a response's members are read
// through the types the registry holds, the promotion before the inference
// (docs/compilers/openapi.md#policies), and they read the document the walks
// built, never the source.
package pagination
