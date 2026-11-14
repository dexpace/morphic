// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package replacement fills what a deprecation names as its replacement
// (ir.Deprecation.ReplacedBy): Resolve from the vendor extensions a policy
// maps to lowering.TargetDeprecationReplacedBy, as every promotion reads the
// extensions the nodes keep, and Infer, under the replacement policy, from
// what the deprecation's message names where no such extension is written.
// Both run once the document is whole, since a replacement may be declared
// after what it replaces, the promotion before the inference
// (docs/compilers/openapi.md#policies).
package replacement
