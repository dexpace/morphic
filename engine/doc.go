// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package engine orchestrates the Morphic pipeline: it asks the registered
// compilers which of them recognizes the source, dispatches to that one, runs
// its ordered list of IR passes, and judges whether the run failed. It is the
// only package that composes compilers and passes together, and it holds no
// knowledge of any source format — what a spec looks like and what its options
// are called are the compiler's to answer.
//
// It also owns every read, write and rendering a run needs: engine/reader
// reads what references name, engine/output writes the document, and
// engine/report renders diagnostics.
package engine
