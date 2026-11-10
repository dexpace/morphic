// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package loweringtest builds the lowering context the compiler's tests lower
// under, over a document the load phase read, as the compiler builds its own.
//
// It is apart from openapitest because it reaches load and lowering, and an
// internal test cannot import a package that imports its own: internal tests of
// load, and of packages beneath it, import openapitest.
package loweringtest
