// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package promotion is the vendor-extension promotion policy: which extension
// fills which typed IR field (docs/compilers/openapi.md#extension-promotion).
// A Promoter reads a node's Unmodeled entries, never the source, and a field it
// fills is marked ExtensionPromotionHeuristic on the node keeping the key, as
// every policy marks what it infers (docs/compilers/openapi.md#policies).
//
// It is a package because the walks and the readers of the whole document
// alike read the policy, and those readers take nothing else of the context
// the walks lower under.
package promotion
