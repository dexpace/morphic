// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package sourceindex answers, in one walk, the questions asked of a decoded
// source tree before any of it is lowered: how many nodes it declares, how
// deeply it nests, which aliases name an ancestor, and which mappings carry a
// tag the parser faults on, as a value the caller carries rather than a walk
// it repeats. Only what an alias names is measured once more.
//
// The index has no exported fields and no maps, and nothing in it changes
// once Build returns, so a copy is as good as the original. Equal trees give
// equal indexes.
package sourceindex
