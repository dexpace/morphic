// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package pathfold folds paths as a case-insensitive, normalization-insensitive
// file system compares them and checks whether a path segment is portable
// across target file systems (docs/emitters/emit/rules.md).
//
// It carries no Morphic concept, so the engine, the emitter contract and
// naming share it from Layer 0.
package pathfold
