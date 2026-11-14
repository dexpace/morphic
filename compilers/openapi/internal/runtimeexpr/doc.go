// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package runtimeexpr reads the runtime expressions an OpenAPI document writes,
// as the values of a link and the page walk of an extension do, and finds the
// members of a body one names, through the reading of the compiled document
// those steps share (lowering.Whole). It reads the document the walks built and
// nothing of the walks, so the step resolving links and the one promoting a
// pagination, which run once the document is whole, read an expression alike
// (docs/compilers/openapi.md#policies).
package runtimeexpr
